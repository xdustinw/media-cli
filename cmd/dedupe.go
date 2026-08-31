package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/dedupecmd"
	"github.com/xdustinw/media-cli/internal/hashcmd"
)

var (
	flagDedupeMethod string
	flagDedupeYes    bool
	flagDedupeNR     bool
	flagDedupeSelect string
)

var dedupeCmd = &cobra.Command{
	Use:   "dedupe [folder ...]",
	Short: "Delete duplicate copies of hash-named files across folders",
	Long: `dedupe groups files by the ".<6-hex>" short hash in their name (written by
'mc hash') across the given folders (default: the current directory), and
deletes all but one copy of each set. Folders are scanned recursively unless
--nr is passed.

Which copy is kept:

  i / interactive  (default) you are shown each set and pick which to keep
                   (1-n), or skip the set
  l / longer-name  keep the file with the longest name
  n / newer        keep the most recently modified file
  o / older        keep the oldest file

--select filters the files (fields: name, path, ext, size, modifiedAt, kind).
The full list of deletions is shown as a TOON preview and confirmed before
anything is removed, unless -y.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		method, err := dedupecmd.ParseMethod(flagDedupeMethod)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("yes") {
			vip.Set(config.KeyAssumeYes, flagDedupeYes)
		}

		folders := args
		if len(folders) == 0 {
			folders = []string{"."}
		}

		in := bufio.NewReader(cmd.InOrStdin())
		out := cmd.OutOrStdout()
		return dedupecmd.Run(cmd.Context(), dedupecmd.Options{
			Folders:   folders,
			Method:    method,
			Select:    flagDedupeSelect,
			Recursive: !flagDedupeNR,
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
			PreHash: func(ctx context.Context, files []string) (int, error) {
				return hashcmd.HashInPlace(ctx, files, vip.GetInt(config.KeyHashNameLen), slog.Default())
			},
			Ask: func(prompt string, count int) (int, error) {
				for {
					fmt.Fprint(out, prompt)
					line, rerr := in.ReadString('\n')
					if rerr != nil && line == "" {
						return 0, nil
					}
					a := strings.ToLower(strings.TrimSpace(line))
					if a == "s" || a == "skip" || a == "" {
						return 0, nil
					}
					if n, cerr := strconv.Atoi(a); cerr == nil && n >= 1 && n <= count {
						return n, nil
					}
					fmt.Fprintf(out, "  please enter a number 1-%d, or s to skip\n", count)
				}
			},
		})
	},
}

func init() {
	dedupeCmd.Flags().StringVarP(&flagDedupeMethod, "method", "m", "",
		"which copy to keep: interactive (default), longer-name, newer, older (i|l|n|o)")
	dedupeCmd.Flags().BoolVarP(&flagDedupeYes, "yes", "y", false, "delete without the confirmation prompt")
	dedupeCmd.Flags().BoolVar(&flagDedupeNR, "nr", false, "non-recursive: skip subfolders")
	dedupeCmd.Flags().StringVar(&flagDedupeSelect, "select", "", "only consider files matching this filter")
	rootCmd.AddCommand(dedupeCmd)
}
