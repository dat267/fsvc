package cmd

import (
	"fmt"
)

func init() {
	SetAppName("fsvc")
}

// SetVersion overrides the default version string. Called from main() with
// the ldflags-injected version value.
func SetVersion(v string) {
	Version = v
}

// CLI is the root CLI struct containing all subcommand groups.
//
// A setting that holds a credential carries a secret:"" tag, which documents it
// at the point of definition; configClasses in config.go is what the masking and
// validation code actually consults, and TestConfigClassesMatchCLIFlags fails if
// a flag is added without being registered there or if the two disagree.
type CLI struct {
	ConfigFile      string          `help:"Config file path" json:"-"`
	Subdomain       string          `help:"Freshservice subdomain (e.g. acme)" env:"FSVC_SUBDOMAIN"`
	ItildeskSession string          `name:"itildesk-session" help:"_itildesk_session cookie value" env:"FSVC_ITILDESK_SESSION" secret:""`
	CSRFToken       string          `help:"CSRF token for write requests" env:"FSVC_CSRF_TOKEN" secret:""`
	BaseURL         string          `help:"Override API base URL (hidden; inferred from subdomain)" hidden:"" env:"FSVC_BASE_URL"`
	TimeZone        string          `help:"Timezone for business-day calculations (e.g. Europe/London)" env:"FSVC_TZ"`
	Concurrency     int             `help:"Max in-flight requests for concurrent phases" default:"8" env:"FSVC_CONCURRENCY"`
	Version         VersionCmd      `cmd:"" help:"Show version"`
	Session         SessionCmd      `cmd:"" help:"Verify the session cookie"`
	Tickets         TicketsCmdGroup `cmd:"" help:"Work with tickets"`
	Config          ConfigCmdGroup  `cmd:"" help:"Manage configuration"`
}

var Version = "dev"

type VersionCmd struct{}

func (c *VersionCmd) Run() error {
	fmt.Println(Version)
	return nil
}
