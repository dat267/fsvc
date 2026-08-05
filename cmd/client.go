package cmd

import "fsvc/internal/fsapi"

func clientConfigFromCLI(cli *CLI) fsapi.ClientConfig {
	return fsapi.ClientConfig{
		Subdomain:       cli.Subdomain,
		ItildeskSession: cli.ItildeskSession,
		CSRF:            cli.CSRFToken,
		BaseURL:         cli.BaseURL,
	}
}
