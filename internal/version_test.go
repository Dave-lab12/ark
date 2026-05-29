package internal

import (
	"bytes"
	"context"
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionStringIncludesBuildNumber(t *testing.T) {
	restoreVersion(t)
	ArkVersion = "1.2.3"
	ArkBuild = "456"

	if got, want := VersionString(), "ark 1.2.3 (build 456)"; got != want {
		t.Fatalf("VersionString() = %q, want %q", got, want)
	}
}

func TestVersionStringDefaultsEmptyValues(t *testing.T) {
	restoreVersion(t)
	stubBuildInfo(t, nil)
	ArkVersion = ""
	ArkBuild = ""

	if got, want := VersionString(), "ark dev (build dev)"; got != want {
		t.Fatalf("VersionString() = %q, want %q", got, want)
	}
}

func TestVersionStringUsesVCSBuildInfoForSourceBuilds(t *testing.T) {
	restoreVersion(t)
	stubBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "70f0794123456789"},
			{Key: "vcs.modified", Value: "true"},
		},
	})
	ArkVersion = "dev"
	ArkBuild = "dev"

	if got, want := VersionString(), "ark source (build 70f079412345-dirty)"; got != want {
		t.Fatalf("VersionString() = %q, want %q", got, want)
	}
}

func TestVersionStringUsesModuleVersionWhenAvailable(t *testing.T) {
	restoreVersion(t)
	stubBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "70f0794123456789"},
		},
	})
	ArkVersion = "dev"
	ArkBuild = "dev"

	if got, want := VersionString(), "ark v1.2.3 (build 70f079412345)"; got != want {
		t.Fatalf("VersionString() = %q, want %q", got, want)
	}
}

func TestRootVersionFlag(t *testing.T) {
	restoreVersion(t)
	ArkVersion = "1.2.3"
	ArkBuild = "456"

	var out bytes.Buffer
	app := &App{out: &out}
	cmd := app.rootCommand(context.Background())
	cmd.SetArgs([]string{"-v"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(out.String()), "ark 1.2.3 (build 456)"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVersionCommand(t *testing.T) {
	restoreVersion(t)
	ArkVersion = "1.2.3"
	ArkBuild = "456"

	var out bytes.Buffer
	app := &App{out: &out}
	cmd := app.rootCommand(context.Background())
	cmd.SetArgs([]string{"version"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(out.String()), "ark 1.2.3 (build 456)"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVersionArgsStayOnRootCommand(t *testing.T) {
	app := &App{}
	root := app.rootCommand(context.Background())
	app.reserved = collectReservedNames(root)
	for _, args := range [][]string{
		{"-v"},
		{"--version"},
		{"version"},
	} {
		if app.shouldRunProject(args) {
			t.Fatalf("shouldRunProject(%v) = true, want false", args)
		}
	}
}

func restoreVersion(t *testing.T) {
	t.Helper()
	version := ArkVersion
	build := ArkBuild
	buildInfo := readBuildInfo
	t.Cleanup(func() {
		ArkVersion = version
		ArkBuild = build
		readBuildInfo = buildInfo
	})
}

func stubBuildInfo(t *testing.T, info *debug.BuildInfo) {
	t.Helper()
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return info, info != nil
	}
}
