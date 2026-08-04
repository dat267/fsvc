package cmd

import "fsvc/internal/fsapi"

func clientConfigFromCLI(cli *CLI) fsapi.ClientConfig {
	return fsapi.ClientConfig{
		Subdomain: cli.Subdomain,
		Cookie:    cli.Cookie,
		CSRF:      cli.CSRFToken,
		BaseURL:   cli.BaseURL,
	}
}
