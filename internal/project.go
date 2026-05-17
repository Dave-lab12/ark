package internal

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

var projectNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

// ValidateProjectName rejects names that don't fit our character set or that
// would collide with an ark subcommand. `reserved` is supplied by the caller
// (built from the cobra command tree) so this stays a pure function and the
// list can't drift from the real commands.
func ValidateProjectName(name string, reserved map[string]struct{}) error {
	if !projectNamePattern.MatchString(name) {
		return fmt.Errorf("invalid project name %q: use letters, numbers, dots, dashes, or underscores", name)
	}
	if name == "." || name == ".." || strings.Contains(name, string(filepath.Separator)) {
		return fmt.Errorf("invalid project name %q", name)
	}
	if _, isReserved := reserved[name]; isReserved {
		return fmt.Errorf("project name %q is reserved by an ark command", name)
	}
	return nil
}

func NewProject(name, runtimeName, projectPath, image, imageFingerprint string, sshEnabled, dockerEnabled bool) (Project, error) {
	id, err := newULID()
	if err != nil {
		return Project{}, err
	}
	now := time.Now().UTC()
	containerName := "ark-" + id
	return Project{
		ID:               id,
		Name:             name,
		Runtime:          runtimeName,
		Path:             projectPath,
		ContainerName:    containerName,
		Image:            image,
		ImageFingerprint: imageFingerprint,
		Volumes: Volumes{
			Home:   containerName + "-home",
			Cache:  containerName + "-cache",
			Docker: dockerVolumeName(containerName, dockerEnabled),
		},
		SSHEnabled:    sshEnabled,
		DockerEnabled: dockerEnabled,
		CreatedAt:     now,
		LastUsedAt:    now,
	}, nil
}

func dockerVolumeName(containerName string, enabled bool) string {
	if !enabled {
		return ""
	}
	return containerName + "-docker"
}

func newULID() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate project id: %w", err)
	}
	return id.String(), nil
}

func ProjectMounts(project Project) []MountSpec {
	mounts := []MountSpec{
		{
			Type:   MountTypeBind,
			Source: project.Path,
			Target: "/work",
		},
		{
			Type:   MountTypeVolume,
			Source: project.Volumes.Home,
			Target: "/home/dev",
		},
		{
			Type:   MountTypeVolume,
			Source: project.Volumes.Cache,
			Target: "/home/dev/.cache",
		},
	}
	if project.Volumes.Docker != "" {
		mounts = append(mounts, MountSpec{
			Type:   MountTypeVolume,
			Source: project.Volumes.Docker,
			Target: "/var/lib/docker",
		})
	}
	return mounts
}

func appendProjectControlPlaneMounts(mounts []MountSpec, paths Paths, project Project) []MountSpec {
	if project.SSHEnabled {
		mounts = append(mounts, MountSpec{
			Type:   MountTypeBind,
			Source: paths.ProjectSocketDir(project),
			Target: "/run/ark",
		})
	}
	return mounts
}

func ensureProjectControlPlane(paths Paths, project Project) error {
	if project.SSHEnabled {
		if err := os.MkdirAll(paths.ProjectSocketDir(project), 0o700); err != nil {
			return fmt.Errorf("create project socket directory: %w", err)
		}
	}
	return nil
}

func projectVolumeNames(project Project) []string {
	names := []string{
		project.Volumes.Home,
		project.Volumes.Cache,
	}
	if project.Volumes.Docker != "" {
		names = append(names, project.Volumes.Docker)
	}
	return names
}

func ProjectEnv(project Project, config Config) []string {
	env := []string{
		"ARK_PROJECT_ID=" + project.ID,
		"ARK_PROJECT_NAME=" + project.Name,
		"ARK_RUNTIME=" + project.Runtime,
	}
	if project.DockerEnabled {
		env = append(env,
			"DOCKER_HOST=unix:///var/run/docker.sock",
			"ARK_DOCKER_DATA_ROOT="+config.Docker.DataRoot,
			fmt.Sprintf("ARK_START_DOCKERD=%t", config.Docker.StartDockerd),
		)
	}
	if project.SSHEnabled && config.Git.Enabled {
		env = append(env,
			"GIT_SSH_COMMAND=/usr/local/bin/ark-ssh",
			"ARK_GIT_BROKER_SOCK="+config.Git.BrokerSocket,
		)
	}
	return env
}
