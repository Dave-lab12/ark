package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

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
		if len(args) >= 2 && isHelpArg(args[1]) {
			return a.printProjectHelp(ctx, args[0])
		}
		cmdArgs, ports, err := parsePortOptionsFromArgs(args[1:])
		if err != nil {
			return err
		}
		return a.RunProject(ctx, args[0], cmdArgs, ports)
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
	var ports PortOptions
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
			normalizedPorts, err := normalizePortOptions(ports)
			if err != nil {
				return err
			}
			if len(args) > 0 {
				return a.RunProject(cmd.Context(), args[0], args[1:], normalizedPorts)
			}
			return a.RunDefault(cmd.Context(), normalizedPorts)
		},
	}
	root.SetIn(a.in)
	root.SetOut(a.out)
	root.SetErr(a.errOut)
	root.AddGroup(
		&cobra.Group{ID: "projects", Title: "PROJECTS"},
		&cobra.Group{ID: "ark", Title: "ARK"},
	)
	root.SetHelpTemplate(helpTemplate)
	root.Flags().BoolVarP(&printVersion, "version", "v", false, "print version and build number")
	addPortFlags(root, &ports)

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
		Use:     "version",
		Short:   "print version",
		GroupID: "ark",
		Args:    cobra.NoArgs,
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
		Use:     "init <name>",
		Short:   "create a project",
		GroupID: "projects",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.SSHEnabled = a.config.Init.SSH && !noSSH
			opts.DockerEnabled = a.config.Init.Docker && a.config.Docker.Enabled && !noDocker
			ports, err := normalizePortOptions(opts.Ports)
			if err != nil {
				return err
			}
			opts.Ports = ports
			return a.InitProject(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Runtime, "runtime", a.config.Runtime, "runtime: auto, apple, or docker")
	cmd.Flags().BoolVar(&noSSH, "no-ssh", false, "disable Git-over-SSH broker support for this project")
	cmd.Flags().BoolVar(&noDocker, "no-docker", false, "disable Docker-in-container for this project")
	addPortFlags(cmd, &opts.Ports)
	return cmd
}

func (a *App) startCommand() *cobra.Command {
	var ports PortOptions
	cmd := &cobra.Command{
		Use:     "start <name>",
		Short:   "start a project without entering",
		GroupID: "projects",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ports, err := normalizePortOptions(ports)
			if err != nil {
				return err
			}
			return a.StartProject(cmd.Context(), args[0], false, ports)
		},
	}
	addPortFlags(cmd, &ports)
	return cmd
}

func (a *App) stopCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "stop <name>",
		Short:   "stop a project",
		GroupID: "projects",
		Args:    cobra.ExactArgs(1),
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
		Short:   "delete a project and its volumes",
		GroupID: "projects",
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
		Use:     "ls",
		Short:   "list projects",
		GroupID: "projects",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListProjects(cmd.Context())
		},
	}
}

func (a *App) tempCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "temp [cmd...]",
		Short:   "Run an ephemeral container",
		GroupID: "projects",
		Hidden:  true,
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("ark temp is not implemented in the Docker lifecycle MVP")
		},
	}
}

func (a *App) configCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Short:   "show or edit ark config",
		GroupID: "ark",
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
		Use:     "doctor",
		Short:   "check local setup",
		GroupID: "ark",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.Doctor(cmd.Context())
		},
	}
}

const helpTemplate = `{{if .Long}}{{.Long}}{{else}}{{.Short}}{{end}}

USAGE
  ark <name> [args...]          enter or run in a project
  ark <command> [args...]       manage projects and ark itself

{{- range .Groups}}
{{$group := .}}
{{.Title}}{{range $.Commands}}{{if and (eq .GroupID $group.ID) (not .Hidden)}}
  {{rpad .Name 12}}{{.Short}}{{end}}{{end}}
{{- end}}

INSIDE A PROJECT
  <name>                       enter the project shell
  <name> <cmd...>              run a command in the project
  <name> --port 3000           add a port (sticky across stop/start)
  <name> --port -3000          remove a port
  <name> --port =3000,8080:80  replace all ports
  <name> --ports               show this project's ports
  <name> --no-ports            clear all ports

"ark <command> --help" for details on any command.
`

func (a *App) shouldRunProject(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if strings.HasPrefix(args[0], "-") {
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

func addPortFlags(cmd *cobra.Command, ports *PortOptions) {
	cmd.Flags().StringSliceVar(&ports.Specs, "port", nil,
		"expose ports; use --port 3000, --port -3000 to remove, --port =3000 to replace")
	cmd.Flags().BoolVar(&ports.Clear, "no-ports", false, "remove all configured ports")
	cmd.Flags().BoolVar(&ports.List, "ports", false, "list current ports without changing them")
}

func parsePortOptionsFromArgs(args []string) ([]string, PortOptions, error) {
	var cmdArgs []string
	var ports PortOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			cmdArgs = append(cmdArgs, args[i+1:]...)
			i = len(args)
		case arg == "--port":
			if i+1 >= len(args) {
				return nil, ports, errors.New("flag needs an argument: --port")
			}
			ports.Specs = append(ports.Specs, args[i+1])
			i++
		case strings.HasPrefix(arg, "--port="):
			ports.Specs = append(ports.Specs, strings.TrimPrefix(arg, "--port="))
		case arg == "--no-ports":
			ports.Clear = true
		case arg == "--ports":
			ports.List = true
		default:
			cmdArgs = append(cmdArgs, arg)
		}
	}
	normalized, err := normalizePortOptions(ports)
	if err != nil {
		return nil, normalized, err
	}
	return cmdArgs, normalized, nil
}
