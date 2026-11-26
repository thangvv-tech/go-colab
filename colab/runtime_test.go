package colab_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thangvv-tech/go-colab/colab"
)

func TestUUIDToWebSafeBase64(t *testing.T) {
	got, err := colab.UUIDToWebSafeBase64("123e4567-e89b-12d3-a456-426614174000")
	if err != nil {
		t.Fatal(err)
	}
	if want := "123e4567_e89b_12d3_a456_426614174000........"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, id := range []string{"", "123e4567-e89b-12d3-a456-42661417400", "123e4567_e89b_12d3_a456_426614174000", "123e4567-e89b-12d3-a456-42661417400g"} {
		if _, err := colab.UUIDToWebSafeBase64(id); err == nil {
			t.Fatalf("UUIDToWebSafeBase64(%q) succeeded", id)
		}
	}
}

func TestRuntimeManagerList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("authuser") != "0" {
			t.Errorf("authuser = %q", r.URL.Query().Get("authuser"))
		}
		fmt.Fprint(w, ")]}'\n{\"assignments\":[{\"accelerator\":\"A100\",\"endpoint\":\"runtime-1\",\"variant\":1,\"machineShape\":1,\"runtimeProxyInfo\":{\"token\":\"token\",\"tokenExpiresInSeconds\":60,\"url\":\"https://runtime\"}}]}")
	}))
	defer server.Close()

	client := colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL})
	assignments, err := colab.NewRuntimeManager(client, nil).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 1 || assignments[0].Endpoint != "runtime-1" || assignments[0].Accelerator != colab.AcceleratorA100 {
		t.Fatalf("unexpected assignments: %#v", assignments)
	}
}

func TestAssignExisting(t *testing.T) {
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		q := r.URL.Query()
		if q.Get("nbh") != "123e4567_e89b_12d3_a456_426614174000........" || q.Get("variant") != "GPU" || q.Get("accelerator") != "A100" {
			t.Errorf("unexpected query: %s", q.Encode())
		}
		fmt.Fprint(w, `{"endpoint":"existing","runtimeProxyInfo":{"token":"existing-token","tokenExpiresInSeconds":60,"url":"https://runtime"}}`)
	}))
	defer server.Close()

	manager := colab.NewRuntimeManager(colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL}), nil)
	session, err := manager.Assign(context.Background(), colab.AssignOptions{Name: "named", NotebookHash: "123e4567-e89b-12d3-a456-426614174000", Accelerator: colab.AcceleratorA100})
	if err != nil {
		t.Fatal(err)
	}
	if posts != 0 || session.Name != "named" || session.Endpoint != "existing" || session.Token != "existing-token" {
		t.Fatalf("posts=%d session=%#v", posts, session)
	}
}

func TestAssignExistingUsesResponseAccelerator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"accelerator":"L4","endpoint":"existing","runtimeProxyInfo":{"token":"token","tokenExpiresInSeconds":60}}`)
	}))
	defer server.Close()

	session, err := colab.NewRuntimeManager(colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL}), nil).Assign(context.Background(), colab.AssignOptions{NotebookHash: "123e4567-e89b-12d3-a456-426614174000", Accelerator: colab.AcceleratorA100})
	if err != nil {
		t.Fatal(err)
	}
	if session.Accel != colab.AcceleratorL4 {
		t.Fatalf("Accel = %q, want %q", session.Accel, colab.AcceleratorL4)
	}
}

func TestAssignHandshake(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"token":"xsrf"}`)
			return
		}
		if r.Header.Get("X-Goog-Colab-Token") != "xsrf" {
			t.Errorf("XSRF token = %q", r.Header.Get("X-Goog-Colab-Token"))
		}
		if r.URL.Query().Get("shape") != "hm" {
			t.Errorf("shape = %q", r.URL.Query().Get("shape"))
		}
		fmt.Fprint(w, `{"accelerator":"A100","endpoint":"created","variant":1,"runtimeProxyInfo":{"token":"runtime-token","tokenExpiresInSeconds":60,"url":"https://runtime"}}`)
	}))
	defer server.Close()

	store := colab.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	manager := colab.NewRuntimeManager(colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL}), store)
	before := time.Now()
	session, err := manager.Assign(context.Background(), colab.AssignOptions{Name: "train", NotebookHash: "123e4567-e89b-12d3-a456-426614174000", Accelerator: colab.AcceleratorA100, HighMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 2 || methods[0] != http.MethodGet || methods[1] != http.MethodPost {
		t.Fatalf("methods = %v", methods)
	}
	if session.Shape != colab.ShapeHighRAM || session.Accel != colab.AcceleratorA100 || session.TokenExp.Before(before.Add(59*time.Second)) {
		t.Fatalf("unexpected session: %#v", session)
	}
	stored, err := store.Get("train")
	if err != nil || stored == nil || stored.Endpoint != "created" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

func TestAssignRefreshesXSRFTokenAfterRetry(t *testing.T) {
	var methods []string
	getCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodGet {
			getCount++
			fmt.Fprintf(w, `{"token":"token-%d"}`, getCount)
			return
		}
		if r.Header.Get("X-Goog-Colab-Token") != fmt.Sprintf("token-%d", getCount) {
			t.Errorf("xsrf = %q", r.Header.Get("X-Goog-Colab-Token"))
		}
		if getCount == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"endpoint":"created","accelerator":"T4","runtimeProxyInfo":{"token":"runtime","tokenExpiresInSeconds":60,"url":"https://runtime"}}`)
	}))
	defer server.Close()
	manager := colab.NewRuntimeManager(colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL}), nil)
	if _, err := manager.Assign(context.Background(), colab.AssignOptions{NotebookHash: "123e4567-e89b-12d3-a456-426614174000", Accelerator: colab.AcceleratorT4}); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(methods, ","), "GET,POST,GET,POST"; got != want {
		t.Fatalf("methods=%s want=%s", got, want)
	}
}

func TestAssignHighMemoryOmittedForL4(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("shape") {
			t.Errorf("unexpected shape: %q", r.URL.Query().Get("shape"))
		}
		fmt.Fprint(w, `{"endpoint":"existing","runtimeProxyInfo":{"token":"token","tokenExpiresInSeconds":60,"url":"https://runtime"}}`)
	}))
	defer server.Close()

	_, err := colab.NewRuntimeManager(colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL}), nil).Assign(context.Background(), colab.AssignOptions{NotebookHash: "123e4567-e89b-12d3-a456-426614174000", Accelerator: colab.AcceleratorL4, HighMemory: true})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnassign(t *testing.T) {
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"token":"delete-token"}`)
			return
		}
		posts++
		if r.Header.Get("X-Goog-Colab-Token") != "delete-token" {
			t.Errorf("XSRF token = %q", r.Header.Get("X-Goog-Colab-Token"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	store := colab.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err := store.Add(&colab.Session{Name: "named", Endpoint: "abc/def"}); err != nil {
		t.Fatal(err)
	}
	manager := colab.NewRuntimeManager(colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL}), store)
	if err := manager.Unassign(context.Background(), "abc/def"); err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Fatalf("posts = %d", posts)
	}
	stored, err := store.Get("named")
	if err != nil || stored != nil {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

func TestUnassignMissingToken(t *testing.T) {
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	err := colab.NewRuntimeManager(colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL}), nil).Unassign(context.Background(), "endpoint")
	if err == nil || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func TestAssignInvalidNotebookHash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected request")
	}))
	defer server.Close()

	_, err := colab.NewRuntimeManager(colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL}), nil).Assign(context.Background(), colab.AssignOptions{NotebookHash: "bad"})
	if err == nil {
		t.Fatal("expected invalid UUID error")
	}
}

func TestUnassignEscapesEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if escaped := r.URL.EscapedPath(); escaped != "/tun/m/unassign/a%2Fb" {
			t.Errorf("escaped path = %q", escaped)
		}
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"token":"t"}`)
		}
	}))
	defer server.Close()

	endpointURL, _ := url.Parse(server.URL)
	_ = endpointURL
	if err := colab.NewRuntimeManager(colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL}), nil).Unassign(context.Background(), "a/b"); err != nil {
		t.Fatal(err)
	}
}
