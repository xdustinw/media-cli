package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/copycmd"
)

var (
	flagCopyMode string
	flagCopyYes  bool
	flagMoveMode string
	flagMoveYes  bool
)

const copyMoveLong = `%[1]s brings every file from <source> (a file or folder) into <target>,
recursively. A file is put at <target>/<path relative to source>.

Before anything is written, each source file's ".<6-hex>" short hash (as written
by 'mc hash') is compared against the short hashes of the files already under
<target>, at any depth. When a match is found you resolve the duplicate:

  o / overwrite       copy the source bytes over the matching target file
                      (the target keeps its folder and name)
  s / skip-duplicate  leave the target as it is; do not bring the source in
  r / rename          rename the matching target file to the source's name
                      (its folder is unchanged); the bytes are left alone

Pass -m/--mode to apply one choice to every duplicate. With -y the plan is
applied without the final confirmation and duplicates default to overwrite.
%[2]s`

var copyCmd = &cobra.Command{
	Use:   "copy <source> <target>",
	Short: "Copy files into a folder, resolving name-hash duplicates first",
	Long:  fmt.Sprintf(copyMoveLong, "copy", "Files are copied; the source is left in place."),
	Args:  cobra.ExactArgs(2),
	RunE:  runCopyMove(false),
}

var moveCmd = &cobra.Command{
	Use:   "move <source> <target>",
	Short: "Move files into a folder, resolving name-hash duplicates first",
	Long:  fmt.Sprintf(copyMoveLong, "move", "Files are moved; a source file is removed once its target is written (skipped duplicates are left in the source)."),
	Args:  cobra.ExactArgs(2),
	RunE:  runCopyMove(true),
}

func runCopyMove(move bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		rawMode, yes := flagCopyMode, flagCopyYes
		if move {
			rawMode, yes = flagMoveMode, flagMoveYes
		}

		mode, err := copycmd.ParseMode(rawMode)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("yes") {
			vip.Set(config.KeyAssumeYes, yes)
		}

		// One reader for the whole command: bufio reads ahead, so a second
		// reader on the same stdin would race it for buffered input.
		in := bufio.NewReader(cmd.InOrStdin())
		out := cmd.OutOrStdout()
		readLine := func(prompt string) (string, bool) {
			fmt.Fprint(out, prompt)
			line, rerr := in.ReadString('\n')
			if rerr != nil && line == "" {
				return "", false
			}
			return strings.ToLower(strings.TrimSpace(line)), true
		}

		return copycmd.Run(cmd.Context(), copycmd.Options{
			Source:    args[0],
			Target:    args[1],
			Move:      move,
			Mode:      mode,
			AssumeYes: vip.GetBool(config.KeyAssumeYes),
			Stdout:    out,
			Stderr:    cmd.ErrOrStderr(),
			Confirm: func(prompt string) (bool, error) {
				a, ok := readLine(prompt)
				return ok && (a == "y" || a == "yes"), nil
			},
			Ask: func(prompt string) (copycmd.Mode, error) {
				for {
					a, ok := readLine(prompt)
					if !ok {
						return copycmd.ModeSkipDuplicate, nil
					}
					switch a {
					case "o", "overwrite":
						return copycmd.ModeOverwrite, nil
					case "s", "skip", "skip-duplicate":
						return copycmd.ModeSkipDuplicate, nil
					case "r", "rename":
						return copycmd.ModeRename, nil
					}
					fmt.Fprintln(out, "  please answer o, s or r")
				}
			},
		})
	}
}

func init() {
	copyCmd.Flags().StringVarP(&flagCopyMode, "mode", "m", "",
		"resolve every duplicate this way: overwrite|skip-duplicate|rename (o|s|r)")
	copyCmd.Flags().BoolVarP(&flagCopyYes, "yes", "y", false, "apply without confirmation (duplicates default to overwrite)")
	moveCmd.Flags().StringVarP(&flagMoveMode, "mode", "m", "",
		"resolve every duplicate this way: overwrite|skip-duplicate|rename (o|s|r)")
	moveCmd.Flags().BoolVarP(&flagMoveYes, "yes", "y", false, "apply without confirmation (duplicates default to overwrite)")
	rootCmd.AddCommand(copyCmd)
	rootCmd.AddCommand(moveCmd)
}
