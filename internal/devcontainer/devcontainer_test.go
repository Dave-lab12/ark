package devcontainer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildDevcontainerBasicProject(t *testing.T) {
	project := devcontainerTestProject(nil, false)
	config := DefaultConfig()

	got := parseDevcontainerJSON(t, project, config)

	if got["name"] != "ark-app" {
		t.Fatalf("name = %v, want ark-app", got["name"])
	}
	if got["image"] != config.Image.Tag {
		t.Fatalf("image = %v, want %s", got["image"], config.Image.Tag)
	}
	if got["workspaceFolder"] != config.Container.Workdir {
		t.Fatalf("workspaceFolder = %v, want %s", got["workspaceFolder"], config.Container.Workdir)
	}
	if got["remoteUser"] != config.Container.User {
		t.Fatalf("remoteUser = %v, want %s", got["remoteUser"], config.Container.User)
	}
	if _, ok := got["containerUser"]; ok {
		t.Fatalf("containerUser should be omitted so the image entrypoint starts as the image default user")
	}
	mount, ok := got["workspaceMount"].(string)
	if !ok || !strings.Contains(mount, "${localWorkspaceFolder}") {
		t.Fatalf("workspaceMount = %v, want local workspace folder variable", got["workspaceMount"])
	}
	if got["overrideCommand"] != false {
		t.Fatalf("overrideCommand = %v, want false", got["overrideCommand"])
	}
	mounts := gotArray(t, got, "mounts")
	if len(mounts) != 2 {
		t.Fatalf("mounts length = %d, want 2: %#v", len(mounts), mounts)
	}
}

func TestBuildDevcontainerWorkspaceFolderOverride(t *testing.T) {
	project := devcontainerTestProject(nil, false)
	config := DefaultConfig()
	got := parseDevcontainerJSONWithOptions(t, project, config, DevcontainerRenderOptions{
		WorkspaceFolder: "/work/packages/api",
	})

	if got["workspaceFolder"] != "/work/packages/api" {
		t.Fatalf("workspaceFolder = %v, want /work/packages/api", got["workspaceFolder"])
	}
}

func TestBuildDevcontainerImageTagFromOptions(t *testing.T) {
	project := devcontainerTestProject(nil, false)
	config := DefaultConfig()
	got := parseDevcontainerJSONWithOptions(t, project, config, DevcontainerRenderOptions{
		ImageTag: "ark-base:test",
	})

	if got["image"] != "ark-base:test" {
		t.Fatalf("image = %v, want ark-base:test", got["image"])
	}
}

func TestBuildDevcontainerWithDockerVolume(t *testing.T) {
	project := devcontainerTestProject(nil, true)
	got := parseDevcontainerJSON(t, project, DefaultConfig())

	mounts := gotArray(t, got, "mounts")
	if len(mounts) != 3 {
		t.Fatalf("mounts length = %d, want 3: %#v", len(mounts), mounts)
	}
	third, ok := mounts[2].(string)
	if !ok || !strings.Contains(third, "ark-test-docker-devcontainer") {
		t.Fatalf("third mount = %v, want docker volume with -devcontainer suffix", mounts[2])
	}
	if strings.Contains(third, "source=ark-test-docker,target=/var/lib/docker") {
		t.Fatalf("third mount shares direct docker volume: %v", third)
	}
}

func TestBuildDevcontainerSingleTCPPort(t *testing.T) {
	project := devcontainerTestProject([]PortMapping{mustPortMapping(t, "3000")}, false)
	got := parseDevcontainerJSON(t, project, DefaultConfig())

	assertNumberArray(t, got, "forwardPorts", []float64{3000})
	assertNumberArray(t, got, "appPort", []float64{3000})
}

func TestBuildDevcontainerMappedPort(t *testing.T) {
	project := devcontainerTestProject([]PortMapping{mustPortMapping(t, "8080:80")}, false)
	got := parseDevcontainerJSON(t, project, DefaultConfig())

	assertNumberArray(t, got, "forwardPorts", []float64{80})
	assertMixedArray(t, got, "appPort", []any{"8080:80"})
}

func TestBuildDevcontainerDynamicPort(t *testing.T) {
	project := devcontainerTestProject([]PortMapping{mustPortMapping(t, "0:3000")}, false)
	got := parseDevcontainerJSON(t, project, DefaultConfig())

	assertNumberArray(t, got, "forwardPorts", []float64{3000})
	if appPort := gotArray(t, got, "appPort"); len(appPort) != 0 {
		t.Fatalf("appPort = %#v, want empty for dynamic port", appPort)
	}
}

func TestBuildDevcontainerUDPPort(t *testing.T) {
	port := mustPortMapping(t, "3000/udp")
	project := devcontainerTestProject([]PortMapping{port}, false)
	got := parseDevcontainerJSON(t, project, DefaultConfig())

	if ports := gotArray(t, got, "forwardPorts"); len(ports) != 0 {
		t.Fatalf("forwardPorts = %#v, want empty for udp", ports)
	}
	if ports := gotArray(t, got, "appPort"); len(ports) != 0 {
		t.Fatalf("appPort = %#v, want empty for udp", ports)
	}
}

func TestBuildDevcontainerMixedTCPAndUDP(t *testing.T) {
	tcp := mustPortMapping(t, "3000")
	udp := mustPortMapping(t, "4000/udp")
	project := devcontainerTestProject([]PortMapping{tcp, udp}, false)
	got := parseDevcontainerJSON(t, project, DefaultConfig())

	assertNumberArray(t, got, "forwardPorts", []float64{3000})
	assertNumberArray(t, got, "appPort", []float64{3000})
}

func TestBuildDevcontainerEnvFromProjectEnv(t *testing.T) {
	project := devcontainerTestProject(nil, true)
	project.SSHEnabled = true
	config := DefaultConfig()
	got := parseDevcontainerJSON(t, project, config)

	env, ok := got["containerEnv"].(map[string]any)
	if !ok {
		t.Fatalf("containerEnv = %#v, want object", got["containerEnv"])
	}
	if env["ARK_PROJECT_ID"] != project.ID {
		t.Fatalf("ARK_PROJECT_ID = %v, want %s", env["ARK_PROJECT_ID"], project.ID)
	}
	if env["ARK_PROJECT_NAME"] != project.Name {
		t.Fatalf("ARK_PROJECT_NAME = %v, want %s", env["ARK_PROJECT_NAME"], project.Name)
	}
	if _, ok := env["DOCKER_HOST"]; !ok {
		t.Fatalf("DOCKER_HOST missing from containerEnv: %#v", env)
	}
	if _, ok := env["GIT_SSH_COMMAND"]; ok {
		t.Fatalf("GIT_SSH_COMMAND should be omitted from native devcontainer env: %#v", env)
	}
	if _, ok := env["ARK_GIT_BROKER_SOCK"]; ok {
		t.Fatalf("ARK_GIT_BROKER_SOCK should be omitted from native devcontainer env: %#v", env)
	}
}

func TestBuildDevcontainerCustomizationsMarker(t *testing.T) {
	got := parseDevcontainerJSON(t, devcontainerTestProject(nil, false), DefaultConfig())

	customizations, ok := got["customizations"].(map[string]any)
	if !ok {
		t.Fatalf("customizations = %#v, want object", got["customizations"])
	}
	ark, ok := customizations["ark"].(map[string]any)
	if !ok {
		t.Fatalf("customizations.ark = %#v, want object", customizations["ark"])
	}
	if ark["generated"] != true {
		t.Fatalf("generated = %v, want true", ark["generated"])
	}
}

func TestBuildDevcontainerDeterminism(t *testing.T) {
	project := devcontainerTestProject([]PortMapping{mustPortMapping(t, "8080:80")}, true)
	config := DefaultConfig()

	first, err := BuildDevcontainer(project, config, DevcontainerRenderOptions{
		ImageFingerprint: "fingerprint",
		ArkVersion:       "version",
	})
	if err != nil {
		t.Fatalf("BuildDevcontainer first: %v", err)
	}
	second, err := BuildDevcontainer(project, config, DevcontainerRenderOptions{
		ImageFingerprint: "fingerprint",
		ArkVersion:       "version",
	})
	if err != nil {
		t.Fatalf("BuildDevcontainer second: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("output is not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestBuildDevcontainerImageFingerprintFromParameter(t *testing.T) {
	project := devcontainerTestProject(nil, false)
	project.ImageFingerprint = "old"

	data, err := BuildDevcontainer(project, DefaultConfig(), DevcontainerRenderOptions{
		ImageFingerprint: "new",
		ArkVersion:       "version",
	})
	if err != nil {
		t.Fatalf("BuildDevcontainer: %v", err)
	}
	if !strings.Contains(string(data), `"image_fingerprint": "new"`) {
		t.Fatalf("missing new fingerprint:\n%s", data)
	}
	if strings.Contains(string(data), `"old"`) {
		t.Fatalf("output used project.ImageFingerprint:\n%s", data)
	}
}

func devcontainerTestProject(ports []PortMapping, docker bool) Project {
	project := Project{
		ID:               "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Name:             "app",
		Runtime:          RuntimeDocker,
		Path:             "/tmp/app",
		ContainerName:    "ark-test-container",
		Image:            DefaultImageTag,
		ImageFingerprint: "old",
		Volumes: Volumes{
			Home:   "ark-test-home",
			Cache:  "ark-test-cache",
			Docker: "",
		},
		Ports:         ports,
		DockerEnabled: docker,
	}
	if docker {
		project.Volumes.Docker = "ark-test-docker"
	}
	return project
}

func parseDevcontainerJSON(t *testing.T, project Project, config Config) map[string]any {
	t.Helper()
	return parseDevcontainerJSONWithOptions(t, project, config, DevcontainerRenderOptions{
		ImageFingerprint: "fingerprint",
		ArkVersion:       "version",
	})
}

func parseDevcontainerJSONWithOptions(t *testing.T, project Project, config Config, opts DevcontainerRenderOptions) map[string]any {
	t.Helper()
	data, err := BuildDevcontainer(project, config, opts)
	if err != nil {
		t.Fatalf("BuildDevcontainer: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse generated JSON: %v\n%s", err, data)
	}
	return parsed
}

func gotArray(t *testing.T, got map[string]any, key string) []any {
	t.Helper()
	raw, ok := got[key]
	if !ok {
		return nil
	}
	out, ok := raw.([]any)
	if !ok {
		t.Fatalf("%s = %#v, want array", key, raw)
	}
	return out
}

func assertNumberArray(t *testing.T, got map[string]any, key string, want []float64) {
	t.Helper()
	raw := gotArray(t, got, key)
	if len(raw) != len(want) {
		t.Fatalf("%s length = %d, want %d: %#v", key, len(raw), len(want), raw)
	}
	for i, item := range raw {
		n, ok := item.(float64)
		if !ok || n != want[i] {
			t.Fatalf("%s[%d] = %#v, want %v", key, i, item, want[i])
		}
	}
}

func assertMixedArray(t *testing.T, got map[string]any, key string, want []any) {
	t.Helper()
	raw := gotArray(t, got, key)
	if len(raw) != len(want) {
		t.Fatalf("%s length = %d, want %d: %#v", key, len(raw), len(want), raw)
	}
	for i, item := range raw {
		if item != want[i] {
			t.Fatalf("%s[%d] = %#v, want %#v", key, i, item, want[i])
		}
	}
}

func mustPortMapping(t *testing.T, spec string) PortMapping {
	t.Helper()
	ports, err := ParsePortList([]string{spec})
	if err != nil {
		t.Fatalf("ParsePortList(%q): %v", spec, err)
	}
	if len(ports) != 1 {
		t.Fatalf("ParsePortList(%q) returned %d ports", spec, len(ports))
	}
	return ports[0]
}
