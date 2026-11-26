package colab

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// Executor executes Python code through a Colab runtime's Jupyter kernel.
// ColabRequestHandler handles Colab-specific kernel requests such as Drive credential propagation.
type ColabRequestHandler func(context.Context, jupyterMessage) (value any, handled bool, err error)

type Executor struct {
	Session             *Session
	Client              *http.Client
	ColabRequestHandler ColabRequestHandler

	mu sync.Mutex
}

// ExecResult contains output collected from one Jupyter execution.
type ExecResult struct {
	Stdout string
	Stderr string
	Error  string
	Images [][]byte
}

// NewExecutor creates an executor for session. client may be nil.
func NewExecutor(session *Session, client *http.Client) *Executor {
	if client == nil {
		client = http.DefaultClient
	}
	return &Executor{Session: session, Client: client}
}

// InputHandler supplies a response to a Jupyter stdin request.
type InputHandler func(context.Context, string) (string, error)

// Exec runs code in the runtime's default Jupyter kernel without stdin support.
func (e *Executor) Exec(ctx context.Context, code string) (ExecResult, error) {
	return e.ExecWithInput(ctx, code, nil)
}

// ExecWithInput runs code and uses input for Jupyter stdin prompts. A nil handler rejects stdin requests.
func (e *Executor) ExecWithInput(ctx context.Context, code string, input InputHandler) (ExecResult, error) {
	if e == nil || e.Session == nil {
		return ExecResult{}, fmt.Errorf("colab: executor has no session")
	}
	if e.Session.URL == "" || e.Session.Token == "" {
		return ExecResult{}, fmt.Errorf("colab: session URL and token are required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	kernelID := e.Session.KernelID
	if kernelID == "" {
		var err error
		kernelID, err = e.createKernel(ctx)
		if err != nil {
			return ExecResult{}, err
		}
		e.Session.KernelID = kernelID
	}
	return e.executeKernel(ctx, kernelID, code, input)
}

func (e *Executor) createKernel(ctx context.Context) (string, error) {
	base, err := runtimeURL(e.Session.URL)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/kernels", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Colab-Client-Agent", clientAgentValue)
	req.Header.Set("X-Colab-Runtime-Proxy-Token", e.Session.Token)
	req.Header.Set("Authorization", "token "+e.Session.Token)

	response, err := e.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("colab: create kernel: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("colab: create kernel: %s", response.Status)
	}
	var kernel struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&kernel); err != nil {
		return "", fmt.Errorf("colab: decode kernel: %w", err)
	}
	if kernel.ID == "" {
		return "", fmt.Errorf("colab: create kernel response missing ID")
	}
	return kernel.ID, nil
}

// Restart recreates the runtime's active Jupyter kernel.
func (e *Executor) Restart(ctx context.Context) error {
	if e == nil || e.Session == nil || e.Session.URL == "" || e.Session.Token == "" {
		return fmt.Errorf("colab: executor has no usable session")
	}
	if e.Session.KernelID == "" {
		return fmt.Errorf("colab: no started kernel to restart")
	}
	base, err := runtimeURL(e.Session.URL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/kernels/"+url.PathEscape(e.Session.KernelID)+"/restart", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Colab-Runtime-Proxy-Token", e.Session.Token)
	req.Header.Set("Authorization", "token "+e.Session.Token)
	response, err := e.Client.Do(req)
	if err != nil {
		return fmt.Errorf("colab: restart kernel: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("colab: restart kernel: %s", response.Status)
	}
	return nil
}

func (e *Executor) executeKernel(ctx context.Context, kernelID, code string, input InputHandler) (ExecResult, error) {
	base, err := runtimeURL(e.Session.URL)
	if err != nil {
		return ExecResult{}, err
	}
	wsURL, err := url.Parse(strings.Replace(base, "https://", "wss://", 1))
	if err != nil {
		return ExecResult{}, err
	}
	if wsURL.Scheme == "http" {
		wsURL.Scheme = "ws"
	}
	wsURL.Path = strings.TrimRight(wsURL.Path, "/") + "/api/kernels/" + url.PathEscape(kernelID) + "/channels"
	sessionID := e.Session.KernelSessionID
	if sessionID == "" {
		sessionID, err = newMessageID()
		if err != nil {
			return ExecResult{}, err
		}
		e.Session.KernelSessionID = sessionID
	}
	q := wsURL.Query()
	q.Set("session_id", sessionID)
	q.Set("token", e.Session.Token)
	q.Set("colab-runtime-proxy-token", e.Session.Token)
	wsURL.RawQuery = q.Encode()

	headers := http.Header{}
	headers.Set("X-Colab-Client-Agent", clientAgentValue)
	headers.Set("X-Colab-Runtime-Proxy-Token", e.Session.Token)
	conn, response, err := websocket.Dial(ctx, wsURL.String(), &websocket.DialOptions{HTTPClient: e.Client, HTTPHeader: headers})
	if err != nil {
		if response != nil {
			return ExecResult{}, fmt.Errorf("colab: connect kernel websocket: %s: %w", response.Status, err)
		}
		return ExecResult{}, fmt.Errorf("colab: connect kernel websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	msgID, err := newMessageID()
	if err != nil {
		return ExecResult{}, err
	}
	message := jupyterMessage{
		Header:       jupyterHeader{MsgID: msgID, Username: "go-colab", Session: sessionID, MsgType: "execute_request", Version: "5.3"},
		ParentHeader: map[string]any{},
		Metadata:     map[string]any{},
		Content: map[string]any{
			"code": code, "silent": false, "store_history": true, "user_expressions": map[string]any{}, "allow_stdin": input != nil, "stop_on_error": true,
		},
		Channel: "shell",
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return ExecResult{}, err
	}
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		return ExecResult{}, fmt.Errorf("colab: send execute request: %w", err)
	}

	var result ExecResult
	for {
		_, body, err := conn.Read(ctx)
		if err != nil {
			return ExecResult{}, fmt.Errorf("colab: read kernel output: %w", err)
		}
		var message jupyterMessage
		if err := json.Unmarshal(body, &message); err != nil {
			return ExecResult{}, fmt.Errorf("colab: decode kernel output: %w", err)
		}
		if message.Header.MsgType == "colab_request" && e.ColabRequestHandler != nil {
			value, handled, err := e.ColabRequestHandler(ctx, message)
			if err != nil {
				return ExecResult{}, err
			}
			if handled {
				requestID, _ := message.Metadata["colab_msg_id"].(string)
				reply := jupyterMessage{Header: jupyterHeader{MsgID: msgID + "-colab", Username: "go-colab", Session: sessionID, MsgType: "input_reply", Version: "5.3"}, ParentHeader: map[string]any{"msg_id": message.Header.MsgID}, Metadata: map[string]any{}, Content: map[string]any{"value": value}, Channel: "stdin"}
				if requestID != "" && value == nil {
					reply.Content["value"] = map[string]any{"type": "colab_reply", "colab_msg_id": requestID}
				}
				payload, err := json.Marshal(reply)
				if err != nil {
					return ExecResult{}, err
				}
				if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
					return ExecResult{}, err
				}
				continue
			}
		}
		if message.ParentHeader["msg_id"] != msgID {
			continue
		}
		if message.Header.MsgType == "input_request" {
			if input == nil {
				return ExecResult{}, fmt.Errorf("colab: runtime requested stdin")
			}
			value, err := input(ctx, stringField(message.Content, "prompt"))
			if err != nil {
				return ExecResult{}, err
			}
			reply := jupyterMessage{Header: jupyterHeader{MsgID: msgID + "-input", Username: "go-colab", Session: msgID, MsgType: "input_reply", Version: "5.3"}, ParentHeader: map[string]any{"msg_id": msgID}, Metadata: map[string]any{}, Content: map[string]any{"value": value}, Channel: "stdin"}
			payload, err := json.Marshal(reply)
			if err != nil {
				return ExecResult{}, err
			}
			if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
				return ExecResult{}, err
			}
			continue
		}
		if err := appendOutput(&result, message); err != nil {
			return ExecResult{}, err
		}
		if message.Header.MsgType == "status" && stringField(message.Content, "execution_state") == "idle" {
			return result, nil
		}
	}
}

type jupyterHeader struct {
	MsgID    string `json:"msg_id"`
	Username string `json:"username"`
	Session  string `json:"session"`
	MsgType  string `json:"msg_type"`
	Version  string `json:"version"`
}

type jupyterMessage struct {
	Header       jupyterHeader  `json:"header"`
	ParentHeader map[string]any `json:"parent_header"`
	Metadata     map[string]any `json:"metadata"`
	Content      map[string]any `json:"content"`
	Channel      string         `json:"channel,omitempty"`
}

func appendOutput(result *ExecResult, message jupyterMessage) error {
	switch message.Header.MsgType {
	case "stream":
		if stringField(message.Content, "name") == "stderr" {
			result.Stderr += stringField(message.Content, "text")
		} else {
			result.Stdout += stringField(message.Content, "text")
		}
	case "error":
		result.Error = stringField(message.Content, "ename") + ": " + stringField(message.Content, "evalue")
	case "execute_reply":
		if stringField(message.Content, "status") == "error" && result.Error == "" {
			result.Error = stringField(message.Content, "ename") + ": " + stringField(message.Content, "evalue")
		}
	case "display_data", "execute_result":
		data, _ := message.Content["data"].(map[string]any)
		image, _ := data["image/png"].(string)
		if image == "" {
			return nil
		}
		decoded, err := base64.StdEncoding.DecodeString(image)
		if err != nil {
			return fmt.Errorf("colab: decode displayed image: %w", err)
		}
		result.Images = append(result.Images, decoded)
	}
	return nil
}

func stringField(values map[string]any, name string) string {
	value, _ := values[name].(string)
	return value
}

func runtimeURL(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("colab: parse runtime endpoint: %w", err)
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	if u.Host == "" {
		return "", fmt.Errorf("colab: runtime endpoint must be a URL")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func newMessageID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("colab: generate message ID: %w", err)
	}
	return hex.EncodeToString(bytes[:]) + fmt.Sprintf("-%d", time.Now().UnixNano()), nil
}
