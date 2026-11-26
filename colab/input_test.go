package colab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nhooyr.io/websocket"
)

func TestExecWithInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/kernels" {
			_, _ = w.Write([]byte(`{"id":"kernel"}`))
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		_, raw, err := conn.Read(context.Background())
		if err != nil {
			t.Error(err)
			return
		}
		var request jupyterMessage
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Error(err)
			return
		}
		if request.Content["allow_stdin"] != true {
			t.Error("allow_stdin false")
		}
		input := jupyterMessage{Header: jupyterHeader{MsgType: "input_request"}, ParentHeader: map[string]any{"msg_id": request.Header.MsgID}, Content: map[string]any{"prompt": "Code: "}}
		encoded, _ := json.Marshal(input)
		if err := conn.Write(context.Background(), websocket.MessageText, encoded); err != nil {
			t.Error(err)
			return
		}
		_, raw, err = conn.Read(context.Background())
		if err != nil {
			t.Error(err)
			return
		}
		var reply jupyterMessage
		if err := json.Unmarshal(raw, &reply); err != nil {
			t.Error(err)
			return
		}
		if reply.Header.MsgType != "input_reply" || reply.Content["value"] != "answer" {
			t.Errorf("reply %#v", reply)
		}
		idle := jupyterMessage{Header: jupyterHeader{MsgType: "status"}, ParentHeader: map[string]any{"msg_id": request.Header.MsgID}, Content: map[string]any{"execution_state": "idle"}}
		encoded, _ = json.Marshal(idle)
		if err := conn.Write(context.Background(), websocket.MessageText, encoded); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	executor := NewExecutor(&Session{URL: server.URL, Token: "token"}, server.Client())
	_, err := executor.ExecWithInput(t.Context(), "input()", func(_ context.Context, prompt string) (string, error) {
		if prompt != "Code: " {
			t.Errorf("prompt %q", prompt)
		}
		return "answer", nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
