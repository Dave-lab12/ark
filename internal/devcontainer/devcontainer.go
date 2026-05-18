package devcontainer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// generatedDevcontainer is the in-memory shape of the file ark writes.
type generatedDevcontainer struct {
	Name            string            `json:"name"`
	Image           string            `json:"image"`
	WorkspaceFolder string            `json:"workspaceFolder"`
	WorkspaceMount  string            `json:"workspaceMount"`
	RemoteUser      string            `json:"remoteUser"`
	OverrideCommand bool              `json:"overrideCommand"`
	Privileged      bool              `json:"privileged"`
	Mounts          []string          `json:"mounts,omitempty"`
	ForwardPorts    []int             `json:"forwardPorts,omitempty"`
	AppPort         []any             `json:"appPort,omitempty"`
	ContainerEnv    map[string]string `json:"containerEnv,omitempty"`
	Customizations  map[string]any    `json:"customizations,omitempty"`
}

type DevcontainerRenderOptions struct {
	ImageTag         string
	ImageFingerprint string
	ArkVersion       string
	WorkspaceFolder  string
}

// BuildDevcontainer renders the devcontainer.json content for a project.
// It is a pure function — same inputs produce the same JSON bytes — so
// "regenerate every time" is cheap and deterministic.
//
// Image metadata is passed explicitly (rather than read from the project)
// so callers can use the current image-store state, which may differ from
// the project's stored fields after `ark image rebuild`.
func BuildDevcontainer(project Project, config Config, opts DevcontainerRenderOptions) ([]byte, error) {
	imageTag := strings.TrimSpace(opts.ImageTag)
	if imageTag == "" {
		imageTag = config.Image.Tag
	}
	workspaceFolder := strings.TrimSpace(opts.WorkspaceFolder)
	if workspaceFolder == "" {
		workspaceFolder = config.Container.Workdir
	}

	dc := generatedDevcontainer{
		Name:            "ark-" + project.Name,
		Image:           imageTag,
		WorkspaceFolder: workspaceFolder,
		// ${localWorkspaceFolder} is a Dev Containers spec variable
		// resolved by the tool to "the folder the user opened in the
		// editor." That's exactly what we want for native mode — the
		// project root.
		WorkspaceMount: fmt.Sprintf(
			"source=${localWorkspaceFolder},target=%s,type=bind,consistency=cached",
			config.Container.Workdir,
		),
		RemoteUser: config.Container.User,
		// The spec defaults overrideCommand to true for image-based
		// configs, which would replace ark's entrypoint with a sleep
		// loop. We explicitly set false so ark-entrypoint runs and
		// starts dockerd, writes /run/ark/ready, etc.
		OverrideCommand: false,
		Privileged:      config.Container.Privileged,
		Mounts:          devcontainerMounts(project),
		ForwardPorts:    devcontainerForwardPorts(project),
		AppPort:         devcontainerAppPorts(project),
		ContainerEnv:    devcontainerEnv(project, config),
		Customizations: map[string]any{
			"ark": map[string]any{
				"generated":         true,
				"ark_version":       opts.ArkVersion,
				"image_fingerprint": opts.ImageFingerprint,
			},
		},
	}
	data, err := json.MarshalIndent(dc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode devcontainer.json: %w", err)
	}
	return append(data, '\n'), nil
}

func devcontainerMounts(p Project) []string {
	mounts := []string{
		fmt.Sprintf("source=%s,target=/home/dev,type=volume", p.Volumes.Home),
		fmt.Sprintf("source=%s,target=/home/dev/.cache,type=volume", p.Volumes.Cache),
	}
	// Don't share /var/lib/docker with ark's directly-managed container.
	// Two dockerd processes on one graph backend will corrupt it. Native
	// mode gets its own Docker data volume.
	if p.Volumes.Docker != "" {
		mounts = append(mounts, fmt.Sprintf(
			"source=%s-devcontainer,target=/var/lib/docker,type=volume",
			p.Volumes.Docker,
		))
	}
	return mounts
}

// devcontainerForwardPorts emits TCP container ports for the spec's
// forwardPorts field. UDP is not representable here and is omitted;
// users who need UDP forwarding should use `ark <name> --port` with
// ark's direct management.
func devcontainerForwardPorts(p Project) []int {
	out := make([]int, 0, len(p.Ports))
	for _, port := range p.Ports {
		if port.Protocol != "" && port.Protocol != "tcp" {
			continue
		}
		n, err := strconv.Atoi(port.ContainerPort)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// devcontainerAppPorts emits the same ports for Zed compatibility.
// Zed currently reads appPort but not forwardPorts; the spec prefers
// forwardPorts. Emit both so the JSON works in both worlds.
//
// Format: integer for "same port host and container", string "H:C" for
// "host:container". Dynamic ports (host port "0") are omitted —
// appPort doesn't have a "pick any" semantic.
func devcontainerAppPorts(p Project) []any {
	out := make([]any, 0, len(p.Ports))
	for _, port := range p.Ports {
		if port.Protocol != "" && port.Protocol != "tcp" {
			continue
		}
		if port.HostPort == "0" {
			continue
		}
		cp, err := strconv.Atoi(port.ContainerPort)
		if err != nil {
			continue
		}
		if port.HostPort == "" || port.HostPort == port.ContainerPort {
			out = append(out, cp)
			continue
		}
		out = append(out, fmt.Sprintf("%s:%s", port.HostPort, port.ContainerPort))
	}
	return out
}

// devcontainerEnv reuses ark's existing environment builder where possible.
// Git broker integration requires a long-lived broker and an Ark
// control-plane mount; native devcontainers are editor-owned, so those
// broker-specific variables are intentionally omitted for now.
func devcontainerEnv(project Project, config Config) map[string]string {
	env := map[string]string{}
	for _, pair := range ProjectEnv(project, config) {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		if k == "GIT_SSH_COMMAND" {
			continue
		}
		if strings.HasPrefix(k, "ARK_GIT_BROKER_") {
			continue
		}
		env[k] = v
	}
	return env
}
