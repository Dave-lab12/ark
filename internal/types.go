package internal

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/Dave-lab12/ark/internal/core"
)

var ArkVersion = "dev"
var ArkBuild = "dev"
var readBuildInfo = debug.ReadBuildInfo

func VersionString() string {
	version, build := versionParts()
	return fmt.Sprintf("ark %s (build %s)", version, build)
}

func versionParts() (string, string) {
	info, _ := readBuildInfo()
	version := strings.TrimSpace(ArkVersion)
	build := strings.TrimSpace(ArkBuild)

	if version == "" || version == "dev" {
		if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		} else if vcsRevision(info) != "" {
			version = "source"
		} else {
			version = "dev"
		}
	}
	if build == "" || build == "dev" {
		build = shortRevision(vcsRevision(info))
		if build == "" {
			build = "dev"
		}
		if vcsModified(info) {
			build += "-dirty"
		}
	}
	return version, build
}

func vcsRevision(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return strings.TrimSpace(setting.Value)
		}
	}
	return ""
}

func vcsModified(info *debug.BuildInfo) bool {
	if info == nil {
		return false
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.modified" {
			return setting.Value == "true"
		}
	}
	return false
}

func shortRevision(revision string) string {
	if len(revision) <= 12 {
		return revision
	}
	return revision[:12]
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
type NetworkConnectSpec = core.NetworkConnectSpec
type NetworkGroup = core.NetworkGroup
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
