// Package cmd holds the Cobra command tree. Command files stay thin: parsing,
// wiring and user-facing error presentation only. Behaviour lives in
// internal/... packages.
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/ffmpeg"
	"github.com/xdustinw/media-cli/internal/logging"
)

// vip is created as a package-level initializer so it is ready before any
// init() in this package runs (Cobra sub-commands bind flags to it in init).
var vip = config.New()

var (
	flagConfig  string
	flagVerbose bool
	flagDebug   bool
)

var rootCmd = &cobra.Command{
	Use:   "mc",
	Short: "media-cli — content-addressable media operations powered by FFmpeg",
	Long: `mc (media-cli) performs metadata-independent operations on media files
using the FFmpeg libraries linked directly into the binary.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		if err := config.Load(vip, flagConfig); err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Flags win over config/env only when the user actually set them.
		if cmd.Flags().Changed("verbose") {
			vip.Set(config.KeyVerbose, flagVerbose)
		}
		if cmd.Flags().Changed("debug") {
			vip.Set(config.KeyDebug, flagDebug)
		}

		verbose := vip.GetBool(config.KeyVerbose)
		debug := vip.GetBool(config.KeyDebug)
		logging.Setup(cmd.ErrOrStderr(), logging.Options{Verbose: verbose, Debug: debug})
		if verbose || debug {
			ffmpeg.SetVerbose(debug)
		}
		return nil
	},
}

// Execute runs the root command with the given context.
func Execute(ctx context.Context) error {
	err := rootCmd.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	return err
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&flagConfig, "config", "", "path to config file (default: ./media-cli.yaml)")
	pf.BoolVarP(&flagVerbose, "verbose", "v", false, "verbose logging")
	pf.BoolVar(&flagDebug, "debug", false, "debug logging (JSON, with source)")

	_ = vip.BindPFlag(config.KeyVerbose, pf.Lookup("verbose"))
	_ = vip.BindPFlag(config.KeyDebug, pf.Lookup("debug"))
}
