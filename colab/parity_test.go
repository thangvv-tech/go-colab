package colab

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPublicRuntimeList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/runtimes" || r.URL.RawQuery != "" {
			t.Fatalf("request %s", r.URL.String())
		}
		fmt.Fprint(w, `{"runtimes":[{"name":"runtimes/fallback","connectionInfo":{"expireTime":"not-time","url":"https://runtime","token":"t"},"runtimeSpec":{"variant":"VARIANT_GPU","accelerator":"UNKNOWN","shape":"SHAPE_HIGHMEM"}}]}`)
	}))
	defer server.Close()
	manager := NewRuntimeManager(NewClientFromHTTP(server.Client(), Environment{Domain: "http://other", PublicAPI: server.URL}), nil)
	manager.Mode = APIModePublic
	assignments, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 1 || assignments[0].Endpoint != "fallback" || assignments[0].Accelerator != AcceleratorNone || assignments[0].Variant != VariantGPU || assignments[0].MachineShape != ShapeHighRAM || assignments[0].RuntimeProxyInfo.TokenExpiresIn != 3600 {
		t.Fatalf("unexpected %#v", assignments)
	}
}

func TestPublicModeBlocksLifecycle(t *testing.T) {
	manager := NewRuntimeManager(NewClientFromHTTP(http.DefaultClient, ProdEnv), nil)
	manager.Mode = APIModePublic
	if _, err := manager.Assign(context.Background(), AssignOptions{}); err == nil {
		t.Fatal("expected assign error")
	}
	if err := manager.Unassign(context.Background(), "endpoint"); err == nil {
		t.Fatal("expected unassign error")
	}
}

func TestSettingsStoreMalformedDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if settings := NewSettingsStore(path).Load(); !settings.EnableUpdateCheck {
		t.Fatal("expected default setting")
	}
}

func TestHistoryStoreExport(t *testing.T) {
	store := NewHistoryStore(t.TempDir())
	if err := store.Append("s", "execution", map[string]any{"code": "print(1)"}); err != nil {
		t.Fatal(err)
	}
	for _, extension := range []string{".jsonl", ".md", ".txt", ".ipynb"} {
		if err := store.Export("s", filepath.Join(t.TempDir(), "out"+extension)); err != nil {
			t.Fatalf("%s: %v", extension, err)
		}
	}
}

func TestRunPayloadAndExitCode(t *testing.T) {
	payload := runPayload("script.py", "#!/usr/bin/env python\nprint(1)", []string{"a b"}, map[string]string{"A": "x=y"})
	if !strings.Contains(payload, `sys.argv = ["script.py", "a b"]`) || !strings.Contains(payload, `os.environ["A"] = "x=y"`) || strings.Contains(payload, "#!") {
		t.Fatalf("bad payload %q", payload)
	}
	if exitCode("SystemExit: 3") != 3 || exitCode("SystemExit: 0") != 0 || exitCode("ValueError: x") != 1 {
		t.Fatal("bad exit code")
	}
}

func TestRuntimeStatusRemainsIdleAfterPriorExecution(t *testing.T) {
	store := NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err := store.Add(&Session{Name: "smoke", LastExecutionAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	status, err := (&RuntimeManager{Store: store}).Status(context.Background(), "smoke")
	if err != nil || status.State != "IDLE" {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}

func TestBrowserRuntimeURL(t *testing.T) {
	manager := NewRuntimeManager(NewClientFromHTTP(http.DefaultClient, ProdEnv), nil)
	result, err := manager.RuntimeURL(&Session{Endpoint: "a/b"})
	if err != nil || !strings.Contains(result, "empty.ipynb") || !strings.Contains(result, "dbu=") {
		t.Fatalf("url=%q err=%v", result, err)
	}
}

func TestSettingsSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store := NewSettingsStore(path)
	if err := store.Save(Settings{UsePublicAPI: true, EnableUpdateCheck: true}); err != nil {
		t.Fatal(err)
	}
	if !store.Load().UsePublicAPI {
		t.Fatal("settings did not persist")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions %o", info.Mode().Perm())
	}
}
