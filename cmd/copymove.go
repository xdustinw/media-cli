package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/copycmd"
	"github.com/xdustinw/media-cli/internal/hashcmd"
)

var (
	flagCopyMode      string
	flagCopyYes       bool
	flagCopyNR        bool
	flagCopySelect    string
	flagMoveMode      string
	flagMoveYes       bool
	flagMoveNR        bool
	flagMoveSelect    string
	flagMoveDeleteSrc bool
)

const copyMoveLong = `%[1]s brings every file from the source(s) — files and/or folders — into
<target>, at <target>/<path relative to that source>. Folders are scanned
recursively unless --nr is passed.

Before anything is written, each source file's ".<6-hex>" short hash (as written
by 'mc hash') is compared against the short hashes of the files already under
<target>, at any depth. --mode says what to do with every match:

  s / skip-duplicate  (default) leave the target; keep the source where it is
  o / overwrite        copy the source bytes over the matching target file
  k / keep-both        bring the source in too, under its own name/path

--select filters the source files (fields: name, path, ext, size, modifiedAt,
kind); you are shown the matches and asked to confirm before the hash compare,
unless -y. -y also skips the final confirmation.
%[2]s`

var copyCmd = &cobra.Command{
	Use:   "copy <source> [<source> ...] <target>",
	Short: "Copy files into a folder, resolving name-hash duplicates",
	Long:  fmt.Sprintf(copyMoveLong, "copy", "Files are copied; sources are left in place."),
	Args:  cobra.MinimumNArgs(2),
	RunE:  runCopyMove(false),
}

var moveCmd = &cobra.Command{
	Use:   "move <source> [<source> ...] <target>",
	Short: "Move files into a folder, resolving name-hash duplicates",
	Long:  fmt.Sprintf(copyMoveLong, "move", "Files are moved; a source is removed once its target is written. On a skipped duplicate the source is left in place (not deleted) unless --delete-source is passed, which removes every matching source regardless of a duplicate under the target."),
	Args:  cobra.MinimumNArgs(2),
	RunE:  runCopyMove(true),
}

func runCopyMove(move bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		rawMode, yes, nr, sel := flagCopyMode, flagCopyYes, flagCopyNR, flagCopySelect
		if move {
			rawMode, yes, nr, sel = flagMoveMode, flagMoveYes, flagMoveNR, flagMoveSelect
		}

		mode, err := copycmd.ParseMode(rawMode)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("yes") {
			vip.Set(config.KeyAssumeYes, yes)
		}

		sources, target := args[:len(args)-1], args[len(args)-1]

		in := bufio.NewReader(cmd.InOrStdin())
		out := cmd.OutOrStdout()
		return copycmd.Run(cmd.Context(), copycmd.Options{
			Sources:      sources,
			Target:       target,
			Move:         move,
			Mode:         mode,
			Select:       sel,
			Recursive:    !nr,
			Verbose:      vip.GetBool(config.KeyVerbose),
			DeleteSource: move && flagMoveDeleteSrc,
			AssumeYes:    vip.GetBool(config.KeyAssumeYes),
			Stdout:       out,
			Stderr:       cmd.ErrOrStderr(),
			Confirm: func(prompt string) (bool, error) {
				fmt.Fprint(out, prompt)
				line, rerr := in.ReadString('\n')
				if rerr != nil && line == "" {
					return false, nil
				}
				a := strings.ToLower(strings.TrimSpace(line))
				return a == "y" || a == "yes", nil
			},
			PreHash: func(ctx context.Context, files []string) (int, error) {
				return hashcmd.HashInPlace(ctx, files, vip.GetInt(config.KeyHashNameLen), slog.Default())
			},
		})
	}
}

func init() {
	for _, c := range []struct {
		cmd  *cobra.Command
		mode *string
		yes  *bool
		nr   *bool
		sel  *string
	}{
		{copyCmd, &flagCopyMode, &flagCopyYes, &flagCopyNR, &flagCopySelect},
		{moveCmd, &flagMoveMode, &flagMoveYes, &flagMoveNR, &flagMoveSelect},
	} {
		c.cmd.Flags().StringVarP(c.mode, "mode", "m", "",
			"duplicate handling: skip-duplicate (default), overwrite, keep-both (s|o|k)")
		c.cmd.Flags().BoolVarP(c.yes, "yes", "y", false, "skip confirmations")
		c.cmd.Flags().BoolVar(c.nr, "nr", false, "non-recursive: skip source subfolders")
		c.cmd.Flags().StringVar(c.sel, "select", "", "only act on source files matching this filter")
		rootCmd.AddCommand(c.cmd)
	}
	moveCmd.Flags().BoolVar(&flagMoveDeleteSrc, "delete-source", false,
		"delete matching source files even when the target already holds a duplicate")
}
