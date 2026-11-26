package colab

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"nhooyr.io/websocket"
)

// SSHMode selects Colab SSH bridge behavior.
type SSHMode string

const (
	// SSHModeInteractive starts a shell session bridge.
	SSHModeInteractive SSHMode = "interactive"
	// SSHModeProxy connects an OpenSSH ProxyCommand bridge.
	SSHModeProxy SSHMode = "proxy"
	// SSHModeCommand connects a one-shot command bridge.
	SSHModeCommand SSHMode = "cmd"
)

// SSHBridge relays raw SSH bytes over Colab's runtime WebSocket.
type SSHBridge struct {
	Session    *Session
	HTTPClient *http.Client
	PublicKey  string
	Mode       SSHMode
}

// Bridge connects stdin to the Colab SSH WebSocket and relays received bytes to stdout.
func (s *SSHBridge) Bridge(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if s == nil || s.Session == nil || s.Session.URL == "" || s.Session.Token == "" {
		return fmt.Errorf("colab: SSH bridge requires runtime URL and token")
	}
	if s.PublicKey == "" {
		return fmt.Errorf("colab: SSH bridge requires public key")
	}
	base, err := runtimeURL(s.Session.URL)
	if err != nil {
		return err
	}
	wsURL, err := url.Parse(strings.Replace(strings.Replace(base, "https://", "wss://", 1), "http://", "ws://", 1))
	if err != nil {
		return err
	}
	wsURL.Path = strings.TrimRight(wsURL.Path, "/") + "/colab/ssh"
	query := wsURL.Query()
	query.Add("colab-runtime-proxy-token", s.Session.Token)
	query.Add("colab-client-agent", "go-colab")
	query.Add("colab-client-agent", "ssh-mode-"+string(s.Mode))
	wsURL.RawQuery = query.Encode()
	headers := http.Header{}
	headers.Set("X-Colab-Ssh-Pubkey", s.PublicKey)
	headers.Set("X-Colab-Client-Agent", clientAgentValue)
	client := s.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	connection, response, err := websocket.Dial(ctx, wsURL.String(), &websocket.DialOptions{HTTPClient: client, HTTPHeader: headers})
	if err != nil {
		if response != nil {
			return fmt.Errorf("colab: SSH bridge connect: %s", response.Status)
		}
		return fmt.Errorf("colab: SSH bridge connect: %w", err)
	}
	defer connection.Close(websocket.StatusNormalClosure, "")

	writeErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 8192)
		for {
			count, readErr := stdin.Read(buffer)
			if count > 0 {
				if err := connection.Write(ctx, websocket.MessageBinary, buffer[:count]); err != nil {
					writeErr <- err
					return
				}
			}
			if readErr != nil {
				if readErr == io.EOF {
					writeErr <- nil
				} else {
					writeErr <- readErr
				}
				return
			}
		}
	}()
	for {
		kind, data, err := connection.Read(ctx)
		if err != nil {
			select {
			case inputErr := <-writeErr:
				return inputErr
			default:
				if websocket.CloseStatus(err) != -1 {
					return nil
				}
				return err
			}
		}
		if kind == websocket.MessageBinary || kind == websocket.MessageText {
			if _, err := stdout.Write(data); err != nil {
				return err
			}
		}
	}
}
