package colab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nhooyr.io/websocket"
)

func TestAppendOutput(t *testing.T) {
	var result ExecResult
	image := base64.StdEncoding.EncodeToString([]byte("png"))
	messages := []jupyterMessage{
		{Header: jupyterHeader{MsgType: "stream"}, Content: map[string]any{"name": "stdout", "text": "out"}},
		{Header: jupyterHeader{MsgType: "stream"}, Content: map[string]any{"name": "stderr", "text": "err"}},
		{Header: jupyterHeader{MsgType: "display_data"}, Content: map[string]any{"data": map[string]any{"image/png": image}}},
		{Header: jupyterHeader{MsgType: "error"}, Content: map[string]any{"ename": "ValueError", "evalue": "bad"}},
	}
	for _, message := range messages {
		if err := appendOutput(&result, message); err != nil {
			t.Fatal(err)
		}
	}
	if result.Stdout != "out" || result.Stderr != "err" || result.Error != "ValueError: bad" || string(result.Images[0]) != "png" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRuntimeURL(t *testing.T) {
	got, err := runtimeURL("http://localhost:8080/proxy/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://localhost:8080/proxy" {
		t.Fatalf("got %q", got)
	}
	if _, err := runtimeURL("not a URL"); err == nil {
		t.Fatal("expected invalid URL error")
	}
}

func TestExecRequiresSession(t *testing.T) {
	if _, err := (&Executor{}).Exec(t.Context(), "print(1)"); err == nil {
		t.Fatal("expected missing session error")
	}
}

func TestExecutorExec(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/kernels" {
			if r.Method != http.MethodPost || r.Header.Get("X-Colab-Runtime-Proxy-Token") != "token" {
				t.Errorf("invalid create request")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"kernel"}`))
			return
		}
		if r.URL.Path != "/api/kernels/kernel/channels" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("colab-runtime-proxy-token") != "token" {
			t.Errorf("missing runtime proxy token query")
		}
		if r.URL.Query().Get("session_id") == "" {
			t.Errorf("missing Jupyter session ID query")
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		_, body, err := conn.Read(context.Background())
		if err != nil {
			t.Error(err)
			return
		}
		var request jupyterMessage
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
			return
		}
		if request.Content["code"] != "print('ok')" {
			t.Errorf("code = %#v", request.Content["code"])
		}
		for _, message := range []jupyterMessage{
			{Header: jupyterHeader{MsgType: "stream"}, ParentHeader: map[string]any{"msg_id": request.Header.MsgID}, Content: map[string]any{"name": "stdout", "text": "ok\n"}},
			{Header: jupyterHeader{MsgType: "status"}, ParentHeader: map[string]any{"msg_id": request.Header.MsgID}, Content: map[string]any{"execution_state": "idle"}},
		} {
			payload, _ := json.Marshal(message)
			if err := conn.Write(context.Background(), websocket.MessageText, payload); err != nil {
				t.Error(err)
				return
			}
		}
	}))
	defer server.Close()

	result, err := NewExecutor(&Session{URL: server.URL, Token: "token"}, server.Client()).Exec(t.Context(), "print('ok')")
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "ok\n" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}
