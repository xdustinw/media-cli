package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/selfupdate"
	"github.com/xdustinw/media-cli/internal/toon"
)

var flagUpdateYes bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update mc to the latest GitHub release",
	Long: `update checks https://github.com/` + selfupdate.Repo + `/releases for a
newer release and, on confirmation, replaces the running mc binary in place —
wherever it currently sits on your PATH.

If the installed version is already current, nothing is downloaded. Pass -y to
update without the confirmation prompt.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		out := cmd.OutOrStdout()

		exePath, err := selfupdate.TargetPath()
		if err != nil {
			return err
		}

		fmt.Fprintln(out, "Checking for updates…")
		rel, err := selfupdate.LatestRelease(ctx)
		if err != nil {
			return fmt.Errorf("checking latest release: %w", err)
		}

		installed := Version()
		fmt.Fprintf(out, "  installed: %s\n  latest:    %s\n", installed, rel.TagName)

		if !selfupdate.IsNewer(selfupdate.NormalizeVersion(rel.TagName), selfupdate.NormalizeVersion(installed)) {
			fmt.Fprintln(out, "\nAlready up to date.")
			return nil
		}

		asset := rel.FindAsset()
		if asset == nil {
			return fmt.Errorf("release %s has no asset %q for %s/%s",
				rel.TagName, selfupdate.AssetName(), runtime.GOOS, runtime.GOARCH)
		}

		doc := &toon.Document{}
		doc.AddField("binary", exePath)
		doc.AddField("from", installed)
		doc.AddField("to", rel.TagName)
		doc.AddField("download", asset.Name)
		fmt.Fprintln(out, "\nA newer version is available:")
		fmt.Fprint(out, doc.String())

		skip := flagUpdateYes || vip.GetBool(config.KeyAssumeYes)
		if !skip && !confirm(cmd, fmt.Sprintf("\nUpdate to %s? [y/N] ", rel.TagName)) {
			fmt.Fprintln(out, "Aborted; binary unchanged.")
			return nil
		}

		fmt.Fprintf(out, "\nDownloading %s…\n", asset.Name)
		if err := selfupdate.Apply(ctx, asset.BrowserDownloadURL, exePath, selfupdate.NormalizeVersion(installed)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Updated %s to %s.\n", exePath, rel.TagName)
		return nil
	},
}

func init() {
	updateCmd.Flags().BoolVarP(&flagUpdateYes, "yes", "y", false, "update without confirmation")
	rootCmd.AddCommand(updateCmd)
}
