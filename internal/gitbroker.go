package internal

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const brokerDefaultOpTimeout = 5 * time.Minute

// Swappable so tests can shorten the deadline without waiting 10s.
var brokerHeaderReadTimeout = 10 * time.Second

func brokerHeaderReadTimeoutForTest(d time.Duration) time.Duration {
	prev := brokerHeaderReadTimeout
	brokerHeaderReadTimeout = d
	return prev
}

const DefaultBrokerSocketPath = "/run/ark/git-broker.sock"

var DefaultAllowedGitHosts = []string{
	"github.com",
	"gitlab.com",
	"bitbucket.org",
	"ssh.dev.azure.com",
}

var allowedGitCommands = map[string]struct{}{
	"git-upload-pack":    {},
	"git-receive-pack":   {},
	"git-upload-archive": {},
}

var gitRepoPattern = regexp.MustCompile(`^[A-Za-z0-9._~/:@+-]+$`)

type GitSSHRequest struct {
	User    string
	Host    string
	Port    int
	Command string
	Repo    string
}

type GitBrokerRequest struct {
	Argv []string          `json:"argv"`
	Env  map[string]string `json:"env"`
	CWD  string            `json:"cwd"`
}

type GitBroker struct {
	socketPath string
	listeners  []net.Listener
	tcpAddress string
	token      string
	hosts      map[string]struct{}
	errOut     io.Writer
	opTimeout  time.Duration
	wg         sync.WaitGroup
}

func StartGitBroker(ctx context.Context, socketPath string, allowedHosts []string, errOut io.Writer) (*GitBroker, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, fmt.Errorf("create Git broker socket directory: %w", err)
	}
	_ = os.Remove(socketPath)
	unixListener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on Git broker socket %s: %w", socketPath, err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = unixListener.Close()
		return nil, fmt.Errorf("chmod Git broker socket: %w", err)
	}
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = unixListener.Close()
		return nil, fmt.Errorf("listen on Git broker TCP fallback: %w", err)
	}
	tcpAddr := tcpListener.Addr().(*net.TCPAddr)
	if errOut == nil {
		errOut = io.Discard
	}
	hosts := map[string]struct{}{}
	for _, host := range allowedHosts {
		hosts[strings.ToLower(strings.TrimSpace(host))] = struct{}{}
	}
	token, err := generateBrokerToken()
	if err != nil {
		_ = unixListener.Close()
		_ = tcpListener.Close()
		return nil, fmt.Errorf("generate Git broker token: %w", err)
	}
	broker := &GitBroker{
		socketPath: socketPath,
		listeners:  []net.Listener{unixListener, tcpListener},
		tcpAddress: fmt.Sprintf("host.docker.internal:%d", tcpAddr.Port),
		token:      token,
		hosts:      hosts,
		errOut:     errOut,
		opTimeout:  resolveBrokerOpTimeout(),
	}
	broker.wg.Add(1)
	go broker.serve(ctx, unixListener, false)
	broker.wg.Add(1)
	go broker.serve(ctx, tcpListener, true)
	return broker, nil
}

func generateBrokerToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func resolveBrokerOpTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("ARK_GIT_BROKER_OP_TIMEOUT"))
	if raw == "" {
		return brokerDefaultOpTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return brokerDefaultOpTimeout
	}
	return d
}

func (b *GitBroker) Close() error {
	if b == nil {
		return nil
	}
	var firstErr error
	for _, listener := range b.listeners {
		if err := listener.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	b.wg.Wait()
	_ = os.Remove(b.socketPath)
	return firstErr
}

func (b *GitBroker) Environment() []string {
	if b == nil || b.tcpAddress == "" {
		return nil
	}
	env := []string{"ARK_GIT_BROKER_TCP=" + b.tcpAddress}
	if b.token != "" {
		env = append(env, "ARK_GIT_BROKER_TOKEN="+b.token)
	}
	return env
}

func (b *GitBroker) serve(ctx context.Context, listener net.Listener, requireToken bool) {
	defer b.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			fmt.Fprintf(b.errOut, "ark git broker: accept: %v\n", err)
			continue
		}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			b.handle(ctx, conn, requireToken)
		}()
	}
}

func (b *GitBroker) handle(ctx context.Context, conn net.Conn, requireToken bool) {
	defer conn.Close()
	// Cap how long a misbehaving client can pin a goroutine + ssh process by
	// holding the connection open without sending a header.
	_ = conn.SetReadDeadline(time.Now().Add(brokerHeaderReadTimeout))
	reader := bufio.NewReader(conn)
	request, err := readBrokerRequest(reader, b.token, requireToken)
	if err != nil {
		fmt.Fprintf(b.errOut, "ark git broker: %v\n", err)
		return
	}
	// Clear the deadline now — the git pack/protocol stream that follows can
	// legitimately take minutes (large fetches), and we don't want the header
	// timeout to kill it mid-transfer.
	_ = conn.SetReadDeadline(time.Time{})

	sshReq, err := parseGitSSHRequest(request.Argv)
	if err != nil {
		fmt.Fprintf(b.errOut, "ark git broker: %v\n", err)
		return
	}
	if err := b.validateRequest(sshReq); err != nil {
		fmt.Fprintf(b.errOut, "ark git broker: %v\n", err)
		return
	}

	args := []string{"-o", "BatchMode=yes"}
	if request.Env["GIT_PROTOCOL"] != "" {
		args = append(args, "-o", "SendEnv=GIT_PROTOCOL")
	}
	if sshReq.Port != 0 && sshReq.Port != 22 {
		args = append(args, "-p", strconv.Itoa(sshReq.Port))
	}
	target := sshReq.Host
	if sshReq.User != "" {
		target = sshReq.User + "@" + sshReq.Host
	}
	args = append(args, target, sshReq.Command)

	opTimeout := b.opTimeout
	if opTimeout <= 0 {
		opTimeout = brokerDefaultOpTimeout
	}
	opCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	cmd := exec.CommandContext(opCtx, "ssh", args...)
	cmd.Stdin = reader
	cmd.Stdout = conn
	// ssh's stderr (auth failures, host key prompts, "Permission denied")
	// goes back to the client so git surfaces it to the user. Broker-internal
	// errors above still go to b.errOut on the ark side.
	cmd.Stderr = conn
	cmd.Env = os.Environ()
	if request.Env["GIT_PROTOCOL"] != "" {
		cmd.Env = append(cmd.Env, "GIT_PROTOCOL="+request.Env["GIT_PROTOCOL"])
	}
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(b.errOut, "ark git broker: ssh failed: %v\n", err)
	}
}

func readBrokerRequest(reader *bufio.Reader, expectedToken string, requireToken bool) (GitBrokerRequest, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return GitBrokerRequest{}, fmt.Errorf("read request header: %w", err)
	}
	line = strings.TrimSpace(line)
	const prefix = "ARKGIT1 "
	if !strings.HasPrefix(line, prefix) {
		return GitBrokerRequest{}, errors.New("invalid request header")
	}
	rest := strings.TrimPrefix(line, prefix)

	// Wire formats:
	//   "ARKGIT1 <base64-json>"          — legacy, allowed only on unix socket
	//   "ARKGIT1 <token> <base64-json>"  — authenticated, required on TCP
	var token, payload string
	if idx := strings.IndexByte(rest, ' '); idx != -1 {
		token = rest[:idx]
		payload = rest[idx+1:]
	} else {
		payload = rest
	}

	if token != "" {
		if expectedToken == "" {
			return GitBrokerRequest{}, errors.New("broker has no token configured")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
			return GitBrokerRequest{}, errors.New("invalid broker token")
		}
	} else if requireToken {
		return GitBrokerRequest{}, errors.New("missing broker token")
	}

	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return GitBrokerRequest{}, fmt.Errorf("decode request header: %w", err)
	}
	var request GitBrokerRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return GitBrokerRequest{}, fmt.Errorf("parse request header: %w", err)
	}
	if len(request.Argv) == 0 {
		return GitBrokerRequest{}, errors.New("empty ssh argv")
	}
	if request.Env == nil {
		request.Env = map[string]string{}
	}
	return request, nil
}

func parseGitSSHRequest(argv []string) (GitSSHRequest, error) {
	req := GitSSHRequest{Port: 22}
	var target string
	var commandParts []string
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if target == "" && strings.HasPrefix(arg, "-") {
			switch {
			case arg == "-p":
				i++
				if i >= len(argv) {
					return GitSSHRequest{}, errors.New("missing ssh -p value")
				}
				port, err := strconv.Atoi(argv[i])
				if err != nil || port <= 0 || port > 65535 {
					return GitSSHRequest{}, fmt.Errorf("invalid ssh port %q", argv[i])
				}
				req.Port = port
			case strings.HasPrefix(arg, "-p") && len(arg) > 2:
				port, err := strconv.Atoi(strings.TrimPrefix(arg, "-p"))
				if err != nil || port <= 0 || port > 65535 {
					return GitSSHRequest{}, fmt.Errorf("invalid ssh port %q", arg)
				}
				req.Port = port
			case arg == "-l":
				i++
				if i >= len(argv) {
					return GitSSHRequest{}, errors.New("missing ssh -l value")
				}
				req.User = argv[i]
			case arg == "-o":
				i++
				if i >= len(argv) {
					return GitSSHRequest{}, errors.New("missing ssh -o value")
				}
			default:
				return GitSSHRequest{}, fmt.Errorf("unsupported ssh option %q", arg)
			}
			continue
		}
		if target == "" {
			target = arg
			continue
		}
		commandParts = append(commandParts, arg)
	}
	if target == "" {
		return GitSSHRequest{}, errors.New("missing ssh host")
	}
	user, host := splitSSHTarget(target)
	if req.User == "" {
		req.User = user
	}
	req.Host = strings.ToLower(strings.Trim(host, "[]"))
	req.Command = strings.Join(commandParts, " ")
	command, repo, err := parseGitCommand(req.Command)
	if err != nil {
		return GitSSHRequest{}, err
	}
	req.Command = command + " " + shellQuote(repo)
	req.Repo = repo
	return req, nil
}

func splitSSHTarget(target string) (string, string) {
	if at := strings.LastIndex(target, "@"); at != -1 {
		return target[:at], target[at+1:]
	}
	return "", target
}

func parseGitCommand(command string) (string, string, error) {
	command = strings.TrimSpace(command)
	for allowed := range allowedGitCommands {
		if !strings.HasPrefix(command, allowed+" ") {
			continue
		}
		repo := strings.TrimSpace(strings.TrimPrefix(command, allowed))
		repo = strings.Trim(repo, `"'`)
		if repo == "" {
			return "", "", errors.New("missing Git repository path")
		}
		if strings.HasPrefix(repo, "-") || strings.ContainsAny(repo, "\x00\r\n") || !gitRepoPattern.MatchString(repo) {
			return "", "", fmt.Errorf("disallowed Git repository path %q", repo)
		}
		return allowed, repo, nil
	}
	return "", "", fmt.Errorf("unsupported Git SSH command %q", command)
}

func (b *GitBroker) validateRequest(req GitSSHRequest) error {
	if req.Host == "" {
		return errors.New("missing Git host")
	}
	if _, ok := b.hosts[req.Host]; !ok {
		return fmt.Errorf("Git host %q is not allowed", req.Host)
	}
	if req.User != "" && req.User != "git" {
		return fmt.Errorf("Git SSH user %q is not allowed", req.User)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
