package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/hashcmd"
	"github.com/xdustinw/media-cli/internal/listmissingcmd"
)

var (
	flagListMissingYes    bool
	flagListMissingNR     bool
	flagListMissingSelect string
)

var listMissingCmd = &cobra.Command{
	Use:     "list-missing <src-folder> <target-folder> [<target-folder> ...]",
	Aliases: []string{"find-missing"},
	Short:   "List source files whose content hash is in no target folder",
	Long: `list-missing walks <src-folder> and reports every file whose ".<6-hex>"
short hash (from 'mc hash') is not present on any file under the target
folder(s). The source folder is scanned recursively unless --nr is passed;
target folders are always scanned recursively.

When files in the source or the targets carry no ".<6-hex>" slot, list-missing
offers to hash them first ('mc hash', in place); decline and the comparison
falls back to the base file name. -y hashes them all automatically.

--select filters the source files (fields: name, path, ext, size, modifiedAt,
kind). The result is printed as a TOON table; nothing is modified aside from an
accepted hash pass.`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("yes") {
			vip.Set(config.KeyAssumeYes, flagListMissingYes)
		}

		in := bufio.NewReader(cmd.InOrStdin())
		out := cmd.OutOrStdout()
		return listmissingcmd.Run(cmd.Context(), listmissingcmd.Options{
			Source:    args[0],
			Targets:   args[1:],
			Select:    flagListMissingSelect,
			Recursive: !flagListMissingNR,
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
		})
	},
}

func init() {
	listMissingCmd.Flags().BoolVarP(&flagListMissingYes, "yes", "y", false, "hash unhashed files without prompting")
	listMissingCmd.Flags().BoolVar(&flagListMissingNR, "nr", false, "non-recursive: skip source subfolders")
	listMissingCmd.Flags().StringVar(&flagListMissingSelect, "select", "", "only consider source files matching this filter")
	rootCmd.AddCommand(listMissingCmd)
}
