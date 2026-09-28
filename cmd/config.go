package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type ConfigCmdGroup struct {
	Path  ConfigPathCmd  `cmd:"" help:"Show configuration file path"`
	Show  ConfigShowCmd  `cmd:"" help:"Print current configuration values (secrets masked)"`
	Set   ConfigSetCmd   `cmd:"" help:"Set a config value"`
	Unset ConfigUnsetCmd `cmd:"" help:"Unset a config value"`
}

// configKeys are the settings the CLI reads from the config file. Anything else
// passed to 'config set' is a typo, so it is rejected instead of being stored
// where nothing will ever read it. TestConfigKeysMatchCLIFlags keeps this list
// aligned with the CLI's own flags.
var configKeys = []string{
	"base-url",
	"concurrency",
	"csrf-token",
	"itildesk-session",
	"subdomain",
	"time-zone",
}

// secretConfigKeys are never printed by 'config show' or echoed by
// 'config set'; only their presence is reported.
var secretConfigKeys = []string{
	"csrf-token",
	"itildesk-session",
}

type ConfigPathCmd struct{}

func (cmd *ConfigPathCmd) Run(app *App) error {
	p := app.CfgPath()
	if _, err := os.Stat(p); os.IsNotExist(err) {
		fmt.Printf("%s (does not exist)\n", p)
		return nil
	}
	fmt.Println(p)
	return nil
}

type ConfigShowCmd struct{}

func (cmd *ConfigShowCmd) Run(app *App) error {
	p := app.CfgPath()
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("%s (does not exist)\n", p)
			return nil
		}
		return fmt.Errorf("failed to read configuration file: %w", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		// The file is deliberately not echoed: an unparseable config may hold a
		// secret the masker never gets a chance to see.
		return fmt.Errorf("failed to parse configuration file: %w", err)
	}

	// SetEscapeHTML(false): the marker is <set>, not \u003cset\u003e.
	var shown bytes.Buffer
	enc := json.NewEncoder(&shown)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(maskSecrets(cfg)); err != nil {
		return fmt.Errorf("failed to render configuration: %w", err)
	}
	fmt.Print(shown.String())
	return nil
}

// maskSecrets replaces the value of every secret key with a marker.
func maskSecrets(cfg map[string]any) map[string]any {
	masked := make(map[string]any, len(cfg))
	for key, value := range cfg {
		masked[key] = value
	}
	for _, key := range secretConfigKeys {
		value, ok := masked[key]
		if !ok {
			continue
		}
		if s, isString := value.(string); isString && s == "" {
			masked[key] = "<empty>"
			continue
		}
		masked[key] = "<set>"
	}
	return masked
}

// isSecretConfigKey reports whether a key's value must not be echoed.
func isSecretConfigKey(key string) bool {
	for _, secret := range secretConfigKeys {
		if key == secret {
			return true
		}
	}
	return false
}

type ConfigSetCmd struct {
	Key   string `arg:"" help:"Configuration key"`
	Value string `arg:"" help:"Value to set (use '--' before a value that starts with a dash)"`
}

func (cmd *ConfigSetCmd) Run(app *App) error {
	if err := validateConfigKey(cmd.Key); err != nil {
		return err
	}

	p := app.CfgPath()
	cfgMap, err := loadConfigMap(p)
	if err != nil {
		return err
	}

	val := parseConfigValue(cmd.Value)
	cfgMap[cmd.Key] = val

	if err := saveConfigMap(p, cfgMap); err != nil {
		return err
	}

	if isSecretConfigKey(cmd.Key) && val != nil {
		fmt.Printf("Set %q = <set>\n", cmd.Key)
		return nil
	}
	fmt.Printf("Set %q = %v\n", cmd.Key, val)
	return nil
}

type ConfigUnsetCmd struct {
	Key string `arg:"" help:"Configuration key to unset"`
}

func (cmd *ConfigUnsetCmd) Run(app *App) error {
	if err := validateConfigKey(cmd.Key); err != nil {
		return err
	}

	p := app.CfgPath()
	cfgMap, err := loadConfigMap(p)
	if err != nil {
		return err
	}

	if _, ok := cfgMap[cmd.Key]; !ok {
		fmt.Printf("Key %q not found\n", cmd.Key)
		return nil
	}
	delete(cfgMap, cmd.Key)

	if err := saveConfigMap(p, cfgMap); err != nil {
		return err
	}
	fmt.Printf("Unset %q\n", cmd.Key)
	return nil
}

// validateConfigKey rejects keys the CLI would never read, so that a typo fails
// loudly instead of silently leaving the CLI unauthenticated.
func validateConfigKey(key string) error {
	for _, known := range configKeys {
		if key == known {
			return nil
		}
	}
	known := append([]string(nil), configKeys...)
	sort.Strings(known)
	return fmt.Errorf("unknown configuration key %q; valid keys: %s", key, strings.Join(known, ", "))
}

// parseConfigValue coerces a command-line string to the closest JSON type.
func parseConfigValue(raw string) any {
	switch raw {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return n
	}
	if n, err := strconv.ParseFloat(raw, 64); err == nil {
		return n
	}
	return raw
}

func loadConfigMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]any), nil
		}
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse configuration file: %w", err)
	}
	if m == nil {
		m = make(map[string]any)
	}
	return m, nil
}

func saveConfigMap(path string, m map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
