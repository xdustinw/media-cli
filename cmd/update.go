package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/selfupdate"
	"github.com/xdustinw/media-cli/internal/toon"
)

var (
	flagUpdateYes     bool
	flagUpdatePreview bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update mc to the latest GitHub release",
	Long: `update checks https://github.com/` + selfupdate.Repo + `/releases for a
newer release and, on confirmation, replaces the running mc binary in place —
wherever it currently sits on your PATH.

Only stable releases are considered by default. A newer preview (pre-release) is
always reported: when there is no stable update it is offered instead, and when
a stable update is offered the preview is noted so you can re-run with --preview.

--preview targets the newest preview build on GitHub and offers it **regardless
of the installed version** — so it can also be used to switch a stable install
onto the preview channel, or to re-install the current preview. With -y that
install happens without a prompt.

If the installed version is already current (and --preview was not given),
nothing is downloaded.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		out := cmd.OutOrStdout()

		exePath, err := selfupdate.TargetPath()
		if err != nil {
			return err
		}

		fmt.Fprintln(out, "Checking for updates…")
		stable, preview, err := selfupdate.LatestReleases(ctx)
		if err != nil {
			return fmt.Errorf("checking releases: %w", err)
		}

		installed := Version()
		norm := selfupdate.NormalizeVersion
		newer := func(r *selfupdate.Release) bool {
			return r != nil && selfupdate.IsNewer(norm(r.TagName), norm(installed))
		}

		fmt.Fprintf(out, "  %-11s %s\n", "installed:", installed)
		fmt.Fprintf(out, "  %-11s %s\n", "stable:", tagOrNone(stable))
		if preview != nil {
			fmt.Fprintf(out, "  %-11s %s\n", "preview:", preview.TagName)
		}

		skipPrompt := flagUpdateYes || vip.GetBool(config.KeyAssumeYes)

		var target *selfupdate.Release
		forcePreview := false
		switch {
		case flagUpdatePreview:
			if preview == nil {
				fmt.Fprintln(out, "\nNo preview (pre-release) release is published.")
				return nil
			}
			// --preview offers the newest preview no matter the installed version.
			target, forcePreview = preview, true
		case newer(stable):
			target = stable
		case newer(preview):
			// Nothing newer on the stable channel, but a preview is available.
			if skipPrompt {
				fmt.Fprintf(out, "\nOn the latest stable release. Preview %s is available — "+
					"run 'mc update --preview' to install it.\n", preview.TagName)
				return nil
			}
			target = preview
		}

		if target == nil || (!forcePreview && !newer(target)) {
			fmt.Fprintln(out, "\nAlready up to date.")
			return nil
		}

		// Heading for stable while a still-newer preview exists: point it out.
		if target == stable && newer(preview) &&
			selfupdate.Compare(norm(preview.TagName), norm(stable.TagName)) > 0 {
			fmt.Fprintf(out, "\nA newer preview release %s is also available (run 'mc update --preview').\n",
				preview.TagName)
		}

		asset := target.FindAsset()
		if asset == nil {
			return fmt.Errorf("release %s has no asset %q for %s/%s",
				target.TagName, selfupdate.AssetName(), runtime.GOOS, runtime.GOARCH)
		}

		doc := &toon.Document{}
		doc.AddField("binary", exePath)
		doc.AddField("from", installed)
		doc.AddField("to", target.TagName)
		if target.Prerelease {
			doc.AddField("channel", "preview")
		}
		doc.AddField("download", asset.Name)

		header, prompt := "\nA newer version is available:", fmt.Sprintf("\nUpdate to %s? [y/N] ", target.TagName)
		if forcePreview && !newer(target) {
			header = "\nPreview build:"
			prompt = fmt.Sprintf("\nInstall preview %s (installed: %s)? [y/N] ", target.TagName, installed)
		}
		fmt.Fprintln(out, header)
		fmt.Fprint(out, doc.String())

		if !skipPrompt && !confirm(cmd, prompt) {
			fmt.Fprintln(out, "Aborted; binary unchanged.")
			return nil
		}

		fmt.Fprintf(out, "\nDownloading %s…\n", asset.Name)
		if err := selfupdate.Apply(ctx, asset.BrowserDownloadURL, exePath, norm(installed)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Updated %s to %s.\n", exePath, target.TagName)
		return nil
	},
}

func tagOrNone(r *selfupdate.Release) string {
	if r == nil {
		return "(none found)"
	}
	return r.TagName
}

func init() {
	updateCmd.Flags().BoolVarP(&flagUpdateYes, "yes", "y", false, "update without confirmation")
	updateCmd.Flags().BoolVar(&flagUpdatePreview, "preview", false, "update to the newest preview (pre-release) build")
	rootCmd.AddCommand(updateCmd)
}
