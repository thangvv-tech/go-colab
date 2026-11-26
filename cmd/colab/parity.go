package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/thangvv-tech/go-colab/colab"
)

type runtimeStatusInfo struct {
	Session  sessionInfo          `json:"session"`
	Hardware string               `json:"hardware"`
	Shape    string               `json:"shape"`
	Variant  colab.RuntimeVariant `json:"variant"`
	State    string               `json:"state"`
}

func runtimeStatusSummary(status *colab.RuntimeStatus) runtimeStatusInfo {
	return runtimeStatusInfo{Session: sessionSummary(status.Session), Hardware: status.Hardware, Shape: status.Shape, Variant: status.Variant, State: status.State}
}

func statusCommand() *cobra.Command {
	var name string
	command := &cobra.Command{Use: "status", Short: "Show runtime status", RunE: func(command *cobra.Command, _ []string) error {
		manager := &colab.RuntimeManager{Store: sessionStore(command)}
		status, err := manager.Status(command.Context(), name)
		if err != nil {
			return err
		}
		return json.NewEncoder(command.OutOrStdout()).Encode(runtimeStatusSummary(status))
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	return command
}

func urlCommand() *cobra.Command {
	var name string
	var open bool
	command := &cobra.Command{Use: "url", Short: "Print runtime browser URL", RunE: func(command *cobra.Command, _ []string) error {
		manager, err := runtimeManager(command)
		if err != nil {
			return err
		}
		session, err := sessionStore(command).Resolve(name)
		if err != nil {
			return err
		}
		url, err := manager.RuntimeURL(session)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintln(command.OutOrStdout(), url); err != nil {
			return err
		}
		if open {
			return openBrowser(url)
		}
		return nil
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	command.Flags().BoolVar(&open, "open", false, "Open URL in default browser")
	return command
}

func runCommand() *cobra.Command {
	var name, gpu, tpu string
	var highMemory, keep bool
	var timeout time.Duration
	var environment []string
	command := &cobra.Command{Use: "run SCRIPT [ARGS...]", Short: "Run a script on an ephemeral runtime", Args: cobra.MinimumNArgs(1), RunE: func(command *cobra.Command, args []string) error {
		manager, err := runtimeManager(command)
		if err != nil {
			return err
		}
		if gpu != "" && tpu != "" {
			return errors.New("--gpu and --tpu cannot be used together")
		}
		accelerator, variant := colab.AcceleratorNone, colab.RuntimeVariantDefault
		if gpu != "" {
			accelerator, variant = colab.Accelerator(strings.ToUpper(gpu)), colab.RuntimeVariantGPU
		}
		if tpu != "" {
			accelerator, variant = colab.Accelerator(strings.ToUpper(tpu)), colab.RuntimeVariantTPU
		}
		result, err := manager.Run(command.Context(), colab.RunOptions{Assign: colab.AssignOptions{Name: name, Accelerator: accelerator, Variant: variant, HighMemory: highMemory}, ScriptPath: args[0], Args: args[1:], Env: environment, Keep: keep, Timeout: timeout})
		if err != nil {
			return err
		}
		fmt.Fprint(command.OutOrStdout(), result.Result.Stdout)
		fmt.Fprint(command.ErrOrStderr(), result.Result.Stderr)
		if result.ExitCode != 0 {
			return fmt.Errorf("remote script exited with code %d", result.ExitCode)
		}
		return nil
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	command.Flags().StringVar(&gpu, "gpu", "", "GPU type")
	command.Flags().StringVar(&tpu, "tpu", "", "TPU type")
	command.Flags().BoolVar(&highMemory, "high-mem", false, "Request high-memory runtime")
	command.Flags().BoolVar(&keep, "keep", false, "Keep allocated runtime")
	command.Flags().DurationVar(&timeout, "timeout", 0, "Execution timeout; zero disables timeout")
	command.Flags().StringSliceVar(&environment, "env", nil, "Environment KEY=VALUE; repeatable")
	return command
}

func replCommand() *cobra.Command {
	var name string
	command := &cobra.Command{Use: "repl", Short: "Run an interactive Python REPL", RunE: func(command *cobra.Command, _ []string) error {
		session, err := sessionStore(command).Resolve(name)
		if err != nil {
			return err
		}
		executor := colab.NewExecutor(session, nil)
		if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice == 0 {
			contents, err := os.ReadFile("/dev/stdin")
			if err != nil {
				return err
			}
			if len(contents) == 0 {
				return nil
			}
			result, err := executor.Exec(command.Context(), string(contents))
			if err != nil {
				return err
			}
			fmt.Fprint(command.OutOrStdout(), result.Stdout)
			fmt.Fprint(command.ErrOrStderr(), result.Stderr)
			if result.Error != "" {
				return errors.New(result.Error)
			}
			return nil
		}
		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Fprint(command.OutOrStdout(), ">>> ")
			if !scanner.Scan() {
				return scanner.Err()
			}
			code := scanner.Text()
			if code == "/quit" || code == "quit()" || code == "exit()" {
				return nil
			}
			if strings.TrimSpace(code) == "" {
				continue
			}
			result, err := executor.Exec(command.Context(), code)
			if err != nil {
				return err
			}
			fmt.Fprint(command.OutOrStdout(), result.Stdout)
			fmt.Fprint(command.ErrOrStderr(), result.Stderr)
			if result.Error != "" {
				fmt.Fprintln(command.ErrOrStderr(), result.Error)
			}
		}
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	return command
}

func automationCommand() *cobra.Command {
	var name string
	root := &cobra.Command{Use: "automation", Short: "Configure runtime services"}
	root.PersistentFlags().StringVarP(&name, "session", "s", "", "Local session name")
	automation := func(command *cobra.Command) (*colab.Automation, error) {
		session, err := sessionStore(command).Resolve(name)
		if err != nil {
			return nil, err
		}
		return colab.NewAutomation(colab.NewExecutor(session, nil)), nil
	}
	root.AddCommand(&cobra.Command{Use: "auth", Short: "Authenticate runtime for GCP", RunE: func(command *cobra.Command, _ []string) error {
		a, err := automation(command)
		if err != nil {
			return err
		}
		return a.Authenticate(command.Context())
	}})
	var drivePath string
	drive := &cobra.Command{Use: "drivemount", Short: "Mount Google Drive", RunE: func(command *cobra.Command, _ []string) error {
		a, err := automation(command)
		if err != nil {
			return err
		}
		return a.MountDrive(command.Context(), drivePath)
	}}
	drive.Flags().StringVar(&drivePath, "path", "/content/drive", "Mount path")
	root.AddCommand(drive)
	var requirements string
	install := &cobra.Command{Use: "install [PACKAGE...]", Short: "Install runtime packages", RunE: func(command *cobra.Command, args []string) error {
		a, err := automation(command)
		if err != nil {
			return err
		}
		return a.Install(command.Context(), requirements, args)
	}}
	install.Flags().StringVarP(&requirements, "requirements", "r", "", "Requirements file")
	root.AddCommand(install)
	return root
}

func automationFor(command *cobra.Command, session *colab.Session) *colab.Automation {
	automation := colab.NewAutomation(colab.NewExecutor(session, nil))
	automation.Notify = func(message string) { fmt.Fprint(command.ErrOrStderr(), message) }
	automation.Input = func(_ context.Context, prompt string) (string, error) {
		if prompt == "" {
			prompt = "[colab] Runtime is waiting for input. Complete any browser authorization, then press Enter: "
		}
		fmt.Fprint(command.ErrOrStderr(), prompt)
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return "", scanner.Err()
		}
		return scanner.Text(), nil
	}
	return automation
}

func rootAutomationCommand(name, action string) *cobra.Command {
	command := automationCommand()
	command.SetArgs([]string{action, "--session", name})
	return command
}

func authCommand() *cobra.Command {
	var name string
	command := &cobra.Command{Use: "auth", Short: "Authenticate runtime for GCP", RunE: func(command *cobra.Command, _ []string) error {
		session, err := sessionStore(command).Resolve(name)
		if err != nil {
			return err
		}
		if err := automationFor(command, session).Authenticate(command.Context()); err != nil {
			return err
		}
		return sessionStore(command).Add(session)
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	return command
}

func driveMountCommand() *cobra.Command {
	var name, path string
	command := &cobra.Command{Use: "drivemount", Short: "Mount Google Drive", RunE: func(command *cobra.Command, _ []string) error {
		session, err := sessionStore(command).Resolve(name)
		if err != nil {
			return err
		}
		automation := automationFor(command, session)
		client, err := apiClient(command.Context(), command)
		if err != nil {
			return err
		}
		automation.Client = client
		if err := automation.MountDrive(command.Context(), path); err != nil {
			return err
		}
		return sessionStore(command).Add(session)
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	command.Flags().StringVar(&path, "path", "/content/drive", "Mount path")
	return command
}

func installCommand() *cobra.Command {
	var name, requirements string
	command := &cobra.Command{Use: "install [PACKAGE...]", Short: "Install runtime packages", RunE: func(command *cobra.Command, args []string) error {
		session, err := sessionStore(command).Resolve(name)
		if err != nil {
			return err
		}
		return automationFor(command, session).Install(command.Context(), requirements, args)
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	command.Flags().StringVarP(&requirements, "requirements", "r", "", "Requirements file")
	return command
}

func editCommand() *cobra.Command {
	var name string
	command := &cobra.Command{Use: "edit PATH", Short: "Edit remote file with $EDITOR", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		session, err := sessionStore(command).Resolve(name)
		if err != nil {
			return err
		}
		return editRemoteFile(command.Context(), colab.NewFileManager(colab.NewExecutor(session, nil)), args[0])
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	return command
}

func restartKernelCommand() *cobra.Command {
	var name string
	command := &cobra.Command{Use: "restart-kernel", Short: "Restart runtime kernel", RunE: func(command *cobra.Command, _ []string) error {
		store := sessionStore(command)
		session, err := store.Resolve(name)
		if err != nil {
			return err
		}
		if err := colab.NewExecutor(session, nil).Restart(command.Context()); err != nil {
			return err
		}
		return store.Add(session)
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	return command
}

func sshCommand() *cobra.Command {
	var name, identity string
	var proxyMode bool
	command := &cobra.Command{Use: "ssh [-- COMMAND...]", Short: "Open SSH session to runtime", RunE: func(command *cobra.Command, args []string) error {
		session, err := sessionStore(command).Resolve(name)
		if err != nil {
			return err
		}
		publicKey, err := resolvePublicKey(identity)
		if err != nil {
			return err
		}
		mode := colab.SSHModeInteractive
		if proxyMode {
			mode = colab.SSHModeProxy
		}
		if len(args) > 0 {
			mode = colab.SSHModeCommand
		}
		bridge := &colab.SSHBridge{Session: session, PublicKey: publicKey, Mode: mode}
		if proxyMode {
			return bridge.Bridge(command.Context(), os.Stdin, command.OutOrStdout())
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		configPath, _ := command.Root().PersistentFlags().GetString("config")
		if configPath == "" {
			configPath = colab.DefaultSessionStorePath()
		}
		proxyArgs := []string{executable, "ssh", "--proxy-mode", "--session", name, "--config", configPath}
		if identity != "" {
			proxyArgs = append(proxyArgs, "--identity", identity)
		}
		quoted := make([]string, len(proxyArgs))
		for index, argument := range proxyArgs {
			quoted[index] = shellQuote(argument)
		}
		sshArgs := []string{"-o", "ProxyCommand=" + strings.Join(quoted, " "), "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR"}
		if identity != "" {
			sshArgs = append(sshArgs, "-i", strings.TrimSuffix(identity, ".pub"))
		}
		sshArgs = append(sshArgs, "root@colab-runtime")
		if len(args) > 0 {
			sshArgs = append(sshArgs, args...)
		}
		localSSH := exec.CommandContext(command.Context(), "ssh", sshArgs...)
		localSSH.Stdin, localSSH.Stdout, localSSH.Stderr = os.Stdin, command.OutOrStdout(), command.ErrOrStderr()
		return localSSH.Run()
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	command.Flags().StringVarP(&identity, "identity", "i", "", "SSH public or private key path")
	command.Flags().BoolVar(&proxyMode, "proxy-mode", false, "Run raw stdio ProxyCommand bridge")
	return command
}

func shellQuote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n'\"\\$`;&|<>()[]{}*?!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\\"'\\\"'") + "'"
}

func resolvePublicKey(identity string) (string, error) {
	candidates := []string{}
	if identity != "" {
		if strings.HasSuffix(identity, ".pub") {
			candidates = append(candidates, identity)
		} else {
			candidates = append(candidates, identity+".pub", identity)
		}
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		candidates = []string{filepath.Join(home, ".ssh", "id_ed25519.pub"), filepath.Join(home, ".ssh", "id_ecdsa.pub")}
	}
	for _, candidate := range candidates {
		contents, err := os.ReadFile(candidate)
		if err == nil && strings.TrimSpace(string(contents)) != "" && strings.HasSuffix(candidate, ".pub") {
			return strings.TrimSpace(string(contents)), nil
		}
	}
	if identity != "" && !strings.HasSuffix(identity, ".pub") {
		output, err := exec.Command("ssh-keygen", "-y", "-f", identity).Output()
		if err == nil && strings.TrimSpace(string(output)) != "" {
			return strings.TrimSpace(string(output)), nil
		}
	}
	return "", errors.New("no usable SSH public key found; pass --identity")
}

func logCommand() *cobra.Command {
	var name, output string
	command := &cobra.Command{Use: "log", Short: "View or export session history", RunE: func(command *cobra.Command, _ []string) error {
		store := colab.NewHistoryStore("")
		if output != "" {
			return store.Export(name, output)
		}
		events, err := store.Read(name)
		if err != nil {
			return err
		}
		return json.NewEncoder(command.OutOrStdout()).Encode(events)
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Session name")
	command.Flags().StringVarP(&output, "output", "o", "", "Export path (.jsonl, .ipynb, .md, .txt)")
	return command
}

func payCommand() *cobra.Command {
	var open bool
	command := &cobra.Command{Use: "pay", Short: "Print Colab subscription URL", RunE: func(command *cobra.Command, _ []string) error {
		if _, err := fmt.Fprintln(command.OutOrStdout(), colab.PaymentURL); err != nil {
			return err
		}
		if open {
			return openBrowser(colab.PaymentURL)
		}
		return nil
	}}
	command.Flags().BoolVar(&open, "open", false, "Open URL in default browser")
	return command
}

func openBrowser(rawURL string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", rawURL)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		command = exec.Command("xdg-open", rawURL)
	}
	return command.Run()
}
func versionCommand() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print CLI version", Run: func(command *cobra.Command, _ []string) { fmt.Fprintf(command.OutOrStdout(), "Version: %s\n", version) }}
}

func updateCommand() *cobra.Command {
	var endpoint string
	command := &cobra.Command{Use: "update", Short: "Check configured go-colab release endpoint", RunE: func(command *cobra.Command, _ []string) error {
		if endpoint == "" {
			fmt.Fprintln(command.OutOrStdout(), "No go-colab release endpoint configured. Upgrade with: go install github.com/thangvv-tech/go-colab/cmd/colab@latest")
			return nil
		}
		ctx, cancel := context.WithTimeout(command.Context(), 5*time.Second)
		defer cancel()
		latest, err := colab.CheckLatestVersion(ctx, nil, endpoint)
		if err != nil {
			return err
		}
		fmt.Fprintf(command.OutOrStdout(), "Latest go-colab version: %s\n", latest)
		return nil
	}}
	command.Flags().StringVar(&endpoint, "release-endpoint", "", "Go-colab release JSON endpoint")
	return command
}

func editRemoteFile(ctx context.Context, manager *colab.FileManager, remotePath string) error {
	temporary, err := os.CreateTemp("", "go-colab-edit-*")
	if err != nil {
		return err
	}
	localPath := temporary.Name()
	temporary.Close()
	defer os.Remove(localPath)
	if err := manager.Download(ctx, remotePath, localPath); err != nil {
		return err
	}
	before, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	editorArgs := strings.Fields(editor)
	if len(editorArgs) == 0 {
		return errors.New("EDITOR is empty")
	}
	command := exec.Command(editorArgs[0], append(editorArgs[1:], localPath)...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return err
	}
	after, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	if string(before) != string(after) {
		return manager.Upload(ctx, localPath, remotePath)
	}
	return nil
}

var _ = filepath.Base
var _ = runtime.GOOS
