package main

import (
	"context"

	"fsvc/cmd"
)

func main() {
	cmd.Execute(context.Background())
}
