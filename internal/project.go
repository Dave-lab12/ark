package internal

import (
	"crypto/rand"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

var projectNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

var reservedProjectNames = map[string]struct{}{
	"code":       {},
	"config":     {},
	"completion": {},
	"doctor":     {},
	"help":       {},
	"init":       {},
	"ls":         {},
	"rm":         {},
	"start":      {},
	"stop":       {},
	"temp":       {},
}

func ValidateProjectName(name string) error {
	if !projectNamePattern.MatchString(name) {
		return fmt.Errorf("invalid project name %q: use letters, numbers, dots, dashes, or underscores", name)
	}
	if name == "." || name == ".." || strings.Contains(name, string(filepath.Separator)) {
		return fmt.Errorf("invalid project name %q", name)
	}
	if _, reserved := reservedProjectNames[name]; reserved {
		return fmt.Errorf("project name %q is reserved by an ark command", name)
	}
	return nil
}

func NewProject(name, runtimeName, projectPath, image string, sshEnabled, dockerEnabled bool) (Project, error) {
	id, err := newULID()
	if err != nil {
		return Project{}, err
	}
	now := time.Now().UTC()
	containerName := "ark-" + id
	return Project{
		ID:            id,
		Name:          name,
		Runtime:       runtimeName,
		Path:          projectPath,
		ContainerName: containerName,
		Image:         image,
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

func ProjectEnv(project Project) []string {
	env := []string{
		"ARK_PROJECT_ID=" + project.ID,
		"ARK_PROJECT_NAME=" + project.Name,
		"ARK_RUNTIME=" + project.Runtime,
	}
	if project.DockerEnabled {
		env = append(env, "DOCKER_HOST=unix:///var/run/docker.sock")
	}
	return env
}
