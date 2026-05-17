package main

import (
	"fmt"
	"os"

	"github.com/AIluffy/tildewire/internal/launcher"
)

var version = "dev"

func main() {
	if err := launcher.Run("tildewire", version, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
