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
type CLI struct {
	ConfigFile      string                `help:"Config file path" json:"-"`
	Subdomain       string                `help:"Freshservice subdomain (e.g. acme)"`
	ItildeskSession string                `name:"itildesk-session" help:"_itildesk_session cookie value" env:"FSVC_ITILDESK_SESSION"`
	CSRFToken       string                `help:"CSRF token for write requests"`
	BaseURL         string                `help:"Override API base URL (hidden; inferred from subdomain)" hidden:""`
	TimeZone        string                `help:"Timezone for business-day calculations (e.g. Europe/London)" env:"FSVC_TZ"`
	Version         VersionCmd            `cmd:"" help:"Show version"`
	Session         SessionCmd            `cmd:"" help:"Verify the session cookie"`
	Tickets         TicketsCmdGroup       `cmd:"" help:"Work with tickets"`
	TicketFilters   TicketFiltersCmdGroup `cmd:"" help:"Work with ticket filters"`
	Users           UsersCmdGroup         `cmd:"" help:"Work with users"`
	Config          ConfigCmdGroup        `cmd:"" help:"Manage configuration"`
}

var Version = "dev"

type VersionCmd struct{}

func (c *VersionCmd) Run() error {
	fmt.Println(Version)
	return nil
}
