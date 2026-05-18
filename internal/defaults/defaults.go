package defaults

const DefaultBrokerSocketPath = "/run/ark/git-broker.sock"

var DefaultAllowedGitHosts = []string{
	"github.com",
	"gitlab.com",
	"bitbucket.org",
	"ssh.dev.azure.com",
}
