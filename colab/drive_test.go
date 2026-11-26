package colab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDrivePropagation(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("authtype") != "dfs_ephemeral" || r.URL.Query().Get("dryrun") != "true" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(")]}'\n{\"token\":\"propagation-token\"}"))
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("x-goog-colab-token") != "propagation-token" {
			t.Errorf("unexpected propagation POST")
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	automation := &Automation{Client: NewClientFromHTTP(server.Client(), Environment{Domain: server.URL}), Executor: NewExecutor(&Session{Endpoint: "runtime"}, server.Client())}
	request := jupyterMessage{Metadata: map[string]any{"colab_msg_id": "request-id"}, Content: map[string]any{"request": map[string]any{"authType": "dfs_ephemeral"}}}
	value, handled, err := automation.handleColabRequest(context.Background(), request)
	if err != nil || !handled {
		t.Fatalf("value=%#v handled=%t err=%v", value, handled, err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
	if got, ok := value.(map[string]any); !ok || got["colab_msg_id"] != "request-id" {
		t.Fatalf("value = %#v", value)
	}
}
