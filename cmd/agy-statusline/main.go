package main

import (
	"fmt"
	"os"
	"time"

	"github.com/aklkbqx/agy-statusline/internal/cli"
	"github.com/aklkbqx/agy-statusline/internal/hud"
)

var (
	version = "0.1.1"
	buildID = "dev"
)

func main() {
	// Hot path when invoked by Antigravity CLI with piped JSON on stdin and no CLI flags.
	if len(os.Args) == 1 {
		if info, err := os.Stdin.Stat(); err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
			if out := hud.RenderReader(os.Stdin, time.Now().UTC(), os.Getenv); out != "" {
				fmt.Println(out)
			}
			return
		}
	}

	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version, buildID, os.Getenv))
}
