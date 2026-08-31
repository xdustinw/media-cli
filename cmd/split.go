package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/splitcmd"
)

var (
	flagSplitOut string
	flagSplitYes bool
)

var splitCmd = &cobra.Command{
	Use:   `split <file> "<t1>,<t2>,…"`,
	Short: "Cut a media file at timestamps into numbered parts (stream copy)",
	Long: `split cuts <file> at the given timestamps and writes
<name>-Part1.<ext>, <name>-Part2.<ext>, … N timestamps produce N+1 parts.

Timestamps are seconds, MM:SS or HH:MM:SS (fractions allowed), comma separated:

  mc split trip.mp4 "1:20,2:30,4:50"
  mc split trip.mp4 "90,210" -o ~/clips

The streams are copied without re-encoding, so each part starts at the nearest
keyframe at or after its timestamp. Parts land in -o/--outputFolder, or the
file's own folder.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("yes") {
			vip.Set(config.KeyAssumeYes, flagSplitYes)
		}
		in := bufio.NewReader(cmd.InOrStdin())
		out := cmd.OutOrStdout()
		return splitcmd.Run(cmd.Context(), splitcmd.Options{
			File:      args[0],
			Cuts:      args[1],
			OutputDir: flagSplitOut,
			AssumeYes: vip.GetBool(config.KeyAssumeYes),
			Stdout:    out,
			Stderr:    cmd.ErrOrStderr(),
			Confirm: func(prompt string) (bool, error) {
				fmt.Fprint(out, prompt)
				line, rerr := in.ReadString('\n')
				if rerr != nil && line == "" {
					return false, nil
				}
				a := strings.ToLower(strings.TrimSpace(line))
				return a == "y" || a == "yes", nil
			},
		})
	},
}

func init() {
	splitCmd.Flags().StringVarP(&flagSplitOut, "outputFolder", "o", "", "where to write the parts (default: the file's folder)")
	splitCmd.Flags().BoolVarP(&flagSplitYes, "yes", "y", false, "write without confirmation")
	rootCmd.AddCommand(splitCmd)
}
