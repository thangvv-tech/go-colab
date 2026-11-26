package colab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentsUpload(t *testing.T) {
	local := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.EscapedPath() != "/api/contents/a%20b/file.txt" {
			t.Fatalf("request %s %s", r.Method, r.URL.EscapedPath())
		}
		if r.URL.Query().Get("authuser") != "0" || r.URL.Query().Get("colab-runtime-proxy-token") != "token" {
			t.Fatal("missing proxy query")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["format"] != "base64" || body["path"] != "a b/file.txt" {
			t.Fatalf("body %#v", body)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	client := NewContentsClient(&Session{URL: server.URL, Token: "token"}, server.Client())
	if err := client.Upload(context.Background(), local, "a b/file.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestContentsDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("content") != "1" {
			t.Fatal("missing content query")
		}
		_, _ = w.Write([]byte(`{"type":"file","format":"base64","content":"aGVsbG8="}`))
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "nested", "out")
	client := NewContentsClient(&Session{URL: server.URL, Token: "token"}, server.Client())
	if err := client.Download(context.Background(), "file", output); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(output)
	if err != nil || string(contents) != "hello" {
		t.Fatalf("contents=%q err=%v", contents, err)
	}
}

func TestContentsDownloadText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"type":"file","format":"text","content":"hello text"}`))
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "out")
	client := NewContentsClient(&Session{URL: server.URL, Token: "token"}, server.Client())
	if err := client.Download(context.Background(), "file", output); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(output)
	if err != nil || string(contents) != "hello text" {
		t.Fatalf("contents=%q err=%v", contents, err)
	}
}

func TestContentsDownloadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"type":"notebook","format":"json","content":{"cells":[]}}`))
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "out.ipynb")
	client := NewContentsClient(&Session{URL: server.URL, Token: "token"}, server.Client())
	if err := client.Download(context.Background(), "notebook.ipynb", output); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(output)
	if err != nil || string(contents) != "{\n  \"cells\": []\n}" {
		t.Fatalf("contents=%q err=%v", contents, err)
	}
}

func TestEscapeContentPath(t *testing.T) {
	if got := escapeContentPath("a b/c?d"); got != "a%20b/c%3Fd" {
		t.Fatalf("%q", got)
	}
	if strings.Contains(escapeContentPath("a/b"), "%2F") {
		t.Fatal("escaped separator")
	}
}
