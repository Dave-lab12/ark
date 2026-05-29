package runtime

import (
	"context"
	"fmt"
	"os/exec"
)

type AppleRuntime struct{}

func NewAppleRuntime() *AppleRuntime { return &AppleRuntime{} }

func (r *AppleRuntime) Name() string { return RuntimeApple }

func (r *AppleRuntime) notSupported(op string) error {
	return fmt.Errorf("Apple runtime %s: %w", op, ErrUnsupported)
}

func (r *AppleRuntime) Available(context.Context) error {
	if _, err := exec.LookPath("container"); err != nil {
		return fmt.Errorf("Apple container CLI is not available: %w", err)
	}
	return nil
}

func (r *AppleRuntime) ImageExists(context.Context, string) (bool, error) {
	return false, r.notSupported("image inspect")
}
func (r *AppleRuntime) BuildImage(context.Context, BuildImageSpec) error {
	return r.notSupported("image build")
}
func (r *AppleRuntime) Create(context.Context, CreateSpec) (string, error) {
	return "", r.notSupported("create")
}
func (r *AppleRuntime) Start(context.Context, string) error          { return r.notSupported("start") }
func (r *AppleRuntime) Stop(context.Context, string, int) error      { return r.notSupported("stop") }
func (r *AppleRuntime) Remove(context.Context, string, bool) error   { return r.notSupported("remove") }
func (r *AppleRuntime) Exec(context.Context, string, ExecSpec) error { return r.notSupported("exec") }
func (r *AppleRuntime) Inspect(context.Context, string) (*Container, error) {
	return nil, r.notSupported("inspect")
}
func (r *AppleRuntime) Stats(context.Context, string) (*ResourceStats, error) {
	return nil, r.notSupported("stats")
}
func (r *AppleRuntime) List(context.Context) ([]Container, error) { return nil, r.notSupported("list") }
func (r *AppleRuntime) EnsureNetwork(context.Context, string) error {
	return r.notSupported("ensure network")
}
func (r *AppleRuntime) ConnectNetwork(context.Context, NetworkConnectSpec) error {
	return r.notSupported("connect network")
}
func (r *AppleRuntime) DisconnectNetwork(context.Context, string, string) error {
	return r.notSupported("disconnect network")
}
func (r *AppleRuntime) ListNetworkGroups(context.Context) ([]NetworkGroup, error) {
	return nil, r.notSupported("list networks")
}
func (r *AppleRuntime) CreateVolume(context.Context, string) error {
	return r.notSupported("create volume")
}
func (r *AppleRuntime) RemoveVolume(context.Context, string) error {
	return r.notSupported("remove volume")
}
