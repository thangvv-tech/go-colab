package colab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Notebook is a Jupyter notebook version 4 document.
type Notebook struct {
	Cells         []NotebookCell `json:"cells"`
	Metadata      map[string]any `json:"metadata"`
	NBFormat      int            `json:"nbformat"`
	NBFormatMinor int            `json:"nbformat_minor"`
}

// NotebookCell is a Jupyter notebook cell.
type NotebookCell struct {
	CellType       string           `json:"cell_type"`
	ID             string           `json:"id,omitempty"`
	Metadata       map[string]any   `json:"metadata"`
	Source         any              `json:"source"`
	ExecutionCount any              `json:"execution_count,omitempty"`
	Outputs        []NotebookOutput `json:"outputs,omitempty"`
}

// NotebookOutput is a Jupyter output record.
type NotebookOutput struct {
	OutputType string         `json:"output_type"`
	Name       string         `json:"name,omitempty"`
	Text       string         `json:"text,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	EName      string         `json:"ename,omitempty"`
	EValue     string         `json:"evalue,omitempty"`
	Traceback  []string       `json:"traceback,omitempty"`
}

// ExecuteNotebook executes every code cell and writes an output notebook next to inputPath.
func (e *Executor) ExecuteNotebook(ctx context.Context, inputPath string) (string, error) {
	contents, err := os.ReadFile(inputPath)
	if err != nil {
		return "", err
	}
	var notebook Notebook
	if err := json.Unmarshal(contents, &notebook); err != nil {
		return "", fmt.Errorf("colab: parse notebook: %w", err)
	}
	if notebook.NBFormat != 4 {
		return "", fmt.Errorf("colab: unsupported notebook format %d", notebook.NBFormat)
	}
	for index := range notebook.Cells {
		cell := &notebook.Cells[index]
		if cell.CellType != "code" {
			continue
		}
		if cell.ID == "" {
			cell.ID, err = notebookID()
			if err != nil {
				return "", err
			}
		}
		result, err := e.Exec(ctx, sourceString(cell.Source))
		if err != nil {
			return "", err
		}
		cell.Outputs = result.NotebookOutputs()
		if result.Error != "" {
			outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + "_output.ipynb"
			encoded, encodeErr := json.MarshalIndent(notebook, "", "  ")
			if encodeErr == nil {
				_ = os.WriteFile(outputPath, encoded, 0600)
			}
			return outputPath, fmt.Errorf("colab: notebook cell %d: %s", index, result.Error)
		}
	}
	outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + "_output.ipynb"
	encoded, err := json.MarshalIndent(notebook, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(outputPath, encoded, 0600); err != nil {
		return "", err
	}
	return outputPath, nil
}

// NotebookOutputs converts execution output to Jupyter output records.
func (r ExecResult) NotebookOutputs() []NotebookOutput {
	var outputs []NotebookOutput
	if r.Stdout != "" {
		outputs = append(outputs, NotebookOutput{OutputType: "stream", Name: "stdout", Text: r.Stdout})
	}
	if r.Stderr != "" {
		outputs = append(outputs, NotebookOutput{OutputType: "stream", Name: "stderr", Text: r.Stderr})
	}
	for _, image := range r.Images {
		outputs = append(outputs, NotebookOutput{OutputType: "display_data", Data: map[string]any{"image/png": image}, Metadata: map[string]any{}})
	}
	if r.Error != "" {
		name, value, found := strings.Cut(r.Error, ": ")
		if !found {
			name, value = "Error", r.Error
		}
		outputs = append(outputs, NotebookOutput{OutputType: "error", EName: name, EValue: value, Traceback: []string{r.Error}})
	}
	return outputs
}

func sourceString(source any) string {
	switch value := source.(type) {
	case string:
		return value
	case []any:
		var builder strings.Builder
		for _, line := range value {
			builder.WriteString(fmt.Sprint(line))
		}
		return builder.String()
	default:
		return ""
	}
}

func notebookID() (string, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
