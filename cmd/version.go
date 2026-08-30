package cmd

import (
	_ "embed"
	"fmt"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
)

//go:embed version.txt
var versionRaw string

// buildVersion, when set via -ldflags, overrides the embedded version.txt. The
// release workflow passes the release tag (or a preview string) here:
//
//	-ldflags "-X github.com/xdustinw/media-cli/cmd.buildVersion=v1.2.3"
var buildVersion string

// Version returns the effective version string: the ldflags override when
// present, otherwise the trimmed contents of cmd/version.txt.
func Version() string {
	if v := strings.TrimSpace(buildVersion); v != "" {
		return v
	}
	return strings.TrimSpace(versionRaw)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the media-cli version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "mc %s (%s/%s, %s)\n",
			Version(), runtime.GOOS, runtime.GOARCH, runtime.Version())
		fmt.Fprintf(out, "bundled: %s\n", ffmpeg.BuildInfo())
		fmt.Fprintln(out, "license: mc is MIT; bundled FFmpeg is LGPL-2.1-or-later — see THIRD-PARTY-NOTICES.md")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
