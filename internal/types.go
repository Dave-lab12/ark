package internal

import (
	"fmt"
	"strings"

	"github.com/Dave-lab12/ark/internal/core"
)

var ArkVersion = "dev"
var ArkBuild = "dev"

func VersionString() string {
	version := strings.TrimSpace(ArkVersion)
	if version == "" {
		version = "dev"
	}
	build := strings.TrimSpace(ArkBuild)
	if build == "" {
		build = "dev"
	}
	return fmt.Sprintf("ark %s (build %s)", version, build)
}

const (
	RuntimeAuto   = core.RuntimeAuto
	RuntimeApple  = core.RuntimeApple
	RuntimeDocker = core.RuntimeDocker

	DefaultBaseImageName    = core.DefaultBaseImageName
	DefaultBaseImageTagName = core.DefaultBaseImageTagName
	DefaultImageTag         = core.DefaultImageTag
	DefaultParentImage      = core.DefaultParentImage
	StateVersion            = core.StateVersion

	MountTypeBind   = core.MountTypeBind
	MountTypeVolume = core.MountTypeVolume
)

type State = core.State
type Project = core.Project
type Volumes = core.Volumes
type PortMapping = core.PortMapping
type Container = core.Container
type ResourceStats = core.ResourceStats
type BuildImageSpec = core.BuildImageSpec
type CreateSpec = core.CreateSpec
type MountSpec = core.MountSpec
type MountType = core.MountType
type ExecSpec = core.ExecSpec

var ErrNotFound = core.ErrNotFound
var ErrUnsupported = core.ErrUnsupported

type ExitError = core.ExitError

func ExitCode(err error) (int, bool) {
	return core.ExitCode(err)
}
