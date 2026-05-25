// Package internal is the CLI/application facade.
// New domain code should import domain packages directly and must not import internal.
package internal

import (
	"context"
	"io"
	"os"

	configpkg "github.com/Dave-lab12/ark/internal/config"
	defaultspkg "github.com/Dave-lab12/ark/internal/defaults"
	devcontainerpkg "github.com/Dave-lab12/ark/internal/devcontainer"
	editorpkg "github.com/Dave-lab12/ark/internal/editor"
	gitbrokerpkg "github.com/Dave-lab12/ark/internal/gitbroker"
	imagepkg "github.com/Dave-lab12/ark/internal/image"
	pathspkg "github.com/Dave-lab12/ark/internal/paths"
	portspkg "github.com/Dave-lab12/ark/internal/ports"
	projectpkg "github.com/Dave-lab12/ark/internal/project"
	runtimepkg "github.com/Dave-lab12/ark/internal/runtime"
)

type Config = configpkg.Config
type InitConfig = configpkg.InitConfig
type ImageConfig = configpkg.ImageConfig
type ContainerConfig = configpkg.ContainerConfig
type MountsConfig = configpkg.MountsConfig
type ReadOnlyMountConfig = configpkg.ReadOnlyMountConfig
type GitConfig = configpkg.GitConfig
type DockerConfig = configpkg.DockerConfig
type EditorConfig = configpkg.EditorConfig

type Paths = pathspkg.Paths
type Registry = projectpkg.Registry
type Runtime = runtimepkg.Runtime
type DockerRuntime = runtimepkg.DockerRuntime
type AppleRuntime = runtimepkg.AppleRuntime
type ImageStore = imagepkg.ImageStore
type ImageState = imagepkg.ImageState
type ImageInfo = imagepkg.ImageInfo
type GitBroker = gitbrokerpkg.GitBroker
type DevcontainerRenderOptions = devcontainerpkg.DevcontainerRenderOptions
type PortChange = portspkg.PortChange
type EditorMode = editorpkg.EditorMode

const (
	EditorModeAttach             = editorpkg.EditorModeAttach
	EditorModeDevcontainerNative = editorpkg.EditorModeDevcontainerNative
	DefaultBrokerSocketPath      = defaultspkg.DefaultBrokerSocketPath
	portRangeErrorText           = portspkg.PortRangeErrorText
)

var DefaultAllowedGitHosts = defaultspkg.DefaultAllowedGitHosts

func DefaultConfig() Config { return configpkg.DefaultConfig() }

func EnsureDefaultConfig(paths Paths) error { return configpkg.EnsureDefaultConfig(paths) }

func LoadConfig(paths Paths) (Config, error) { return configpkg.LoadConfig(paths) }

func WriteDefaultConfig(paths Paths, force bool) error {
	return configpkg.WriteDefaultConfig(paths, force)
}

func DefaultPaths() (Paths, error) { return pathspkg.DefaultPaths() }

func IsInsidePath(path, root string) bool { return pathspkg.IsInsidePath(path, root) }

func DirExistsNonEmpty(path string) (bool, error) { return pathspkg.DirExistsNonEmpty(path) }

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return pathspkg.AtomicWriteFile(path, data, perm)
}

func mkdirAllWithMode(path string, perm os.FileMode) error {
	return pathspkg.MkdirAllWithMode(path, perm)
}

func ValidateProjectName(name string, reserved map[string]struct{}) error {
	return projectpkg.ValidateProjectName(name, reserved)
}

func NewProject(name, runtimeName, projectPath, image, imageFingerprint string, sshEnabled, dockerEnabled bool) (Project, error) {
	return projectpkg.NewProject(name, runtimeName, projectPath, image, imageFingerprint, sshEnabled, dockerEnabled)
}

func NewRegistry(paths Paths) *Registry { return projectpkg.NewRegistry(paths) }

func ProjectMounts(project Project) []MountSpec { return projectpkg.ProjectMounts(project) }

func ProjectEnv(project Project, config Config) []string {
	return projectpkg.ProjectEnv(project, config)
}

func appendProjectControlPlaneMounts(mounts []MountSpec, paths Paths, project Project) []MountSpec {
	return projectpkg.AppendProjectControlPlaneMounts(mounts, paths, project)
}

func ensureProjectControlPlane(paths Paths, project Project) error {
	return projectpkg.EnsureProjectControlPlane(paths, project)
}

func projectVolumeNames(project Project) []string {
	return projectpkg.ProjectVolumeNames(project)
}

func ParsePortChange(specs []string, current []PortMapping) ([]PortMapping, error) {
	return portspkg.ParsePortChange(specs, current)
}

func ParsePortList(specs []string) ([]PortMapping, error) { return portspkg.ParsePortList(specs) }

func PortMappingsEqual(a, b []PortMapping) bool { return portspkg.PortMappingsEqual(a, b) }

func FormatPortMapping(p PortMapping) string { return portspkg.FormatPortMapping(p) }

func FormatPortList(ports []PortMapping) string { return portspkg.FormatPortList(ports) }

func ResolveEditorBinary(name string) (string, error) { return editorpkg.ResolveEditorBinary(name) }

func EditorModeFor(name string) EditorMode { return editorpkg.EditorModeFor(name) }

func BuildRemoteAuthority(containerName string) string {
	return editorpkg.BuildRemoteAuthority(containerName)
}

func resolveContainerFolder(folder, workdir string) string {
	return editorpkg.ResolveContainerFolder(folder, workdir)
}

func resolveNativeWorkspaceFolder(folder, workdir string) (string, error) {
	return editorpkg.ResolveNativeWorkspaceFolder(folder, workdir)
}

var launchEditor = editorpkg.LaunchEditor
var launchEditorPath = editorpkg.LaunchEditorPath

func ResolveRuntime(ctx context.Context, requested string) (Runtime, string, error) {
	return runtimepkg.ResolveRuntime(ctx, requested)
}

func RuntimeByName(name string) (Runtime, error) { return runtimepkg.RuntimeByName(name) }

func NewDockerRuntime() (*DockerRuntime, error) { return runtimepkg.NewDockerRuntime() }

func NewAppleRuntime() *AppleRuntime { return runtimepkg.NewAppleRuntime() }

func BuildBaseImage(ctx context.Context, rt Runtime, config Config, out, errOut io.Writer) error {
	return imagepkg.BuildBaseImage(ctx, rt, config, out, errOut)
}

func EnsureImageSource(config Config) error { return imagepkg.EnsureImageSource(config) }

func ComputeImageFingerprint(runtimeName string, arkVersion string, baseDir string) (string, error) {
	return imagepkg.ComputeImageFingerprint(runtimeName, arkVersion, baseDir)
}

func computeImageFingerprint(runtimeName string, arkVersion string, baseDir string) (string, error) {
	return imagepkg.ComputeImageFingerprint(runtimeName, arkVersion, baseDir)
}

func NewImageStore(paths Paths) *ImageStore { return imagepkg.NewImageStore(paths) }

func BuildDevcontainer(project Project, config Config, opts DevcontainerRenderOptions) ([]byte, error) {
	return devcontainerpkg.BuildDevcontainer(project, config, opts)
}

func StartGitBroker(ctx context.Context, socketPath string, allowedHosts []string, errOut io.Writer) (*GitBroker, error) {
	return gitbrokerpkg.StartGitBroker(ctx, socketPath, allowedHosts, errOut)
}
