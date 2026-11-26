package colab

import "testing"

func TestPyString(t *testing.T) {
	if got, want := pyString("a'\n\\b"), `"a'\n\\b"`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFileManagerRequiresExecutor(t *testing.T) {
	manager := NewFileManager(nil)
	if _, err := manager.List(t.Context(), "/content"); err == nil {
		t.Fatal("expected error")
	}
	if err := manager.Upload(t.Context(), "local", "remote"); err == nil {
		t.Fatal("expected error")
	}
	if err := manager.Download(t.Context(), "remote", "local"); err == nil {
		t.Fatal("expected error")
	}
	if err := manager.Remove(t.Context(), "remote"); err == nil {
		t.Fatal("expected error")
	}
}
