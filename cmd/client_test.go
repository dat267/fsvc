package cmd

import (
	"testing"

	"fsvc/internal/fsapi"
)

func TestClientConfigFromCLI(t *testing.T) {
	cli := &CLI{
		Subdomain: "acme",
		Cookie:    "helpdesk_node_session=abc",
		CSRFToken: "tok",
		BaseURL:   "http://127.0.0.1:9999",
	}

	cfg := clientConfigFromCLI(cli)
	want := fsapi.ClientConfig{
		Subdomain: "acme",
		Cookie:    "helpdesk_node_session=abc",
		CSRF:      "tok",
		BaseURL:   "http://127.0.0.1:9999",
	}
	if cfg != want {
		t.Errorf("expected %+v, got %+v", want, cfg)
	}
}

func TestClientConfigFromCLI_Empty(t *testing.T) {
	cfg := clientConfigFromCLI(&CLI{})
	if cfg != (fsapi.ClientConfig{}) {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}
