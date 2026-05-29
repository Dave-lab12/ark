package runtime

import "github.com/Dave-lab12/ark/internal/core"

const (
	RuntimeAuto   = core.RuntimeAuto
	RuntimeApple  = core.RuntimeApple
	RuntimeDocker = core.RuntimeDocker

	MountTypeBind   = core.MountTypeBind
	MountTypeVolume = core.MountTypeVolume
)

type BuildImageSpec = core.BuildImageSpec
type CreateSpec = core.CreateSpec
type ExecSpec = core.ExecSpec
type Container = core.Container
type ResourceStats = core.ResourceStats
type NetworkConnectSpec = core.NetworkConnectSpec
type NetworkGroup = core.NetworkGroup
type PortMapping = core.PortMapping
type ExitError = core.ExitError

var ErrNotFound = core.ErrNotFound
var ErrUnsupported = core.ErrUnsupported
