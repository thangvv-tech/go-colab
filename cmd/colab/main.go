package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/thangvv-tech/go-colab/colab"
)

var version = "dev"

//go:embed oauth_client.json
var defaultOAuthClientConfig []byte

func main() {
	root := newRootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "colab",
		Short:         "Manage Google Colab runtimes",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("auth", "oauth2", "Authentication mode: oauth2 or adc")
	root.PersistentFlags().String("oauth-config", "", "Path to OAuth client JSON")
	root.PersistentFlags().String("config", "", "Path to local session store")
	root.PersistentFlags().Bool("public-api", false, "Use public runtime listing API")
	root.PersistentFlags().Bool("no-public-api", false, "Force tunnel runtime API")
	root.AddCommand(
		newCommand(), sessionsCommand(), stopCommand(), execCommand(), filesCommand(), usageCommand(),
		statusCommand(), urlCommand(), runCommand(), replCommand(), restartKernelCommand(),
		authCommand(), driveMountCommand(), installCommand(), editCommand(), sshCommand(),
		logCommand(), payCommand(), versionCommand(), updateCommand(),
		flatListCommand(), flatUploadCommand(), flatDownloadCommand(), flatRemoveCommand(),
	)
	return root
}

type sessionInfo struct {
	Name        string            `json:"name"`
	Endpoint    string            `json:"endpoint"`
	Accelerator colab.Accelerator `json:"accelerator"`
	Shape       colab.Shape       `json:"shape"`
}

func sessionSummary(session *colab.Session) sessionInfo {
	return sessionInfo{Name: session.Name, Endpoint: session.Endpoint, Accelerator: session.Accel, Shape: session.Shape}
}

type assignmentInfo struct {
	Endpoint    string                  `json:"endpoint"`
	Accelerator colab.Accelerator       `json:"accelerator"`
	Variant     colab.AssignmentVariant `json:"variant"`
	Shape       colab.Shape             `json:"machineShape"`
}

func assignmentSummary(assignment colab.ListedAssignment) assignmentInfo {
	return assignmentInfo{Endpoint: assignment.Endpoint, Accelerator: assignment.Accelerator, Variant: assignment.Variant, Shape: assignment.MachineShape}
}

func newCommand() *cobra.Command {
	var name, gpu, tpu string
	var highMemory bool
	command := &cobra.Command{
		Use:   "new",
		Short: "Allocate a runtime",
		RunE: func(command *cobra.Command, _ []string) error {
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
			session, err := manager.Assign(command.Context(), colab.AssignOptions{Name: name, Accelerator: accelerator, Variant: variant, HighMemory: highMemory})
			if err != nil {
				return err
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(sessionSummary(session))
		},
	}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	command.Flags().StringVar(&gpu, "gpu", "", "GPU type: T4, L4, A100, H100, G4")
	command.Flags().StringVar(&tpu, "tpu", "", "TPU type: V5E1, V6E1")
	command.Flags().BoolVar(&highMemory, "high-mem", false, "Request high-memory runtime")
	return command
}

func sessionsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List active runtimes",
		RunE: func(command *cobra.Command, _ []string) error {
			manager, err := runtimeManager(command)
			if err != nil {
				return err
			}
			assignments, err := manager.List(command.Context())
			if err != nil {
				return err
			}
			summaries := make([]assignmentInfo, len(assignments))
			for index, assignment := range assignments {
				summaries[index] = assignmentSummary(assignment)
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(summaries)
		},
	}
}

func stopCommand() *cobra.Command {
	var name string
	var all bool
	command := &cobra.Command{
		Use:   "stop",
		Short: "Terminate a runtime",
		RunE: func(command *cobra.Command, _ []string) error {
			manager, err := runtimeManager(command)
			if err != nil {
				return err
			}
			if all {
				return manager.StopAll(command.Context())
			}
			session, err := sessionStore(command).Resolve(name)
			if err != nil {
				return err
			}
			return manager.Unassign(command.Context(), session.Endpoint)
		},
	}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	command.Flags().BoolVar(&all, "all", false, "Stop all local sessions")
	return command
}

func execCommand() *cobra.Command {
	var name, file string
	command := &cobra.Command{
		Use:   "exec [code]",
		Short: "Execute Python code in a runtime",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			code := strings.Join(args, "")
			if file != "" && strings.HasSuffix(strings.ToLower(file), ".ipynb") {
				session, err := sessionStore(command).Resolve(name)
				if err != nil {
					return err
				}
				output, err := colab.NewExecutor(session, nil).ExecuteNotebook(command.Context(), file)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(command.OutOrStdout(), output)
				return err
			}
			if file != "" {
				contents, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				code = string(contents)
			}
			if code == "" {
				contents, err := os.ReadFile("/dev/stdin")
				if err != nil {
					return err
				}
				code = string(contents)
			}
			session, err := sessionStore(command).Resolve(name)
			if err != nil {
				return err
			}
			executor := colab.NewExecutor(session, nil)
			result, err := executor.Exec(command.Context(), code)
			if err != nil {
				return err
			}
			session.LastExecutionAt = time.Now()
			if err := sessionStore(command).Add(session); err != nil {
				return err
			}
			_ = colab.NewHistoryStore("").Append(session.Name, "execution", map[string]any{"code": code, "outputs": result.NotebookOutputs()})
			fmt.Fprint(command.OutOrStdout(), result.Stdout)
			fmt.Fprint(command.ErrOrStderr(), result.Stderr)
			if result.Error != "" {
				return errors.New(result.Error)
			}
			return nil
		},
	}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	command.Flags().StringVarP(&file, "file", "f", "", "Python source file")
	return command
}

func filesCommand() *cobra.Command {
	var name string
	root := &cobra.Command{Use: "files", Short: "Manage runtime files"}
	root.PersistentFlags().StringVarP(&name, "session", "s", "", "Local session name")
	fileManager := func(command *cobra.Command) (*colab.FileManager, error) {
		session, err := sessionStore(command).Resolve(name)
		if err != nil {
			return nil, err
		}
		return colab.NewFileManager(colab.NewExecutor(session, nil)), nil
	}

	root.AddCommand(&cobra.Command{Use: "ls [path]", Args: cobra.MaximumNArgs(1), RunE: func(command *cobra.Command, args []string) error {
		path := "."
		if len(args) == 1 {
			path = args[0]
		}
		manager, err := fileManager(command)
		if err != nil {
			return err
		}
		entries, err := manager.List(command.Context(), path)
		if err != nil {
			return err
		}
		return json.NewEncoder(command.OutOrStdout()).Encode(entries)
	}})
	root.AddCommand(&cobra.Command{Use: "upload LOCAL REMOTE", Args: cobra.ExactArgs(2), RunE: func(command *cobra.Command, args []string) error {
		manager, err := fileManager(command)
		if err != nil {
			return err
		}
		return manager.Upload(command.Context(), args[0], args[1])
	}})
	root.AddCommand(&cobra.Command{Use: "download REMOTE LOCAL", Args: cobra.ExactArgs(2), RunE: func(command *cobra.Command, args []string) error {
		manager, err := fileManager(command)
		if err != nil {
			return err
		}
		return manager.Download(command.Context(), args[0], args[1])
	}})
	root.AddCommand(&cobra.Command{Use: "rm PATH", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		manager, err := fileManager(command)
		if err != nil {
			return err
		}
		return manager.Remove(command.Context(), args[0])
	}})
	return root
}

func usageCommand() *cobra.Command {
	return &cobra.Command{Use: "usage", Short: "Show compute unit usage", RunE: func(command *cobra.Command, _ []string) error {
		client, err := apiClient(command.Context(), command)
		if err != nil {
			return err
		}
		usage, err := client.GetUsage(command.Context())
		if err != nil {
			return err
		}
		return json.NewEncoder(command.OutOrStdout()).Encode(usage)
	}}
}

func runtimeManager(command *cobra.Command) (*colab.RuntimeManager, error) {
	client, err := apiClient(command.Context(), command)
	if err != nil {
		return nil, err
	}
	manager := colab.NewRuntimeManager(client, sessionStore(command))
	usePublic, _ := command.Root().PersistentFlags().GetBool("public-api")
	noPublic, _ := command.Root().PersistentFlags().GetBool("no-public-api")
	if usePublic && noPublic {
		return nil, errors.New("--public-api and --no-public-api cannot be used together")
	}
	if usePublic {
		manager.Mode = colab.APIModePublic
	} else if noPublic {
		manager.Mode = colab.APIModeTunnel
	}
	return manager, nil
}

func apiClient(ctx context.Context, command *cobra.Command) (*colab.Client, error) {
	provider, _ := command.Root().PersistentFlags().GetString("auth")
	oauthConfig, _ := command.Root().PersistentFlags().GetString("oauth-config")
	var auth colab.Authenticator
	switch provider {
	case "adc":
		auth = colab.NewADCAuth()
	case "oauth2":
		if oauthConfig == "" {
			auth = colab.NewOAuth2AuthFromConfig(defaultOAuthClientConfig, "")
		} else {
			auth = colab.NewOAuth2Auth(oauthConfig, "")
		}
	default:
		return nil, fmt.Errorf("unknown auth provider %q", provider)
	}
	return colab.NewClient(ctx, auth, colab.ProdEnv)
}

func sessionStore(command *cobra.Command) *colab.SessionStore {
	path, _ := command.Root().PersistentFlags().GetString("config")
	return colab.NewSessionStore(path)
}
