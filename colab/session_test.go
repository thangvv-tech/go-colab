package colab_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thangvv-tech/go-colab/colab"
)

func newStore(t *testing.T) *colab.SessionStore {
	t.Helper()
	return colab.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
}

func makeSession(name string) *colab.Session {
	return &colab.Session{
		Name:      name,
		Endpoint:  "https://example.com/" + name,
		Token:     "tok-" + name,
		TokenExp:  time.Now().Add(time.Hour),
		Shape:     colab.ShapeStandard,
		Accel:     colab.AcceleratorNone,
		CreatedAt: time.Now(),
	}
}

func TestSessionStoreEmptyOnMissing(t *testing.T) {
	store := newStore(t)
	sessions, err := store.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("want empty map, got %d entries", len(sessions))
	}
}

func TestSessionStoreAddAndGet(t *testing.T) {
	store := newStore(t)
	sess := makeSession("foo")
	sess.Accel = colab.AcceleratorT4

	if err := store.Add(sess); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, err := store.Get("foo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("got nil session")
	}
	if got.Name != "foo" {
		t.Errorf("Name: want foo, got %q", got.Name)
	}
	if got.Endpoint != sess.Endpoint {
		t.Errorf("Endpoint mismatch: want %q got %q", sess.Endpoint, got.Endpoint)
	}
	if got.Accel != colab.AcceleratorT4 {
		t.Errorf("Accel: want T4, got %v", got.Accel)
	}
}

func TestSessionStoreRemove(t *testing.T) {
	store := newStore(t)
	if err := store.Add(makeSession("bar")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := store.Remove("bar"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, err := store.Get("bar")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after remove")
	}
}

func TestSessionStoreActive_Single(t *testing.T) {
	store := newStore(t)
	if err := store.Add(makeSession("only")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, err := store.Active()
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if got.Name != "only" {
		t.Errorf("want only, got %q", got.Name)
	}
}

func TestSessionStoreActive_None(t *testing.T) {
	store := newStore(t)
	_, err := store.Active()
	if err == nil {
		t.Fatal("expected error for empty store")
	}
	if !strings.Contains(err.Error(), "no active sessions") {
		t.Errorf("want 'no active sessions' in error, got %q", err.Error())
	}
}

func TestSessionStoreActive_Multiple(t *testing.T) {
	store := newStore(t)
	store.Add(makeSession("a")) //nolint:errcheck
	store.Add(makeSession("b")) //nolint:errcheck
	_, err := store.Active()
	if err == nil {
		t.Fatal("expected error for multiple sessions")
	}
	if !strings.Contains(err.Error(), "multiple") {
		t.Errorf("want 'multiple' in error, got %q", err.Error())
	}
}

func TestSessionStoreResolveByName(t *testing.T) {
	store := newStore(t)
	store.Add(makeSession("x")) //nolint:errcheck
	store.Add(makeSession("y")) //nolint:errcheck
	got, err := store.Resolve("x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name != "x" {
		t.Errorf("want x, got %q", got.Name)
	}
}

func TestSessionStoreResolveEmptyFallback(t *testing.T) {
	store := newStore(t)
	store.Add(makeSession("solo")) //nolint:errcheck
	got, err := store.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name != "solo" {
		t.Errorf("want solo, got %q", got.Name)
	}
}

func TestSessionIsTokenValid(t *testing.T) {
	future := &colab.Session{TokenExp: time.Now().Add(time.Hour)}
	if !future.IsTokenValid() {
		t.Error("future token should be valid")
	}
	past := &colab.Session{TokenExp: time.Now().Add(-time.Hour)}
	if past.IsTokenValid() {
		t.Error("past token should be invalid")
	}
}

func TestSessionStoreAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	store := colab.NewSessionStore(path)

	sessions := map[string]*colab.Session{
		"z": makeSession("z"),
	}
	if err := store.Save(sessions); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// No .tmp file left behind
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error(".tmp file should not exist after atomic write")
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded["z"] == nil {
		t.Fatal("session z not found after round-trip")
	}
	if loaded["z"].Name != "z" {
		t.Errorf("Name mismatch: got %q", loaded["z"].Name)
	}
}
