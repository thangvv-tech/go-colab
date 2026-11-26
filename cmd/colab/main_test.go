package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thangvv-tech/go-colab/colab"
)

func TestSessionSummaryDoesNotExposeToken(t *testing.T) {
	encoded, err := json.Marshal(sessionSummary(&colab.Session{Name: "smoke", Endpoint: "endpoint", URL: "https://runtime", Token: "secret", Accel: colab.AcceleratorA100}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "token") {
		t.Fatalf("secret leaked: %s", encoded)
	}
}

func TestAssignmentSummaryDoesNotExposeToken(t *testing.T) {
	encoded, err := json.Marshal(assignmentSummary(colab.ListedAssignment{Endpoint: "endpoint", Accelerator: colab.AcceleratorA100, RuntimeProxyInfo: colab.RuntimeProxyInfo{Token: "secret", URL: "https://runtime"}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "token") || strings.Contains(string(encoded), "https://runtime") {
		t.Fatalf("sensitive data leaked: %s", encoded)
	}
}

func TestRuntimeStatusSummaryDoesNotExposeToken(t *testing.T) {
	encoded, err := json.Marshal(runtimeStatusSummary(&colab.RuntimeStatus{Session: &colab.Session{Name: "smoke", Endpoint: "endpoint", Token: "secret"}, Hardware: "CPU", Shape: "Standard", Variant: colab.RuntimeVariantDefault, State: "IDLE"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "token") {
		t.Fatalf("secret leaked: %s", encoded)
	}
}

func TestResolvePublicKeyPrefersPublicSibling(t *testing.T) {
	directory := t.TempDir()
	privatePath := filepath.Join(directory, "key")
	if err := os.WriteFile(privatePath, []byte("-----BEGIN PRIVATE KEY-----\nprivate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(privatePath+".pub", []byte("ssh-ed25519 public-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := resolvePublicKey(privatePath)
	if err != nil || key != "ssh-ed25519 public-key" {
		t.Fatalf("key=%q err=%v", key, err)
	}
}

func TestDefaultAuthIsOAuth2(t *testing.T) {
	root := newRootCommand()
	value, err := root.PersistentFlags().GetString("auth")
	if err != nil {
		t.Fatal(err)
	}
	if value != "oauth2" {
		t.Fatalf("auth default = %q, want oauth2", value)
	}
	if len(defaultOAuthClientConfig) == 0 {
		t.Fatal("bundled OAuth client config is empty")
	}
}
