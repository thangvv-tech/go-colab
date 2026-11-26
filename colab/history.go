package colab

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HistoryEvent is one immutable Colab operation record.
type HistoryEvent struct {
	Timestamp time.Time      `json:"timestamp"`
	EventType string         `json:"event_type"`
	Data      map[string]any `json:"-"`
}

// HistoryStore persists per-session event history as JSONL.
type HistoryStore struct {
	Dir string
}

// NewHistoryStore creates a history store. Empty path uses the default directory.
func NewHistoryStore(dir string) *HistoryStore {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config", "go-colab", "history")
	}
	return &HistoryStore{Dir: dir}
}

// Append records an event for session.
func (s *HistoryStore) Append(session, event string, data map[string]any) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	payload := make(map[string]any, len(data)+2)
	payload["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	payload["event_type"] = event
	for key, value := range data {
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(s.path(session), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(encoded, '\n'))
	return err
}

// Read returns a session's events in insertion order.
func (s *HistoryStore) Read(session string) ([]map[string]any, error) {
	file, err := os.Open(s.path(session))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var events []map[string]any
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

// Sessions lists sessions with stored history.
func (s *HistoryStore) Sessions() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sessions []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			sessions = append(sessions, strings.TrimSuffix(entry.Name(), ".jsonl"))
		}
	}
	sort.Strings(sessions)
	return sessions, nil
}

// Export converts session history to JSONL, Markdown, plain text, or notebook JSON.
func (s *HistoryStore) Export(session, outputPath string) error {
	events, err := s.Read(session)
	if err != nil {
		return err
	}
	switch strings.ToLower(filepath.Ext(outputPath)) {
	case ".jsonl":
		return copyFile(s.path(session), outputPath)
	case ".md":
		return os.WriteFile(outputPath, []byte(historyMarkdown(session, events)), 0600)
	case ".txt":
		return os.WriteFile(outputPath, []byte(historyText(session, events)), 0600)
	case ".ipynb":
		return exportHistoryNotebook(outputPath, session, events)
	default:
		return fmt.Errorf("colab: history output extension must be .jsonl, .ipynb, .md, or .txt")
	}
}

func (s *HistoryStore) path(session string) string {
	return filepath.Join(s.Dir, filepath.Base(session)+".jsonl")
}

func copyFile(source, destination string) error {
	contents, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		contents = nil
	} else if err != nil {
		return err
	}
	return os.WriteFile(destination, contents, 0600)
}

func historyMarkdown(session string, events []map[string]any) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Colab session: %s\n\n", session)
	for _, event := range events {
		fmt.Fprintf(&builder, "## %v\n\n", event["event_type"])
		if code, ok := event["code"].(string); ok {
			fmt.Fprintf(&builder, "```python\n%s\n```\n\n", code)
		}
	}
	return builder.String()
}

func historyText(session string, events []map[string]any) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Colab session: %s\n====================\n", session)
	for _, event := range events {
		fmt.Fprintf(&builder, "[%v] %v\n", event["timestamp"], event["event_type"])
	}
	return builder.String()
}

func exportHistoryNotebook(path, session string, events []map[string]any) error {
	notebook := Notebook{NBFormat: 4, NBFormatMinor: 5, Metadata: map[string]any{"kernelspec": map[string]any{"name": "python3", "display_name": "Python 3"}}, Cells: []NotebookCell{{CellType: "markdown", Metadata: map[string]any{}, Source: "# Colab session: " + session}}}
	for _, event := range events {
		if code, ok := event["code"].(string); ok {
			notebook.Cells = append(notebook.Cells, NotebookCell{CellType: "code", Metadata: map[string]any{}, Source: code, ExecutionCount: nil, Outputs: []NotebookOutput{}})
		}
	}
	contents, err := json.MarshalIndent(notebook, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, contents, 0600)
}
