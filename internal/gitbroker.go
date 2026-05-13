package internal

import (
	"context"
	"fmt"
	"net"
)

const DefaultBrokerSocketPath = "/run/ark/git-broker.sock"

var DefaultAllowedGitHosts = []string{
	"github.com",
	"gitlab.com",
	"bitbucket.org",
	"ssh.dev.azure.com",
}

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
	listener   net.Listener
}

func StartGitBroker(ctx context.Context, socketPath string) (*GitBroker, error) {
	return nil, fmt.Errorf("Git broker is not implemented in the Docker lifecycle MVP: %w", ErrUnsupported)
}

func (b *GitBroker) Close() error {
	if b == nil || b.listener == nil {
		return nil
	}
	return b.listener.Close()
}

func GitBrokerEnvironment() []string {
	return []string{
		"GIT_SSH_COMMAND=/usr/local/bin/ark-ssh",
		"ARK_GIT_BROKER_SOCK=" + DefaultBrokerSocketPath,
	}
}
