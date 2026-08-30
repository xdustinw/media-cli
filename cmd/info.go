package cmd

import (
	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/infocmd"
	"github.com/xdustinw/media-cli/internal/render"
)

var flagInfoFormat string

var infoCmd = &cobra.Command{
	Use:   "info <file>",
	Short: "Print all metadata, file info and encoding details for one file",
	Long: `info shows everything known about <file>: path, size and modified time;
container format, duration and bitrate; every stream's codec and parameters;
and all embedded metadata (including image EXIF / PNG text).

  --format   toon (default) or json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := render.ParseFormat(flagInfoFormat, render.TOON, render.JSON)
		if err != nil {
			return err
		}
		return infocmd.Run(cmd.Context(), infocmd.Options{
			Path:   args[0],
			Format: format,
			Stdout: cmd.OutOrStdout(),
			Stderr: cmd.ErrOrStderr(),
		})
	},
}

func init() {
	infoCmd.Flags().StringVar(&flagInfoFormat, "format", "toon", "output format: toon or json")
	rootCmd.AddCommand(infoCmd)
}
