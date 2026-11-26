package main

import (
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/thangvv-tech/go-colab/colab"
)

func flatFileManager(command *cobra.Command, name string) (*colab.FileManager, error) {
	session, err := sessionStore(command).Resolve(name)
	if err != nil {
		return nil, err
	}
	return colab.NewFileManager(colab.NewExecutor(session, nil)), nil
}

func flatListCommand() *cobra.Command {
	var name string
	command := &cobra.Command{Use: "ls [PATH]", Short: "List remote files", Args: cobra.MaximumNArgs(1), RunE: func(command *cobra.Command, args []string) error {
		path := "/content"
		if len(args) == 1 {
			path = args[0]
		}
		manager, err := flatFileManager(command, name)
		if err != nil {
			return err
		}
		entries, err := manager.List(command.Context(), path)
		if err != nil {
			return err
		}
		return json.NewEncoder(command.OutOrStdout()).Encode(entries)
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	return command
}

func flatUploadCommand() *cobra.Command {
	var name string
	command := &cobra.Command{Use: "upload LOCAL REMOTE", Short: "Upload local file", Args: cobra.ExactArgs(2), RunE: func(command *cobra.Command, args []string) error {
		manager, err := flatFileManager(command, name)
		if err != nil {
			return err
		}
		return manager.Upload(command.Context(), args[0], args[1])
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	return command
}

func flatDownloadCommand() *cobra.Command {
	var name string
	command := &cobra.Command{Use: "download REMOTE LOCAL", Short: "Download remote file", Args: cobra.ExactArgs(2), RunE: func(command *cobra.Command, args []string) error {
		manager, err := flatFileManager(command, name)
		if err != nil {
			return err
		}
		return manager.Download(command.Context(), args[0], args[1])
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	return command
}

func flatRemoveCommand() *cobra.Command {
	var name string
	command := &cobra.Command{Use: "rm PATH", Short: "Delete remote file", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		manager, err := flatFileManager(command, name)
		if err != nil {
			return err
		}
		return manager.Remove(command.Context(), args[0])
	}}
	command.Flags().StringVarP(&name, "session", "s", "", "Local session name")
	return command
}
