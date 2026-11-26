package colab

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileEntry describes a file in a Colab runtime filesystem.
type FileEntry struct {
	Path  string
	IsDir bool
	Size  int64
}

// FileManager operates on a Colab runtime filesystem through its kernel.
type FileManager struct {
	Executor *Executor
	Contents *ContentsClient
}

// NewFileManager creates a file manager using executor.
func NewFileManager(executor *Executor) *FileManager {
	manager := &FileManager{Executor: executor}
	if executor != nil {
		manager.Contents = NewContentsClient(executor.Session, executor.Client)
	}
	return manager
}

// List returns direct children of path.
func (m *FileManager) List(ctx context.Context, path string) ([]FileEntry, error) {
	if m == nil || m.Executor == nil {
		return nil, fmt.Errorf("colab: file manager has no executor")
	}
	if m.Contents != nil {
		contents, err := m.Contents.List(ctx, path)
		if err != nil {
			return nil, err
		}
		entries := make([]FileEntry, 0, len(contents))
		for _, content := range contents {
			entries = append(entries, FileEntry{Path: content.Path, IsDir: content.Type == "directory", Size: content.Size})
		}
		return entries, nil
	}
	result, err := m.Executor.Exec(ctx, "import os\nfor e in os.scandir("+pyString(path)+"):\n print(e.path, int(e.is_dir()), e.stat().st_size, sep='\\t')")
	if err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, fmt.Errorf("colab: list remote files: %s", result.Error)
	}
	var entries []FileEntry
	for _, line := range strings.Split(strings.TrimSuffix(result.Stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("colab: decode remote file entry %q", line)
		}
		size, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("colab: decode remote file size: %w", err)
		}
		entries = append(entries, FileEntry{Path: parts[0], IsDir: parts[1] == "1", Size: size})
	}
	return entries, nil
}

// Upload writes localPath to remotePath. Files are base64 encoded in memory.
func (m *FileManager) Upload(ctx context.Context, localPath, remotePath string) error {
	if m == nil || m.Executor == nil {
		return fmt.Errorf("colab: file manager has no executor")
	}
	if m.Contents != nil {
		return m.Contents.Upload(ctx, localPath, remotePath)
	}
	contents, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("colab: read local file: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(contents)
	code := "import base64, os\nos.makedirs(os.path.dirname(" + pyString(remotePath) + ") or '.', exist_ok=True)\nwith open(" + pyString(remotePath) + ", 'wb') as f: f.write(base64.b64decode(" + pyString(encoded) + "))"
	result, err := m.Executor.Exec(ctx, code)
	if err != nil {
		return err
	}
	if result.Error != "" {
		return fmt.Errorf("colab: upload remote file: %s", result.Error)
	}
	return nil
}

// Download copies remotePath to localPath. Files are held in memory.
func (m *FileManager) Download(ctx context.Context, remotePath, localPath string) error {
	if m == nil || m.Executor == nil {
		return fmt.Errorf("colab: file manager has no executor")
	}
	if m.Contents != nil {
		return m.Contents.Download(ctx, remotePath, localPath)
	}
	result, err := m.Executor.Exec(ctx, "import base64\nprint(base64.b64encode(open("+pyString(remotePath)+", 'rb').read()).decode())")
	if err != nil {
		return err
	}
	if result.Error != "" {
		return fmt.Errorf("colab: download remote file: %s", result.Error)
	}
	contents, err := base64.StdEncoding.DecodeString(strings.TrimSpace(result.Stdout))
	if err != nil {
		return fmt.Errorf("colab: decode downloaded file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("colab: create local directory: %w", err)
	}
	if err := os.WriteFile(localPath, contents, 0600); err != nil {
		return fmt.Errorf("colab: write local file: %w", err)
	}
	return nil
}

// Remove deletes path from the remote filesystem.
func (m *FileManager) Remove(ctx context.Context, path string) error {
	if m == nil || m.Executor == nil {
		return fmt.Errorf("colab: file manager has no executor")
	}
	if m.Contents != nil {
		return m.Contents.Delete(ctx, path)
	}
	result, err := m.Executor.Exec(ctx, "import os, shutil\np="+pyString(path)+"\n(shutil.rmtree(p) if os.path.isdir(p) else os.remove(p))")
	if err != nil {
		return err
	}
	if result.Error != "" {
		return fmt.Errorf("colab: remove remote file: %s", result.Error)
	}
	return nil
}

func pyString(value string) string {
	return strconv.Quote(value)
}
