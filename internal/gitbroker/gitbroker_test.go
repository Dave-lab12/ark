package gitbroker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dialUnix opens a unix-socket connection to the broker.
func dialUnix(t *testing.T, path string) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial unix: %v", err)
	}
	return conn
}

// dialTCP parses the broker's host:port string and opens a TCP connection.
// The broker advertises host.docker.internal:<port>, but the listener itself
// is bound to 127.0.0.1 — so we strip the host and dial loopback directly.
func dialTCP(t *testing.T, addr string) net.Conn {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split tcp address %q: %v", addr, err)
	}
	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		t.Fatalf("dial tcp: %v", err)
	}
	return conn
}

// encodeRequest mimics what ark-ssh writes on the wire: the JSON request,
// base64-encoded, optionally prefixed by a token.
func encodeRequest(t *testing.T, token string, req GitBrokerRequest) []byte {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	payload := base64.StdEncoding.EncodeToString(raw)
	if token != "" {
		return []byte("ARKGIT1 " + token + " " + payload + "\n")
	}
	return []byte("ARKGIT1 " + payload + "\n")
}

// startTestBroker boots a broker with a captured stderr and returns it along
// with the buffer so tests can inspect the error stream.
//
// We mkdtemp in a short temp root rather than using t.TempDir() because macOS limits
// sun_path to 104 chars and the default temp dir blows past that.
func startTestBroker(t *testing.T) (*GitBroker, *bytes.Buffer) {
	t.Helper()
	dir, err := os.MkdirTemp(shortSocketTempRoot(), "ark-broker-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "s")
	var errBuf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	broker, err := StartGitBroker(ctx, sockPath, DefaultAllowedGitHosts, &errBuf)
	if err != nil {
		if strings.Contains(err.Error(), "bind: operation not permitted") {
			t.Skipf("unix sockets are not permitted in this sandbox: %v", err)
		}
		t.Fatalf("StartGitBroker: %v", err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	if broker.token == "" {
		t.Fatal("broker did not generate a token")
	}
	return broker, &errBuf
}

func shortSocketTempRoot() string {
	if _, err := os.Stat("/private/tmp"); err == nil {
		return "/private/tmp"
	}
	return "/tmp"
}

// validRequest is a request that will pass parseGitSSHRequest and
// validateRequest, so the only thing under test is the token/transport gate.
// We don't actually let the broker exec ssh: the parse layer accepts the
// request, then the handler will try to run ssh, but the test only inspects
// what the broker did *before* that point (whether it parsed/auth'd at all).
func validRequest() GitBrokerRequest {
	return GitBrokerRequest{
		Argv: []string{"git@github.com", "git-upload-pack 'foo/bar.git'"},
		Env:  map[string]string{},
		CWD:  "/",
	}
}

// waitForLog polls the broker's error buffer for substring `needle` and
// returns whether it appeared within the timeout. The handler runs in a
// goroutine, so we can't read its output synchronously.
func waitForLog(buf *bytes.Buffer, needle string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), needle) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestBrokerUnixAcceptsTokenlessRequest(t *testing.T) {
	broker, errBuf := startTestBroker(t)
	conn := dialUnix(t, broker.socketPath)
	defer conn.Close()
	if _, err := conn.Write(encodeRequest(t, "", validRequest())); err != nil {
		t.Fatalf("write request: %v", err)
	}
	// We don't have a real ssh upstream, so the broker will eventually log
	// an ssh failure. The point of this assertion is the *absence* of any
	// auth/parse complaints — those would mean we never got past the gate.
	if waitForLog(errBuf, "invalid broker token", 250*time.Millisecond) {
		t.Fatalf("unix socket rejected token-less request: %s", errBuf.String())
	}
	if waitForLog(errBuf, "missing broker token", 250*time.Millisecond) {
		t.Fatalf("unix socket required a token: %s", errBuf.String())
	}
}

func TestBrokerTCPRejectsTokenlessRequest(t *testing.T) {
	broker, errBuf := startTestBroker(t)
	conn := dialTCP(t, broker.tcpAddress)
	defer conn.Close()
	if _, err := conn.Write(encodeRequest(t, "", validRequest())); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if !waitForLog(errBuf, "missing broker token", time.Second) {
		t.Fatalf("expected token rejection, got: %s", errBuf.String())
	}
}

func TestBrokerTCPRejectsWrongToken(t *testing.T) {
	broker, errBuf := startTestBroker(t)
	conn := dialTCP(t, broker.tcpAddress)
	defer conn.Close()
	if _, err := conn.Write(encodeRequest(t, "not-the-real-token", validRequest())); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if !waitForLog(errBuf, "invalid broker token", time.Second) {
		t.Fatalf("expected invalid-token rejection, got: %s", errBuf.String())
	}
}

func TestBrokerTCPAcceptsCorrectToken(t *testing.T) {
	broker, errBuf := startTestBroker(t)
	conn := dialTCP(t, broker.tcpAddress)
	defer conn.Close()
	if _, err := conn.Write(encodeRequest(t, broker.token, validRequest())); err != nil {
		t.Fatalf("write request: %v", err)
	}
	// As in the unix test, success is measured by the absence of auth
	// complaints — the request reached the ssh-exec stage.
	if waitForLog(errBuf, "invalid broker token", 250*time.Millisecond) {
		t.Fatalf("correct token was rejected: %s", errBuf.String())
	}
	if waitForLog(errBuf, "missing broker token", 250*time.Millisecond) {
		t.Fatalf("correct token reported as missing: %s", errBuf.String())
	}
}

func TestBrokerEnvironmentIncludesToken(t *testing.T) {
	broker, _ := startTestBroker(t)
	env := broker.Environment()
	var sawTCP, sawToken bool
	for _, kv := range env {
		if strings.HasPrefix(kv, "ARK_GIT_BROKER_TCP=") {
			sawTCP = true
		}
		if strings.HasPrefix(kv, "ARK_GIT_BROKER_TOKEN=") {
			sawToken = true
			if got := strings.TrimPrefix(kv, "ARK_GIT_BROKER_TOKEN="); got != broker.token {
				t.Fatalf("env token %q does not match broker token %q", got, broker.token)
			}
		}
	}
	if !sawTCP {
		t.Error("Environment() missing ARK_GIT_BROKER_TCP")
	}
	if !sawToken {
		t.Error("Environment() missing ARK_GIT_BROKER_TOKEN")
	}
}

// A connection that opens and then never sends a header must be reaped by
// the broker's header read deadline (10s), rather than pinning a goroutine
// indefinitely. The shortened deadline lets us assert this within the test
// budget without waiting the full default.
func TestBrokerClosesIdleConnection(t *testing.T) {
	prevTimeout := brokerHeaderReadTimeoutForTest(2 * time.Second)
	t.Cleanup(func() { brokerHeaderReadTimeoutForTest(prevTimeout) })

	broker, _ := startTestBroker(t)
	conn := dialTCP(t, broker.tcpAddress)
	defer conn.Close()

	// Give ourselves a generous safety margin beyond the broker's deadline.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	buf := make([]byte, 1)
	_, err := conn.Read(buf)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected broker to close idle connection, got no error after %v", elapsed)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("broker took too long to close idle connection: %v", elapsed)
	}
}

// TestReadBrokerRequestWireFormats exercises the parser directly, since the
// goroutine-driven handler tests can only assert via stderr side effects.
func TestReadBrokerRequestWireFormats(t *testing.T) {
	token := "secret-token-value"
	req := validRequest()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	payload := base64.StdEncoding.EncodeToString(raw)

	cases := []struct {
		name         string
		line         string
		token        string
		requireToken bool
		wantErr      string // substring; empty means success
	}{
		{
			name:         "unix accepts no token",
			line:         "ARKGIT1 " + payload + "\n",
			token:        token,
			requireToken: false,
		},
		{
			name:         "unix accepts correct token",
			line:         "ARKGIT1 " + token + " " + payload + "\n",
			token:        token,
			requireToken: false,
		},
		{
			name:         "tcp rejects no token",
			line:         "ARKGIT1 " + payload + "\n",
			token:        token,
			requireToken: true,
			wantErr:      "missing broker token",
		},
		{
			name:         "tcp accepts correct token",
			line:         "ARKGIT1 " + token + " " + payload + "\n",
			token:        token,
			requireToken: true,
		},
		{
			name:         "tcp rejects wrong token",
			line:         "ARKGIT1 wrong " + payload + "\n",
			token:        token,
			requireToken: true,
			wantErr:      "invalid broker token",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(tc.line))
			_, err := readBrokerRequest(r, tc.token, tc.requireToken)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
