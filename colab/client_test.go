package colab_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/thangvv-tech/go-colab/colab"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*colab.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	env := colab.Environment{
		Domain:    srv.URL,
		API:       srv.URL,
		PublicAPI: srv.URL,
	}
	c := colab.NewClientFromHTTP(srv.Client(), env)
	return c, srv
}

// TestClientGetJSON — XSSI stripping tested indirectly here.
func TestClientUsesUpstreamColabAgentHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Colab-Client-Agent"); got != "colab-cli" {
			t.Fatalf("agent header = %q", got)
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	client := colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL})
	if err := client.GetJSON(context.Background(), server.URL, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
}

func TestClientGetJSON(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(")]}'\n{\"key\":\"val\"}"))
	})

	var out map[string]string
	if err := c.GetJSON(t.Context(), srv.URL+"/test", &out); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if out["key"] != "val" {
		t.Fatalf("want key=val, got %v", out)
	}
}

func TestClientNon2xxReturnsColabError(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})

	var out map[string]string
	err := c.GetJSON(t.Context(), srv.URL+"/test", &out)
	if err == nil {
		t.Fatal("expected error")
	}
	var ce *colab.ColabError
	if !errors.As(err, &ce) {
		t.Fatalf("want *ColabError, got %T: %v", err, err)
	}
	if ce.StatusCode != 403 {
		t.Fatalf("want 403, got %d", ce.StatusCode)
	}
}

func TestClientTunnelDomainGetsAuthuser(t *testing.T) {
	var gotAuthuser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthuser = r.URL.Query().Get("authuser")
		w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)

	// Domain == srv.URL so isTunnelDomain matches
	env := colab.Environment{
		Domain:    srv.URL,
		API:       srv.URL,
		PublicAPI: srv.URL,
	}
	c := colab.NewClientFromHTTP(srv.Client(), env)

	var out map[string]any
	if err := c.GetJSON(t.Context(), srv.URL+"/tun/m", &out); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if gotAuthuser != "0" {
		t.Fatalf("want authuser=0, got %q", gotAuthuser)
	}
}

func TestClientPostWithXSRF(t *testing.T) {
	const fakeToken = "xsrf-abc"
	var gotToken string

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]string{"token": fakeToken})
			return
		}
		// POST
		gotToken = r.Header.Get("X-Goog-Colab-Token")
		w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)

	env := colab.Environment{Domain: srv.URL, API: srv.URL, PublicAPI: srv.URL}
	c := colab.NewClientFromHTTP(srv.Client(), env)

	// Step 1: GET to get token
	var getOut map[string]string
	if err := c.GetJSON(t.Context(), srv.URL+"/xsrf", &getOut); err != nil {
		t.Fatalf("GET: %v", err)
	}
	token := getOut["token"]

	// Step 2: POST with token
	var postOut map[string]any
	if err := c.PostWithToken(t.Context(), srv.URL+"/xsrf", token, &postOut); err != nil {
		t.Fatalf("PostWithToken: %v", err)
	}

	if gotToken != fakeToken {
		t.Fatalf("want X-Goog-Colab-Token=%q, got %q", fakeToken, gotToken)
	}
}

func TestTooManyAssignmentsError(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed) // 412
		w.Write([]byte("too many"))
	})

	var out map[string]any
	err := c.GetJSON(t.Context(), srv.URL+"/test", &out)
	if err == nil {
		t.Fatal("expected error")
	}
	var tma *colab.TooManyAssignmentsError
	if !errors.As(err, &tma) {
		t.Fatalf("want *TooManyAssignmentsError, got %T: %v", err, err)
	}
}
