package colab

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Content describes an entry returned by the Jupyter Contents API.
type Content struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Type     string    `json:"type"`
	Format   string    `json:"format"`
	Content  any       `json:"content"`
	Size     int64     `json:"size"`
	Created  time.Time `json:"created"`
	Modified time.Time `json:"last_modified"`
}

// ContentsClient accesses a runtime's Jupyter Contents REST API.
type ContentsClient struct {
	Session    *Session
	HTTPClient *http.Client
}

// NewContentsClient creates a Contents REST client for session.
func NewContentsClient(session *Session, client *http.Client) *ContentsClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &ContentsClient{Session: session, HTTPClient: client}
}

// List returns children at path.
func (c *ContentsClient) List(ctx context.Context, path string) ([]Content, error) {
	var response Content
	if err := c.request(ctx, http.MethodGet, path, nil, false, &response); err != nil {
		return nil, err
	}
	if response.Type != "directory" {
		return nil, fmt.Errorf("colab: remote path %q is not a directory", path)
	}
	contents, ok := response.Content.([]any)
	if !ok {
		return nil, fmt.Errorf("colab: invalid directory contents")
	}
	entries := make([]Content, 0, len(contents))
	for _, content := range contents {
		encoded, err := json.Marshal(content)
		if err != nil {
			return nil, err
		}
		var entry Content
		if err := json.Unmarshal(encoded, &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// Delete removes a remote file or directory.
func (c *ContentsClient) Delete(ctx context.Context, path string) error {
	return c.request(ctx, http.MethodDelete, path, nil, false, nil)
}

// Upload copies localPath to remotePath with the Contents API.
func (c *ContentsClient) Upload(ctx context.Context, localPath, remotePath string) error {
	contents, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("colab: read local file: %w", err)
	}
	body := map[string]any{
		"name": filepath.Base(remotePath),
		"path": strings.TrimPrefix(remotePath, "/"),
		"type": "file", "format": "base64",
		"content": base64.StdEncoding.EncodeToString(contents), "chunk": 1,
	}
	return c.request(ctx, http.MethodPut, remotePath, body, false, nil)
}

// Download copies remotePath to localPath with the Contents API.
func (c *ContentsClient) Download(ctx context.Context, remotePath, localPath string) error {
	var response Content
	if err := c.request(ctx, http.MethodGet, remotePath, nil, true, &response); err != nil {
		return err
	}
	if response.Type == "directory" {
		return fmt.Errorf("colab: remote path %q is a directory", remotePath)
	}
	var contents []byte
	switch response.Format {
	case "base64":
		encoded, ok := response.Content.(string)
		if !ok {
			return fmt.Errorf("colab: invalid base64 remote file content")
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return fmt.Errorf("colab: decode remote file: %w", err)
		}
		contents = decoded
	case "json":
		encoded, err := json.MarshalIndent(response.Content, "", "  ")
		if err != nil {
			return fmt.Errorf("colab: encode remote JSON file: %w", err)
		}
		contents = encoded
	default:
		contents = []byte(fmt.Sprint(response.Content))
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(localPath, contents, 0600); err != nil {
		return err
	}
	return nil
}

func (c *ContentsClient) request(ctx context.Context, method, path string, payload any, includeContent bool, output any) error {
	if c == nil || c.Session == nil || c.Session.URL == "" || c.Session.Token == "" {
		return fmt.Errorf("colab: contents API requires runtime URL and token")
	}
	base, err := runtimeURL(c.Session.URL)
	if err != nil {
		return err
	}
	path = strings.Trim(path, "/")
	u, err := url.Parse(base + "/api/contents/" + escapeContentPath(path))
	if err != nil {
		return err
	}
	query := u.Query()
	query.Set("authuser", "0")
	query.Set("colab-runtime-proxy-token", c.Session.Token)
	if includeContent {
		query.Set("content", "1")
	}
	u.RawQuery = query.Encode()

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Colab-Runtime-Proxy-Token", c.Session.Token)
	req.Header.Set("X-Colab-Client-Agent", clientAgentValue)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *c.HTTPClient
	if client.Timeout == 0 {
		client.Timeout = 300 * time.Second
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("colab: contents request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return fmt.Errorf("colab: remote path %q not found", path)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		contents, _ := io.ReadAll(response.Body)
		return &ColabError{StatusCode: response.StatusCode, Message: fmt.Sprintf("%s %s: %s", method, u.Path, response.Status), Body: string(contents)}
	}
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("colab: decode contents response: %w", err)
	}
	return nil
}

func escapeContentPath(path string) string {
	if path == "" {
		return ""
	}
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}
