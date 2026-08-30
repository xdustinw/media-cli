package cmd

import (
	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/setcmd"
	"github.com/xdustinw/media-cli/internal/tag"
)

var (
	flagSetSelect    string
	flagSetYes       bool
	flagSetRecursive bool
)

var setCmd = &cobra.Command{
	Use:   "set <key=value,...> [folder]",
	Short: "Write chosen metadata onto matching media files in a folder",
	Long: `set writes one or more metadata key/value pairs onto the media files in
[folder] (default: current directory). Only that folder's own files are
considered; pass -r/--recursive to descend into subdirectories.

  mc set 'rating=3,author=Adam' ~/Photos --select='name=3*Adam*'

Values may contain spaces; wrap a value in double quotes to keep a comma inside
it ('author="Doe, Jane"'). Video files are remuxed with stream copy (pixels and
streams untouched); image tags are stored in the file's native text area (PNG
tEXt, JPEG COM, GIF comment, WebP chunk).

You are strongly encouraged to pass --select — without it every media file in
the folder is updated. The change is previewed and confirmed (unless -y).`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		tags, err := tag.Parse(args[0])
		if err != nil {
			return err
		}
		return setcmd.Run(cmd.Context(), setcmd.Options{
			Target:     argOr(args, 1, "."),
			Tags:       tags,
			Select:     flagSetSelect,
			Extensions: vip.GetStringSlice(config.KeyMediaExts),
			AssumeYes:  flagSetYes,
			Recursive:  flagSetRecursive,
			Stdout:     cmd.OutOrStdout(),
			Stderr:     cmd.ErrOrStderr(),
			Confirm: func(prompt string) (bool, error) {
				return confirm(cmd, prompt), nil
			},
		})
	},
}

func init() {
	setCmd.Flags().StringVar(&flagSetSelect, "select", "", "only update files matching this filter (see `mc list --help`)")
	setCmd.Flags().BoolVarP(&flagSetYes, "yes", "y", false, "apply without confirmation")
	setCmd.Flags().BoolVarP(&flagSetRecursive, "recursive", "r", false, "descend into subdirectories")
	rootCmd.AddCommand(setCmd)
}
