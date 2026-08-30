// Command mc (media-cli) is a small toolkit for content-addressable media
// operations built on top of the FFmpeg libraries.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/xdustinw/media-cli/cmd"
)

func main() {
	// A cancellable context wired to SIGINT/SIGTERM so long-running media
	// operations can unwind cleanly on Ctrl+C.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.Execute(ctx); err != nil {
		// Cobra already prints the error to stderr; just pick the exit code.
		os.Exit(1)
	}
}
