package colab

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RunOptions configures an ephemeral Colab script run.
type RunOptions struct {
	Assign     AssignOptions
	ScriptPath string
	Args       []string
	Env        []string
	Keep       bool
	Timeout    time.Duration
}

// RunResult contains a run's session, execution output, and process-style exit code.
type RunResult struct {
	Session  *Session
	Result   ExecResult
	ExitCode int
}

// Run allocates a runtime, executes a local Python script, then releases it unless Keep is true.
func (m *RuntimeManager) Run(ctx context.Context, options RunOptions) (_ *RunResult, resultErr error) {
	if options.ScriptPath == "" {
		return nil, fmt.Errorf("colab: script path is required")
	}
	contents, err := os.ReadFile(options.ScriptPath)
	if err != nil {
		return nil, fmt.Errorf("colab: read script: %w", err)
	}
	environment, err := parseEnvironment(options.Env)
	if err != nil {
		return nil, err
	}
	session, err := m.Assign(ctx, options.Assign)
	if err != nil {
		return nil, err
	}
	if !options.Keep {
		defer func() {
			cleanupErr := m.Unassign(context.WithoutCancel(ctx), session.Endpoint)
			if cleanupErr != nil {
				resultErr = errors.Join(resultErr, cleanupErr)
			}
		}()
	}
	payload := runPayload(filepath.Base(options.ScriptPath), string(contents), options.Args, environment)
	executionContext := ctx
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		executionContext, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	result, err := NewExecutor(session, nil).Exec(executionContext, payload)
	if err != nil {
		return nil, err
	}
	if options.Keep && m.Store != nil {
		if err := m.Store.Add(session); err != nil {
			return nil, fmt.Errorf("colab: persist kept session: %w", err)
		}
	}
	return &RunResult{Session: session, Result: result, ExitCode: exitCode(result.Error)}, nil
}

func parseEnvironment(entries []string) (map[string]string, error) {
	environment := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, found := strings.Cut(entry, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("colab: environment value %q must be KEY=VALUE", entry)
		}
		environment[key] = value
	}
	return environment, nil
}

func runPayload(name, script string, args []string, environment map[string]string) string {
	var builder strings.Builder
	builder.WriteString("import os, sys\n")
	for key, value := range environment {
		builder.WriteString("os.environ[")
		builder.WriteString(strconv.Quote(key))
		builder.WriteString("] = ")
		builder.WriteString(strconv.Quote(value))
		builder.WriteString("\n")
	}
	builder.WriteString("sys.argv = [")
	builder.WriteString(strconv.Quote(name))
	for _, arg := range args {
		builder.WriteString(", ")
		builder.WriteString(strconv.Quote(arg))
	}
	builder.WriteString("]\n__name__ = '__main__'\n")
	if strings.HasPrefix(script, "#!") {
		if newline := strings.IndexByte(script, '\n'); newline >= 0 {
			script = script[newline+1:]
		} else {
			script = ""
		}
	}
	builder.WriteString(script)
	return builder.String()
}

func exitCode(remoteError string) int {
	if remoteError == "" {
		return 0
	}
	if strings.HasPrefix(remoteError, "SystemExit:") {
		value := strings.TrimSpace(strings.TrimPrefix(remoteError, "SystemExit:"))
		if value == "" || value == "None" || value == "0" {
			return 0
		}
		if code, err := strconv.Atoi(value); err == nil {
			return code
		}
	}
	return 1
}
