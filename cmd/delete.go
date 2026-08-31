package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/deletecmd"
)

var (
	flagDeleteSelect string
	flagDeleteYes    bool
	flagDeleteNR     bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete [folder ...] --select=<expr>",
	Short: "Delete the files under folders that match a --select filter",
	Long: `delete removes the files under the given folders (default: the current
directory) that match --select. Folders are scanned recursively unless --nr.

--select is required — it is the only thing that decides which files go. Fields:
name, path, ext, size, modifiedAt, kind; operators = != > < >= <= (= is a
case-insensitive * / ? wildcard); combine with 'and' / 'or'.

  mc delete ~/incoming --select='name=IMG_2* or name=IMG_3*'
  mc delete . --select='ext=tmp and size<1k'

The full list is shown as a TOON preview and confirmed before anything is
removed, unless -y.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagDeleteSelect == "" {
			return fmt.Errorf("--select is required")
		}
		if cmd.Flags().Changed("yes") {
			vip.Set(config.KeyAssumeYes, flagDeleteYes)
		}

		folders := args
		if len(folders) == 0 {
			folders = []string{"."}
		}

		in := bufio.NewReader(cmd.InOrStdin())
		out := cmd.OutOrStdout()
		return deletecmd.Run(cmd.Context(), deletecmd.Options{
			Folders:   folders,
			Select:    flagDeleteSelect,
			Recursive: !flagDeleteNR,
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
	deleteCmd.Flags().StringVar(&flagDeleteSelect, "select", "", "REQUIRED: which files to delete")
	deleteCmd.Flags().BoolVarP(&flagDeleteYes, "yes", "y", false, "delete without the confirmation prompt")
	deleteCmd.Flags().BoolVar(&flagDeleteNR, "nr", false, "non-recursive: skip subfolders")
	rootCmd.AddCommand(deleteCmd)
}
