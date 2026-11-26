package colab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Automation performs common setup operations inside a Colab runtime.
type Automation struct {
	Executor *Executor
	Files    *FileManager
	Client   *Client
	Input    InputHandler
	Notify   func(string)
}

// NewAutomation creates runtime setup operations backed by executor.
func NewAutomation(executor *Executor) *Automation {
	return &Automation{Executor: executor, Files: NewFileManager(executor)}
}

// Authenticate opens Colab's GCP user authentication flow in the runtime.
func (a *Automation) Authenticate(ctx context.Context) error {
	return a.execute(ctx, authenticateCode())
}

func authenticateCode() string {
	return "import os\nos.environ['USE_AUTH_EPHEM'] = '0'\nfrom google.colab import auth\nauth.authenticate_user()"
}

// MountDrive mounts Google Drive at path. Empty path uses /content/drive.
func (a *Automation) MountDrive(ctx context.Context, path string) error {
	if path == "" {
		path = "/content/drive"
	}
	if a.Client == nil {
		return fmt.Errorf("colab: Drive mount requires authenticated client")
	}
	if a.Notify != nil {
		a.Notify("[colab] Starting Google Drive mount; waiting for authorization request...\n")
	}
	a.Executor.ColabRequestHandler = a.handleColabRequest
	return a.execute(ctx, "from google.colab import drive\ndrive.mount("+strconv.Quote(path)+")")
}

func (a *Automation) handleColabRequest(ctx context.Context, message jupyterMessage) (any, bool, error) {
	request, _ := message.Content["request"].(map[string]any)
	if request["authType"] != "dfs_ephemeral" {
		return nil, false, nil
	}
	if a.Client == nil || a.Executor == nil || a.Executor.Session == nil {
		return nil, true, fmt.Errorf("colab: Drive request requires authenticated client and session")
	}
	requestID, _ := message.Metadata["colab_msg_id"].(string)
	if a.Notify != nil {
		a.Notify("[colab] Received Drive authorization request; propagating credentials...\n")
	}
	params := url.Values{"authuser": {"0"}, "authtype": {"dfs_ephemeral"}, "version": {"2"}, "dryrun": {"true"}, "propagate": {"true"}, "record": {"false"}}
	endpoint := strings.TrimRight(a.Client.env.Domain, "/") + "/tun/m/credentials-propagation/" + url.PathEscape(a.Executor.Session.Endpoint)
	if a.Notify != nil {
		a.Notify("[colab] Requesting Drive propagation token...\n")
	}
	response, data, err := a.propagationRequest(ctx, http.MethodGet, endpoint, params, "")
	if err != nil {
		return driveReply(requestID), true, err
	}
	token, _ := data["token"].(string)
	if response.StatusCode != http.StatusOK || token == "" {
		return driveReply(requestID), true, fmt.Errorf("colab: Drive credential propagation request failed: %s", response.Status)
	}
	if a.Notify != nil {
		a.Notify("[colab] Propagating Drive credentials to runtime...\n")
	}
	response, data, err = a.propagationRequest(ctx, http.MethodPost, endpoint, params, token)
	if err != nil {
		return driveReply(requestID), true, err
	}
	if success, _ := data["success"].(bool); !success {
		if uri, _ := data["unauthorized_redirect_uri"].(string); uri != "" && a.Input != nil {
			if a.Notify != nil {
				a.Notify("[colab] Drive consent required. URL follows in prompt.\n")
			}
			if _, err := a.Input(ctx, "Open this Drive authorization URL, grant access, then press Enter:\n"+uri+"\n"); err != nil {
				return driveReply(requestID), true, err
			}
		}
		params.Set("dryrun", "false")
		response, _, err = a.propagationRequest(ctx, http.MethodPost, endpoint, params, token)
		if err != nil {
			return driveReply(requestID), true, err
		}
		if response.StatusCode != http.StatusOK {
			return driveReply(requestID), true, fmt.Errorf("colab: Drive credential propagation failed: %s", response.Status)
		}
	}
	if a.Notify != nil {
		a.Notify("[colab] Drive credentials propagated; resuming mount...\n")
	}
	return driveReply(requestID), true, nil
}

func driveReply(requestID string) map[string]any {
	return map[string]any{"type": "colab_reply", "colab_msg_id": requestID}
}

func (a *Automation) propagationRequest(ctx context.Context, method, endpoint string, params url.Values, token string) (*http.Response, map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var body bytes.Buffer
	var contentType string
	if method == http.MethodPost {
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("file_id", "empty.ipynb"); err != nil {
			return nil, nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, nil, err
		}
		contentType = writer.FormDataContentType()
	}
	var reader *bytes.Reader
	if method == http.MethodPost {
		reader = bytes.NewReader(body.Bytes())
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint+"?"+params.Encode(), reader)
	if err != nil {
		return nil, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("x-goog-colab-token", token)
	}
	response, err := a.Client.HTTPClient().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return response, nil, err
	}
	var data map[string]any
	if len(bodyBytes) > 0 {
		_ = json.Unmarshal([]byte(stripXSSI(string(bodyBytes))), &data)
	}
	if data == nil {
		data = map[string]any{}
	}
	return response, data, nil
}

// Install installs packages with uv then falls back to pip.
func (a *Automation) Install(ctx context.Context, requirementsPath string, packages []string) error {
	if requirementsPath == "" && len(packages) == 0 {
		return fmt.Errorf("colab: package or requirements path is required")
	}
	var arguments []string
	if requirementsPath != "" {
		remote := "/content/" + filepath.Base(requirementsPath)
		if err := a.Files.Upload(ctx, requirementsPath, remote); err != nil {
			return err
		}
		arguments = append(arguments, "-r", remote)
	}
	arguments = append(arguments, packages...)
	quoted := make([]string, len(arguments))
	for index, argument := range arguments {
		quoted[index] = strconv.Quote(argument)
	}
	code := "import subprocess, sys\nargs = [" + strings.Join(quoted, ", ") + "]\nresult = subprocess.run(['uv', 'pip', 'install', '--system', *args])\nif result.returncode != 0: subprocess.check_call([sys.executable, '-m', 'pip', 'install', *args])"
	return a.execute(ctx, code)
}

func (a *Automation) execute(ctx context.Context, code string) error {
	if a == nil || a.Executor == nil {
		return fmt.Errorf("colab: automation requires executor")
	}
	if _, deadline := ctx.Deadline(); !deadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
	}
	result, err := a.Executor.ExecWithInput(ctx, code, a.Input)
	if err != nil {
		return err
	}
	if result.Error != "" {
		return fmt.Errorf("colab: runtime automation: %s", result.Error)
	}
	return nil
}
