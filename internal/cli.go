package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const (
	flagPort     = "port"
	flagNoPorts  = "no-ports"
	flagPorts    = "ports"
	flagMemory   = "memory"
	flagNoMemory = "no-memory"
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
		cmdArgs, projectOpts, err := parseProjectOptionsFromArgs(args[1:])
		if err != nil {
			return err
		}
		return a.RunProjectWithOptions(ctx, args[0], cmdArgs, projectOpts)
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
	var memory MemoryOptions
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
			projectOpts, err := normalizeProjectOptions(ProjectOptions{Ports: ports, Memory: memory})
			if err != nil {
				return err
			}
			if len(args) > 0 {
				return a.RunProjectWithOptions(cmd.Context(), args[0], args[1:], projectOpts)
			}
			return a.RunDefaultWithOptions(cmd.Context(), projectOpts)
		},
	}
	root.SetIn(a.in)
	root.SetOut(a.out)
	root.SetErr(a.errOut)
	root.AddGroup(
		&cobra.Group{ID: "projects", Title: "PROJECTS"},
		&cobra.Group{ID: "ark", Title: "ARK"},
	)
	root.SetHelpFunc(a.helpFunc)
	root.Flags().BoolVarP(&printVersion, "version", "v", false, "print version and build number")
	addPortFlags(root, &ports)
	addMemoryFlags(root, &memory)

	root.AddCommand(a.initCommand())
	root.AddCommand(a.startCommand())
	root.AddCommand(a.editCommand())
	root.AddCommand(a.stopCommand())
	root.AddCommand(a.removeCommand())
	root.AddCommand(a.listCommand())
	root.AddCommand(a.statsCommand())
	root.AddCommand(a.networkCommand())
	root.AddCommand(a.tempCommand())
	root.AddCommand(a.configCommand())
	root.AddCommand(a.imageCommand())
	root.AddCommand(a.devcontainerCommand())
	root.AddCommand(a.rebuildCommand())
	root.AddCommand(a.doctorCommand())
	root.AddCommand(a.updateCommand())
	root.AddCommand(a.versionCommand())
	root.AddCommand(a.gitBrokerCommand())
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

func (a *App) gitBrokerCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "git-broker <name>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.RunGitBroker(cmd.Context(), args[0])
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
			memory, err := normalizeMemoryOptions(opts.Memory)
			if err != nil {
				return err
			}
			opts.Memory = memory
			return a.InitProject(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Runtime, "runtime", a.config.Runtime, "runtime: auto, apple, or docker")
	cmd.Flags().BoolVar(&noSSH, "no-ssh", false, "disable Git-over-SSH broker support for this project")
	cmd.Flags().BoolVar(&noDocker, "no-docker", false, "disable Docker-in-container for this project")
	addPortFlags(cmd, &opts.Ports)
	addMemoryFlags(cmd, &opts.Memory)
	return cmd
}

func (a *App) devcontainerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "devcontainer",
		Short:   "manage devcontainer.json generation",
		GroupID: "ark",
	}
	var inProject bool
	writeCmd := &cobra.Command{
		Use:   "write <name>",
		Short: "write the devcontainer.json for a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.DevcontainerWrite(cmd.Context(), args[0], inProject)
		},
	}
	writeCmd.Flags().BoolVar(&inProject, "in-project", false,
		"write into <project>/.devcontainer/ instead of ~/.ark/devcontainers/")
	cmd.AddCommand(writeCmd)
	return cmd
}

func (a *App) startCommand() *cobra.Command {
	var ports PortOptions
	var memory MemoryOptions
	cmd := &cobra.Command{
		Use:     "start <name>",
		Short:   "start a project without entering",
		GroupID: "projects",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectOpts, err := normalizeProjectOptions(ProjectOptions{Ports: ports, Memory: memory})
			if err != nil {
				return err
			}
			return a.StartProjectWithOptions(cmd.Context(), args[0], false, projectOpts)
		},
	}
	addPortFlags(cmd, &ports)
	addMemoryFlags(cmd, &memory)
	return cmd
}

func (a *App) editCommand() *cobra.Command {
	var opts EditOptions
	var editor string
	cmd := &cobra.Command{
		Use:     "edit <name>",
		Short:   "open a project in your editor",
		GroupID: "projects",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.EditorOverride = editor
			ports, err := normalizePortOptions(opts.Ports)
			if err != nil {
				return err
			}
			opts.Ports = ports
			return a.EditProject(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&editor, "editor", "", "editor binary to launch (overrides [editor].default)")
	cmd.Flags().StringVar(&opts.Folder, "folder", "", "subdirectory inside the container to open (relative to workdir, or absolute)")
	addPortFlags(cmd, &opts.Ports)
	addMemoryFlags(cmd, &opts.Memory)
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

func (a *App) statsCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "stats [project...]",
		Short:   "show live project CPU and RAM usage",
		GroupID: "projects",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListProjectStats(cmd.Context(), args)
		},
	}
}

func (a *App) networkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "network",
		Short:   "manage project network groups",
		GroupID: "projects",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "list network groups",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.ListNetworkGroups(cmd.Context())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "create <group>",
		Short: "create a network group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.CreateNetworkGroup(cmd.Context(), args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "add <group> <project...>",
		Short: "add projects to a network group",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.AddProjectsToNetworkGroup(cmd.Context(), args[0], args[1:])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:     "remove <group> <project...>",
		Aliases: []string{"rm"},
		Short:   "remove projects from a network group",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.RemoveProjectsFromNetworkGroup(cmd.Context(), args[0], args[1:])
		},
	})
	return cmd
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
		Short: "print config file path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(a.out, a.paths.ConfigFile)
			return nil
		},
	})
	var force bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "create a sample config file",
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

type helpStyle struct {
	color bool
}

func (s helpStyle) heading(text string) string {
	if !s.color {
		return text
	}
	return "\x1b[1;36m" + text + "\x1b[0m"
}

func (s helpStyle) command(text string) string {
	if !s.color {
		return text
	}
	return "\x1b[1;32m" + text + "\x1b[0m"
}

func (a *App) helpFunc(cmd *cobra.Command, _ []string) {
	style := helpStyle{color: a.helpColorEnabled()}
	out := cmd.OutOrStdout()
	if cmd.Root() == cmd {
		printRootHelp(out, cmd, style)
		return
	}
	printCommandHelp(out, cmd, style)
}

func (a *App) helpColorEnabled() bool {
	if !a.isInteractive() {
		return false
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return true
}

func printRootHelp(out io.Writer, root *cobra.Command, style helpStyle) {
	fmt.Fprintf(out, "%s\n\n", commandSummary(root))
	fmt.Fprintf(out, "%s\n", style.heading("USAGE"))
	fmt.Fprintln(out, "  ark <name> [args...]          enter or run in a project")
	fmt.Fprintln(out, "  ark <command> [args...]       manage projects and ark itself")

	for _, group := range root.Groups() {
		fmt.Fprintf(out, "\n%s\n", style.heading(group.Title))
		for _, cmd := range root.Commands() {
			if cmd.GroupID != group.ID || !cmd.IsAvailableCommand() {
				continue
			}
			fmt.Fprintf(out, "  %s%s\n", style.command(padRight(cmd.Name(), 12)), cmd.Short)
			for _, sub := range visibleSubcommands(cmd) {
				fmt.Fprintf(out, "    %s%s\n", style.command(padRight(sub.Name(), 10)), sub.Short)
			}
		}
	}

	fmt.Fprintf(out, "\n%s\n", style.heading("INSIDE A PROJECT"))
	fmt.Fprintln(out, "  <name>                       enter the project shell")
	fmt.Fprintln(out, "  <name> <cmd...>              run a command in the project")
	fmt.Fprintln(out, "  <name> --port 3000           add a port (sticky across stop/start)")
	fmt.Fprintln(out, "  <name> --port -3000          remove a port")
	fmt.Fprintln(out, "  <name> --port =3000,8080:80  replace all ports")
	fmt.Fprintln(out, "  <name> --ports               show this project's ports")
	fmt.Fprintln(out, "  <name> --no-ports            clear all ports")
	fmt.Fprintln(out, "  <name> --memory 4g           set container memory limit")
	fmt.Fprintln(out, "  <name> --no-memory           clear container memory limit")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, `"ark <command> --help" for details on any command.`)
}

func printCommandHelp(out io.Writer, cmd *cobra.Command, style helpStyle) {
	fmt.Fprintf(out, "%s\n\n", commandSummary(cmd))
	fmt.Fprintf(out, "%s\n", style.heading("USAGE"))
	if cmd.HasAvailableSubCommands() {
		if cmd.Runnable() {
			fmt.Fprintf(out, "  %s [command]\n", cmd.CommandPath())
		} else {
			fmt.Fprintf(out, "  %s <command>\n", cmd.CommandPath())
		}
	} else {
		fmt.Fprintf(out, "  %s\n", cmd.UseLine())
	}

	if subcommands := visibleSubcommands(cmd); len(subcommands) > 0 {
		fmt.Fprintf(out, "\n%s\n", style.heading("COMMANDS"))
		width := commandNameWidth(subcommands)
		for _, sub := range subcommands {
			fmt.Fprintf(out, "  %s%s\n", style.command(padRight(sub.Name(), width)), sub.Short)
		}

		fmt.Fprintf(out, "\n%s\n", style.heading("EXAMPLES"))
		for _, sub := range subcommands {
			fmt.Fprintf(out, "  %s %s\n", cmd.CommandPath(), sub.Name())
		}
	}

	printFlagHelp(out, cmd, style)

	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(out, "\n%q for details on a subcommand.\n", cmd.CommandPath()+" <command> --help")
	}
}

func printFlagHelp(out io.Writer, cmd *cobra.Command, style helpStyle) {
	if cmd.HasAvailableLocalFlags() {
		fmt.Fprintf(out, "\n%s\n%s\n", style.heading("FLAGS"), strings.TrimRight(cmd.LocalFlags().FlagUsages(), "\n"))
	}
	if cmd.HasAvailableInheritedFlags() {
		fmt.Fprintf(out, "\n%s\n%s\n", style.heading("GLOBAL FLAGS"), strings.TrimRight(cmd.InheritedFlags().FlagUsages(), "\n"))
	}
}

func commandSummary(cmd *cobra.Command) string {
	if cmd.Long != "" {
		return cmd.Long
	}
	return cmd.Short
}

func visibleSubcommands(cmd *cobra.Command) []*cobra.Command {
	subcommands := []*cobra.Command{}
	for _, sub := range cmd.Commands() {
		if sub.IsAvailableCommand() {
			subcommands = append(subcommands, sub)
		}
	}
	return subcommands
}

func commandNameWidth(commands []*cobra.Command) int {
	width := 12
	for _, cmd := range commands {
		if len(cmd.Name())+2 > width {
			width = len(cmd.Name()) + 2
		}
	}
	return width
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s + "  "
	}
	return s + strings.Repeat(" ", width-len(s))
}

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
	cmd.Flags().StringSliceVar(&ports.Specs, flagPort, nil,
		"expose ports (sticky); --port 3000 adds, --port -3000 removes, "+
			"--port =3000 replaces; comma-separated tokens are independent")
	cmd.Flags().BoolVar(&ports.Clear, flagNoPorts, false, "remove all configured ports")
	cmd.Flags().BoolVar(&ports.List, flagPorts, false, "list current ports without changing them")
}

func addMemoryFlags(cmd *cobra.Command, memory *MemoryOptions) {
	cmd.Flags().StringVar(&memory.Limit, flagMemory, "", "set container memory limit, for example 4g")
	cmd.Flags().BoolVar(&memory.Clear, flagNoMemory, false, "remove the configured memory limit")
}

func parsePortOptionsFromArgs(args []string) ([]string, PortOptions, error) {
	cmdArgs, opts, err := parseProjectOptionsFromArgs(args)
	return cmdArgs, opts.Ports, err
}

func parseProjectOptionsFromArgs(args []string) ([]string, ProjectOptions, error) {
	var cmdArgs []string
	var opts ProjectOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			cmdArgs = append(cmdArgs, args[i+1:]...)
			i = len(args)
		case arg == "--"+flagPort:
			if i+1 >= len(args) {
				return nil, opts, errors.New("flag needs an argument: --" + flagPort)
			}
			opts.Ports.Specs = append(opts.Ports.Specs, args[i+1])
			i++
		case strings.HasPrefix(arg, "--"+flagPort+"="):
			opts.Ports.Specs = append(opts.Ports.Specs, strings.TrimPrefix(arg, "--"+flagPort+"="))
		case arg == "--"+flagNoPorts:
			opts.Ports.Clear = true
		case arg == "--"+flagPorts:
			opts.Ports.List = true
		case arg == "--"+flagMemory:
			if i+1 >= len(args) {
				return nil, opts, errors.New("flag needs an argument: --" + flagMemory)
			}
			opts.Memory.Limit = args[i+1]
			i++
		case strings.HasPrefix(arg, "--"+flagMemory+"="):
			opts.Memory.Limit = strings.TrimPrefix(arg, "--"+flagMemory+"=")
		case arg == "--"+flagNoMemory:
			opts.Memory.Clear = true
		default:
			cmdArgs = append(cmdArgs, arg)
		}
	}
	normalized, err := normalizeProjectOptions(opts)
	if err != nil {
		return nil, normalized, err
	}
	return cmdArgs, normalized, nil
}
