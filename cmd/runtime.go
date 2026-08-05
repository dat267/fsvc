package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/kong"
)

// appName is used for config path resolution. SetAppName overrides it.
var appName = "fsvc"

// tz is set by Execute from the --tz flag before commands run.
var tz string

// now returns the current time; tests may replace it to pin the clock.
var now = time.Now

// nowInTZ returns the current time in the configured timezone (or local if unset).
func nowInTZ() time.Time {
	return NowInTZAt(tz, now())
}

const configFileFlagName = "config-file"

func SetAppName(name string) {
	if name != "" {
		appName = name
	}
}

// App carries the resolved config file path to commands.
type App struct {
	cfgPath string
}

func (a *App) CfgPath() string {
	if a == nil || a.cfgPath == "" {
		return resolveConfigPath()
	}
	return a.cfgPath
}

func resolveConfigPath() string {
	envKey := strings.ToUpper(appName) + "_CONFIG_FILE"
	if cf := os.Getenv(envKey); cf != "" {
		return cf
	}
	localFile := appName + ".json"
	if _, err := os.Stat(localFile); err == nil {
		return localFile
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, appName, appName+".json")
	}
	return localFile
}

func Execute(ctx context.Context) {
	app := &App{}
	if cf := resolveConfigFileFlag(); cf != "" {
		app.cfgPath = cf
	}
	activeConfig := app.CfgPath()

	cli := &CLI{}
	client := New(ClientConfig{})
	options := []kong.Option{
		kong.Name(appName),
		kong.Description("Freshservice private-API CLI"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.Bind(app),
		kong.Bind(client),
	}

	if f, err := os.Open(activeConfig); err == nil {
		if resolver, err := JSONResolver(f); err == nil {
			options = append(options, kong.Resolvers(resolver))
		}
		_ = f.Close()
	}

	k, err := kong.New(cli, options...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	kongCtx, err := k.Parse(os.Args[1:])
	k.FatalIfErrorf(err)

	app.cfgPath = cli.ConfigFile
	tz = cli.TimeZone
	client.Update(clientConfigFromCLI(cli))
	k.FatalIfErrorf(kongCtx.Run())
}

func resolveConfigFileFlag() string {
	for i, arg := range os.Args {
		if arg == "--"+configFileFlagName && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if strings.HasPrefix(arg, "--"+configFileFlagName+"=") {
			return strings.SplitN(arg, "=", 2)[1]
		}
	}
	return ""
}

// JSONResolver builds a Kong resolver capable of loading both flat and nested JSON configuration.
func JSONResolver(r io.Reader) (kong.Resolver, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	flat := make(map[string]any)

	var flattenNested func(prefix string, m map[string]any)
	flattenNested = func(prefix string, m map[string]any) {
		for k, v := range m {
			key := k
			if prefix != "" {
				key = prefix + "-" + k
			}
			if sub, ok := v.(map[string]any); ok {
				flattenNested(key, sub)
			} else if prefix != "" {
				flat[key] = v
			}
		}
	}
	flattenNested("", raw)

	for k, v := range raw {
		if _, isMap := v.(map[string]any); !isMap {
			flat[k] = v
		}
	}

	return kong.ResolverFunc(func(ctx *kong.Context, parent *kong.Path, flag *kong.Flag) (any, error) {
		if val, ok := flat[flag.Name]; ok {
			return val, nil
		}
		return nil, nil
	}), nil
}
