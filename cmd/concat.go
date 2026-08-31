package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/concatcmd"
	"github.com/xdustinw/media-cli/internal/config"
)

var (
	flagConcatOut string
	flagConcatYes bool
)

var concatCmd = &cobra.Command{
	Use:   "concat <file1> <file2> [<file3> ...]",
	Short: "Join media files into one",
	Long: `concat joins two or more media files, in order, into a single output.

When the inputs share codec and parameters they are stream-copied. When they
differ, a warning is shown and every input is re-encoded to MPEG-4 video + AAC
audio at the first file's geometry (this build has no H.264 encoder, so expect
a quality drop) before being joined.

  mc concat clip1.mp4 clip2.mp4 clip3.mp4
  mc concat a.mp4 b.mp4 -o joined.mp4

Without -o/--outputFile the result is <file1>-<file2>-…-combined.<ext>.`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("yes") {
			vip.Set(config.KeyAssumeYes, flagConcatYes)
		}
		in := bufio.NewReader(cmd.InOrStdin())
		out := cmd.OutOrStdout()
		return concatcmd.Run(cmd.Context(), concatcmd.Options{
			Inputs:     args,
			OutputFile: flagConcatOut,
			AssumeYes:  vip.GetBool(config.KeyAssumeYes),
			Stdout:     out,
			Stderr:     cmd.ErrOrStderr(),
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
	concatCmd.Flags().StringVarP(&flagConcatOut, "outputFile", "o", "", "output path (default: <file1>-<file2>-…-combined.<ext>)")
	concatCmd.Flags().BoolVarP(&flagConcatYes, "yes", "y", false, "write without confirmation")
	rootCmd.AddCommand(concatCmd)
}
