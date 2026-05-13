package internal

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func Main(ctx context.Context, args []string) int {
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
	if len(args) > 0 && !isKnownCommand(args[0]) && args[0] != "--help" && args[0] != "-h" {
		return a.RunProject(ctx, args[0], args[1:])
	}
	root := a.rootCommand(ctx)
	root.SetArgs(args)
	return root.ExecuteContext(ctx)
}

func (a *App) rootCommand(ctx context.Context) *cobra.Command {
	root := &cobra.Command{
		Use:           "ark",
		Short:         "Isolated development containers per project",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return a.RunProject(cmd.Context(), args[0], args[1:])
			}
			return a.RunDefault(cmd.Context())
		},
	}
	root.SetIn(a.in)
	root.SetOut(a.out)
	root.SetErr(a.errOut)

	root.AddCommand(a.initCommand())
	root.AddCommand(a.startCommand())
	root.AddCommand(a.stopCommand())
	root.AddCommand(a.removeCommand())
	root.AddCommand(a.listCommand())
	root.AddCommand(a.tempCommand())
	root.AddCommand(a.codeCommand())
	root.AddCommand(a.doctorCommand())
	return root
}

func (a *App) initCommand() *cobra.Command {
	opts := InitOptions{
		Runtime:       RuntimeAuto,
		SSHEnabled:    true,
		DockerEnabled: true,
	}
	var noSSH bool
	var noDocker bool
	cmd := &cobra.Command{
		Use:   "init <name>",
		Short: "Create a persistent project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.SSHEnabled = !noSSH
			opts.DockerEnabled = !noDocker
			return a.InitProject(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Runtime, "runtime", RuntimeAuto, "runtime: auto, apple, or docker")
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

func (a *App) codeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "code <name>",
		Short: "Print/open VS Code Remote-SSH target",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.Code(cmd.Context(), args[0])
		},
	}
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

func isKnownCommand(arg string) bool {
	_, ok := reservedProjectNames[arg]
	return ok
}
