package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/docker/go-units"
	"golang.org/x/term"
)

const (
	dockerLabelManaged      = "ark.managed"
	dockerLabelProject      = "ark.project"
	dockerLabelID           = "ark.project_id"
	dockerLabelNetworkGroup = "ark.network_group"
)

type DockerRuntime struct {
	client *client.Client
}

func NewDockerRuntime() (*DockerRuntime, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	return &DockerRuntime{client: cli}, nil
}

func (r *DockerRuntime) Name() string {
	return RuntimeDocker
}

func (r *DockerRuntime) Available(ctx context.Context) error {
	if _, err := r.client.Ping(ctx); err != nil {
		return fmt.Errorf("Docker daemon is not available: %w", err)
	}
	return nil
}

func (r *DockerRuntime) ImageExists(ctx context.Context, tag string) (bool, error) {
	if _, _, err := r.client.ImageInspectWithRaw(ctx, tag); err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect image %s: %w", tag, err)
	}
	return true, nil
}

func (r *DockerRuntime) BuildImage(ctx context.Context, spec BuildImageSpec) error {
	buildContext, err := tarDirectory(spec.ContextDir)
	if err != nil {
		return err
	}
	defer buildContext.Close()
	dockerfile := spec.Dockerfile
	if dockerfile == "" {
		dockerfile = "Containerfile"
	}
	buildArgs := map[string]*string{}
	for key, value := range spec.BuildArgs {
		v := value
		buildArgs[key] = &v
	}

	resp, err := r.client.ImageBuild(ctx, buildContext, types.ImageBuildOptions{
		Tags:       []string{spec.Tag},
		Dockerfile: dockerfile,
		BuildArgs:  buildArgs,
		Remove:     true,
	})
	if err != nil {
		return fmt.Errorf("build image %s: %w", spec.Tag, err)
	}
	defer resp.Body.Close()

	if err := streamDockerBuild(resp.Body, writerOrDefault(spec.Out, os.Stdout)); err != nil {
		return fmt.Errorf("build image %s: %w", spec.Tag, err)
	}
	return nil
}

func (r *DockerRuntime) Create(ctx context.Context, spec CreateSpec) (string, error) {
	mounts := make([]mount.Mount, 0, len(spec.Mounts))
	for _, m := range spec.Mounts {
		switch m.Type {
		case MountTypeBind:
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeBind,
				Source:   m.Source,
				Target:   m.Target,
				ReadOnly: m.ReadOnly,
			})
		case MountTypeVolume:
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeVolume,
				Source:   m.Source,
				Target:   m.Target,
				ReadOnly: m.ReadOnly,
			})
		default:
			return "", fmt.Errorf("unknown mount type %q", m.Type)
		}
	}

	networkMode := container.NetworkMode("bridge")
	if !spec.Network {
		networkMode = container.NetworkMode("none")
	}

	config := &container.Config{
		Image:      spec.Image,
		Env:        append(spec.Env, fmt.Sprintf("ARK_DOCKER_ENABLED=%t", spec.DockerEnabled)),
		WorkingDir: spec.Workdir,
		Labels: map[string]string{
			dockerLabelManaged: "true",
			dockerLabelProject: spec.ProjectName,
			dockerLabelID:      spec.ProjectID,
		},
	}
	hostConfig := &container.HostConfig{
		Mounts:      mounts,
		NetworkMode: networkMode,
		Privileged:  spec.Privileged,
		// Linux Engine doesn't map host.docker.internal by default; this makes
		// it resolve to the host so the Git broker's TCP fallback works there.
		// No-op on Docker Desktop where the mapping already exists.
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
	}
	if spec.Memory != "" {
		memoryBytes, err := units.RAMInBytes(spec.Memory)
		if err != nil || memoryBytes <= 0 {
			if err == nil {
				err = fmt.Errorf("memory limit must be greater than zero")
			}
			return "", fmt.Errorf("memory %q: %w", spec.Memory, err)
		}
		hostConfig.Resources.Memory = memoryBytes
	}

	if len(spec.Ports) > 0 {
		exposed := nat.PortSet{}
		bindings := nat.PortMap{}
		for _, p := range spec.Ports {
			proto := p.Protocol
			if proto == "" {
				proto = "tcp"
			}
			port, err := nat.NewPort(proto, p.ContainerPort)
			if err != nil {
				return "", fmt.Errorf("port %s/%s: %w", p.ContainerPort, proto, err)
			}
			exposed[port] = struct{}{}
			hostIP := p.HostIP
			if hostIP == "" {
				hostIP = "127.0.0.1"
			}
			bindings[port] = append(bindings[port], nat.PortBinding{
				HostIP:   hostIP,
				HostPort: p.HostPort,
			})
		}
		config.ExposedPorts = exposed
		hostConfig.PortBindings = bindings
	}

	resp, err := r.client.ContainerCreate(ctx, config, hostConfig, &network.NetworkingConfig{}, nil, spec.Name)
	if err != nil {
		// Older Docker API versions reject the "host-gateway" magic string.
		// Retry once without it and warn — the container still works; only
		// the TCP fallback for the Git broker is at risk.
		if strings.Contains(err.Error(), "host-gateway") {
			fmt.Fprintln(os.Stderr, "ark: Docker API does not support host-gateway; Git broker TCP fallback may not reach the host")
			hostConfig.ExtraHosts = nil
			resp, err = r.client.ContainerCreate(ctx, config, hostConfig, &network.NetworkingConfig{}, nil, spec.Name)
		}
		if err != nil {
			return "", fmt.Errorf("create container %s: %w", spec.Name, err)
		}
	}
	return resp.ID, nil
}

func (r *DockerRuntime) Start(ctx context.Context, containerName string) error {
	inspect, err := r.client.ContainerInspect(ctx, containerName)
	if err == nil && inspect.State != nil && inspect.State.Running {
		return nil
	}
	if err := r.client.ContainerStart(ctx, containerName, types.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start container %s: %w", containerName, err)
	}
	return nil
}

func (r *DockerRuntime) Stop(ctx context.Context, containerName string, timeoutSeconds int) error {
	timeout := timeoutSeconds
	if err := r.client.ContainerStop(ctx, containerName, container.StopOptions{Timeout: &timeout}); err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("container %s: %w", containerName, ErrNotFound)
		}
		return fmt.Errorf("stop container %s: %w", containerName, err)
	}
	return nil
}

func (r *DockerRuntime) Remove(ctx context.Context, containerName string, force bool) error {
	if err := r.client.ContainerRemove(ctx, containerName, types.ContainerRemoveOptions{
		Force: force,
	}); err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("container %s: %w", containerName, ErrNotFound)
		}
		return fmt.Errorf("remove container %s: %w", containerName, err)
	}
	return nil
}

func (r *DockerRuntime) Exec(ctx context.Context, containerName string, spec ExecSpec) error {
	cmd := spec.Cmd
	if len(cmd) == 0 {
		cmd = []string{"/bin/zsh"}
	}
	user := spec.User
	if user == "" {
		user = "dev"
	}
	workdir := spec.Workdir
	if workdir == "" {
		workdir = "/work"
	}
	execResp, err := r.client.ContainerExecCreate(ctx, containerName, types.ExecConfig{
		AttachStdin:  spec.Stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
		Env:          spec.Env,
		Tty:          spec.TTY,
		User:         user,
		WorkingDir:   workdir,
	})
	if err != nil {
		return fmt.Errorf("create exec in %s: %w", containerName, err)
	}

	hijack, err := r.client.ContainerExecAttach(ctx, execResp.ID, types.ExecStartCheck{Tty: spec.TTY})
	if err != nil {
		return fmt.Errorf("attach exec in %s: %w", containerName, err)
	}
	defer hijack.Close()

	restore, err := prepareTerminal(spec)
	if err != nil {
		return err
	}
	defer restore()

	if spec.TTY {
		resizeTTY(ctx, r.client, execResp.ID, spec)
		stopResize := watchTTYResize(ctx, r.client, execResp.ID, spec)
		defer stopResize()
	}

	if spec.Stdin != nil {
		go func() {
			defer hijack.CloseWrite()
			_, _ = io.Copy(hijack.Conn, spec.Stdin)
		}()
	} else {
		_ = hijack.CloseWrite()
	}

	var copyErr error
	if spec.TTY {
		_, copyErr = io.Copy(writerOrDefault(spec.Stdout, os.Stdout), hijack.Reader)
	} else {
		_, copyErr = stdcopy.StdCopy(
			writerOrDefault(spec.Stdout, os.Stdout),
			writerOrDefault(spec.Stderr, os.Stderr),
			hijack.Reader,
		)
	}
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return fmt.Errorf("stream exec output: %w", copyErr)
	}

	inspect, err := r.client.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return fmt.Errorf("inspect exec result: %w", err)
	}
	if inspect.ExitCode != 0 {
		return &ExitError{Code: inspect.ExitCode}
	}
	return nil
}

func (r *DockerRuntime) Inspect(ctx context.Context, containerName string) (*Container, error) {
	inspect, err := r.client.ContainerInspect(ctx, containerName)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("container %s: %w", containerName, ErrNotFound)
		}
		return nil, fmt.Errorf("inspect container %s: %w", containerName, err)
	}
	c := &Container{
		ID:      inspect.ID,
		Name:    strings.TrimPrefix(inspect.Name, "/"),
		Image:   inspect.Config.Image,
		Status:  inspect.State.Status,
		Running: inspect.State.Running,
		Runtime: RuntimeDocker,
	}
	if inspect.NetworkSettings != nil {
		for portProto, bindings := range inspect.NetworkSettings.Ports {
			proto := portProto.Proto()
			containerPort := portProto.Port()
			for _, b := range bindings {
				c.Ports = append(c.Ports, PortMapping{
					HostIP:        b.HostIP,
					HostPort:      b.HostPort,
					ContainerPort: containerPort,
					Protocol:      proto,
				})
			}
		}
	}
	return c, nil
}

func (r *DockerRuntime) Stats(ctx context.Context, containerName string) (*ResourceStats, error) {
	resp, err := r.client.ContainerStats(ctx, containerName, false)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, fmt.Errorf("container %s: %w", containerName, ErrNotFound)
		}
		return nil, fmt.Errorf("stats container %s: %w", containerName, err)
	}
	defer resp.Body.Close()

	var stats types.StatsJSON
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("decode stats for %s: %w", containerName, err)
	}
	return &ResourceStats{
		CPUPercent:  calculateCPUPercent(stats),
		MemoryUsage: calculateMemoryUsage(stats),
		MemoryLimit: stats.MemoryStats.Limit,
	}, nil
}

func calculateCPUPercent(stats types.StatsJSON) float64 {
	if stats.CPUStats.CPUUsage.TotalUsage < stats.PreCPUStats.CPUUsage.TotalUsage ||
		stats.CPUStats.SystemUsage < stats.PreCPUStats.SystemUsage {
		return 0
	}
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)
	onlineCPUs := float64(stats.CPUStats.OnlineCPUs)
	if onlineCPUs == 0 {
		onlineCPUs = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if cpuDelta <= 0 || systemDelta <= 0 || onlineCPUs <= 0 {
		return 0
	}
	return (cpuDelta / systemDelta) * onlineCPUs * 100
}

func calculateMemoryUsage(stats types.StatsJSON) uint64 {
	usage := stats.MemoryStats.Usage
	cache := stats.MemoryStats.Stats["inactive_file"]
	if cache == 0 {
		cache = stats.MemoryStats.Stats["total_inactive_file"]
	}
	if usage > cache {
		return usage - cache
	}
	return usage
}

func (r *DockerRuntime) List(ctx context.Context) ([]Container, error) {
	containers, err := r.client.ContainerList(ctx, types.ContainerListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", dockerLabelManaged+"=true"),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("list Docker containers: %w", err)
	}
	out := make([]Container, 0, len(containers))
	for _, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		out = append(out, Container{
			ID:      c.ID,
			Name:    name,
			Image:   c.Image,
			Status:  c.Status,
			Running: c.State == "running",
			Runtime: RuntimeDocker,
		})
	}
	return out, nil
}

func (r *DockerRuntime) EnsureNetwork(ctx context.Context, networkName string) error {
	if _, err := r.client.NetworkInspect(ctx, networkName, types.NetworkInspectOptions{}); err == nil {
		return nil
	} else if !errdefs.IsNotFound(err) {
		return fmt.Errorf("inspect network %s: %w", networkName, err)
	}
	if _, err := r.client.NetworkCreate(ctx, networkName, types.NetworkCreate{
		Driver:         "bridge",
		CheckDuplicate: true,
		Labels: map[string]string{
			dockerLabelManaged:      "true",
			dockerLabelNetworkGroup: networkGroupNameFromNetwork(networkName),
		},
	}); err != nil {
		if errdefs.IsConflict(err) {
			return nil
		}
		return fmt.Errorf("create network %s: %w", networkName, err)
	}
	return nil
}

func networkGroupNameFromNetwork(networkName string) string {
	return strings.TrimPrefix(networkName, "ark-")
}

func (r *DockerRuntime) ConnectNetwork(ctx context.Context, spec NetworkConnectSpec) error {
	if err := r.client.NetworkConnect(ctx, spec.NetworkName, spec.ContainerName, &network.EndpointSettings{
		Aliases: spec.Aliases,
	}); err != nil {
		if errdefs.IsNotModified(err) || strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("connect %s to network %s: %w", spec.ContainerName, spec.NetworkName, err)
	}
	return nil
}

func (r *DockerRuntime) DisconnectNetwork(ctx context.Context, networkName, containerName string) error {
	if err := r.client.NetworkDisconnect(ctx, networkName, containerName, false); err != nil {
		if errdefs.IsNotFound(err) || strings.Contains(err.Error(), "is not connected") {
			return nil
		}
		return fmt.Errorf("disconnect %s from network %s: %w", containerName, networkName, err)
	}
	return nil
}

func (r *DockerRuntime) ListNetworkGroups(ctx context.Context) ([]NetworkGroup, error) {
	networks, err := r.client.NetworkList(ctx, types.NetworkListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", dockerLabelManaged+"=true"),
			filters.Arg("label", dockerLabelNetworkGroup),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	groups := make([]NetworkGroup, 0, len(networks))
	for _, networkSummary := range networks {
		networkInfo, err := r.client.NetworkInspect(ctx, networkSummary.ID, types.NetworkInspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("inspect network %s: %w", networkSummary.Name, err)
		}
		groupName := networkInfo.Labels[dockerLabelNetworkGroup]
		if groupName == "" {
			groupName = networkGroupNameFromNetwork(networkInfo.Name)
		}
		group := NetworkGroup{
			Name:        groupName,
			NetworkName: networkInfo.Name,
			Containers:  make([]string, 0, len(networkInfo.Containers)),
		}
		for _, endpoint := range networkInfo.Containers {
			group.Containers = append(group.Containers, endpoint.Name)
		}
		sort.Strings(group.Containers)
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})
	return groups, nil
}

func (r *DockerRuntime) CreateVolume(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}
	if _, err := r.client.VolumeCreate(ctx, volume.CreateOptions{
		Name: name,
		Labels: map[string]string{
			dockerLabelManaged: "true",
		},
	}); err != nil {
		return fmt.Errorf("create volume %s: %w", name, err)
	}
	return nil
}

func (r *DockerRuntime) RemoveVolume(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}
	if err := r.client.VolumeRemove(ctx, name, true); err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("remove volume %s: %w", name, err)
	}
	return nil
}

func streamDockerBuild(r io.Reader, out io.Writer) error {
	dec := json.NewDecoder(r)
	for {
		var msg struct {
			Stream string `json:"stream"`
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("decode build output: %w", err)
		}
		switch {
		case msg.Error != "":
			return errors.New(msg.Error)
		case msg.Stream != "":
			fmt.Fprint(out, msg.Stream)
		case msg.Status != "":
			fmt.Fprintln(out, msg.Status)
		}
	}
}

func prepareTerminal(spec ExecSpec) (func(), error) {
	in, ok := spec.Stdin.(*os.File)
	if !spec.TTY || !ok || !term.IsTerminal(int(in.Fd())) {
		return func() {}, nil
	}
	oldState, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil, fmt.Errorf("put terminal in raw mode: %w", err)
	}
	return func() {
		_ = term.Restore(int(in.Fd()), oldState)
	}, nil
}

func resizeTTY(ctx context.Context, cli *client.Client, execID string, spec ExecSpec) {
	out, ok := spec.Stdout.(*os.File)
	if !ok || !term.IsTerminal(int(out.Fd())) {
		return
	}
	width, height, err := term.GetSize(int(out.Fd()))
	if err != nil {
		return
	}
	_ = cli.ContainerExecResize(ctx, execID, types.ResizeOptions{
		Height: uint(height),
		Width:  uint(width),
	})
}

func watchTTYResize(ctx context.Context, cli *client.Client, execID string, spec ExecSpec) func() {
	out, ok := spec.Stdout.(*os.File)
	if !ok || !term.IsTerminal(int(out.Fd())) {
		return func() {}
	}
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(signals, syscall.SIGWINCH)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-signals:
				resizeTTY(ctx, cli, execID, spec)
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}

func writerOrDefault(w io.Writer, fallback io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return fallback
}
