package colab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckLatestVersionTopLevel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"version":"1.2.3"}`)) }))
	defer server.Close()
	version, err := CheckLatestVersion(context.Background(), server.Client(), server.URL)
	if err != nil || version != "1.2.3" {
		t.Fatalf("version=%q err=%v", version, err)
	}
}
