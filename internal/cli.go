package internal

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func Main(ctx context.Context, args []string) int {
	if isVersionArgList(args) {
		fmt.Fprintln(os.Stdout, VersionString())
		return 0
	}
	app, err := NewApp(os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := app.Execute(ctx, args); err != nil {
		if code, ok := ExitCode(err); ok {
			return code
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func (a *App) Execute(ctx context.Context, args []string) error {
	if err := a.Prepare(ctx); err != nil {
		return err
	}
	// Build the command tree before routing so the reserved-name set is
	// derived from the real cobra commands rather than a parallel list that
	// can drift when subcommands are added.
	root := a.rootCommand(ctx)
	a.reserved = collectReservedNames(root)
	if a.shouldRunProject(args) {
		return a.RunProject(ctx, args[0], args[1:])
	}
	root.SetArgs(args)
	return root.ExecuteContext(ctx)
}

// collectReservedNames walks the cobra command tree and returns the set of
// names and aliases that ark uses as subcommands. Any of these would shadow
// a project name on the command line.
func collectReservedNames(root *cobra.Command) map[string]struct{} {
	reserved := map[string]struct{}{}
	for _, cmd := range root.Commands() {
		reserved[cmd.Name()] = struct{}{}
		for _, alias := range cmd.Aliases {
			reserved[alias] = struct{}{}
		}
	}
	return reserved
}

func (a *App) rootCommand(ctx context.Context) *cobra.Command {
	var printVersion bool
	root := &cobra.Command{
		Use:           "ark",
		Short:         "Isolated development containers per project",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if printVersion {
				fmt.Fprintln(a.out, VersionString())
				return nil
			}
			if len(args) > 0 {
				return a.RunProject(cmd.Context(), args[0], args[1:])
			}
			return a.RunDefault(cmd.Context())
		},
	}
	root.SetIn(a.in)
	root.SetOut(a.out)
	root.SetErr(a.errOut)
	root.Flags().BoolVarP(&printVersion, "version", "v", false, "print version and build number")

	root.AddCommand(a.initCommand())
	root.AddCommand(a.startCommand())
	root.AddCommand(a.stopCommand())
	root.AddCommand(a.removeCommand())
	root.AddCommand(a.listCommand())
	root.AddCommand(a.tempCommand())
	root.AddCommand(a.configCommand())
	root.AddCommand(a.imageCommand())
	root.AddCommand(a.rebuildCommand())
	root.AddCommand(a.doctorCommand())
	root.AddCommand(a.updateCommand())
	root.AddCommand(a.versionCommand())
	return root
}

func (a *App) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and build number",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(a.out, VersionString())
			return nil
		},
	}
}

func (a *App) initCommand() *cobra.Command {
	opts := InitOptions{
		Runtime:       a.config.Runtime,
		SSHEnabled:    a.config.Init.SSH,
		DockerEnabled: a.config.Init.Docker && a.config.Docker.Enabled,
		Enter:         a.config.Init.Enter,
	}
	var noSSH bool
	var noDocker bool
	cmd := &cobra.Command{
		Use:   "init <name>",
		Short: "Create a persistent project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.SSHEnabled = a.config.Init.SSH && !noSSH
			opts.DockerEnabled = a.config.Init.Docker && a.config.Docker.Enabled && !noDocker
			return a.InitProject(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Runtime, "runtime", a.config.Runtime, "runtime: auto, apple, or docker")
	cmd.Flags().BoolVarP(&opts.AssumeYes, "yes", "y", false, "skip confirmation prompts")
	cmd.Flags().BoolVar(&noSSH, "no-ssh", false, "disable Git-over-SSH broker support for this project")
	cmd.Flags().BoolVar(&noDocker, "no-docker", false, "disable Docker-in-container for this project")
	return cmd
}

func (a *App) startCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Start a project and enter it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.StartProject(cmd.Context(), args[0], true)
		},
	}
}

func (a *App) stopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.StopProject(cmd.Context(), args[0])
		},
	}
}

func (a *App) removeCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove"},
		Short:   "Remove a project container and volumes",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.RemoveProject(cmd.Context(), args[0], force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "remove without prompting")
	return cmd
}

func (a *App) listCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListProjects(cmd.Context())
		},
	}
}

func (a *App) tempCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "temp [cmd...]",
		Short: "Run an ephemeral container",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("ark temp is not implemented in the Docker lifecycle MVP")
		},
	}
}

func (a *App) configCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Ark config",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(a.out, a.paths.ConfigFile)
			return nil
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print config file path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(a.out, a.paths.ConfigFile)
			return nil
		},
	})
	var force bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create a sample config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := WriteDefaultConfig(a.paths, force); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Wrote %s\n", a.paths.ConfigFile)
			return nil
		},
	}
	initCmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite existing config")
	cmd.AddCommand(initCmd)
	return cmd
}

func (a *App) doctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check local setup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.Doctor(cmd.Context())
		},
	}
}

func (a *App) shouldRunProject(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if _, reserved := a.reserved[args[0]]; reserved {
		return false
	}
	if isHelpArg(args[0]) || isVersionArg(args[0]) {
		return false
	}
	return true
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func isVersionArg(arg string) bool {
	return arg == "--version" || arg == "-v"
}

func isVersionArgList(args []string) bool {
	return len(args) == 1 && (isVersionArg(args[0]) || args[0] == "version")
}
