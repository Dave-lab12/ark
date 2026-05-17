package internal

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// A single Docker-build `stream` frame larger than bufio.Scanner's 64KB
// default line buffer used to truncate the build output and surface a
// spurious "token too long" error. json.Decoder has no such ceiling.
func TestStreamDockerBuildHandlesLargePayload(t *testing.T) {
	huge := strings.Repeat("a", 128*1024)
	frame, err := json.Marshal(map[string]string{"stream": huge})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}

	var out bytes.Buffer
	if err := streamDockerBuild(bytes.NewReader(frame), &out); err != nil {
		t.Fatalf("streamDockerBuild: %v", err)
	}
	if out.String() != huge {
		t.Fatalf("output truncated or altered: got %d bytes, want %d", out.Len(), len(huge))
	}
}

func TestStreamDockerBuildPropagatesError(t *testing.T) {
	frame, err := json.Marshal(map[string]string{"error": "build failed: missing FROM"})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	var out bytes.Buffer
	err = streamDockerBuild(bytes.NewReader(frame), &out)
	if err == nil || !strings.Contains(err.Error(), "missing FROM") {
		t.Fatalf("expected error frame to surface, got %v", err)
	}
}

func TestStreamDockerBuildEmitsStatusWithNewline(t *testing.T) {
	stream, _ := json.Marshal(map[string]string{"stream": "Step 1/2 : FROM scratch"})
	status, _ := json.Marshal(map[string]string{"status": "Pulling fs layer"})
	input := append(stream, status...)

	var out bytes.Buffer
	if err := streamDockerBuild(bytes.NewReader(input), &out); err != nil {
		t.Fatalf("streamDockerBuild: %v", err)
	}
	// stream frames are written verbatim (no synthetic newline); status
	// frames get a newline appended. Preserve the original behavior.
	want := "Step 1/2 : FROM scratchPulling fs layer\n"
	if out.String() != want {
		t.Fatalf("output mismatch:\n got %q\nwant %q", out.String(), want)
	}
}
