package main

import (
	"context"

	"fsvc/cmd"
)

// version is overridden at build time via ldflags
// (go build -ldflags="-X main.version=v1.0.0"). It is intentionally
// referenced here so the linker target stays meaningful.
var version = "dev"

func main() {
	_ = version
	cmd.Execute(context.Background())
}
