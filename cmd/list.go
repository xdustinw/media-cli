package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/listcmd"
	"github.com/xdustinw/media-cli/internal/render"
)

var (
	flagListMeta   string
	flagListSortBy string
	flagListSelect string
	flagListFormat string
)

var listCmd = &cobra.Command{
	Use:   "list <folder>",
	Short: "List media files in a folder with size and metadata",
	Long: `list walks <folder> recursively. Each file row is:
filename, size, mc.hash, rating, authors, tags.
toon/json nest the rows under their folders; csv is one flat table (absolute paths).

  --meta      extra metadata columns, comma separated (e.g. --meta=title,make,model)
  --select    keep only matching files, e.g.
              --select='name=sample* and rating>=4 and size>1g and modifiedAt>2026-08-01'
              fields: name, path, size, rating, modifiedAt, kind, format, authors,
              tags, or any metadata key. ops: = != > < >= <=  ( = is a
              case-insensitive * / ? wildcard match ).
              combine with 'and' / 'or'.
  --sort-by   comma separated keys with optional 'desc', e.g.
              --sort-by='rating desc, size desc, name'
  --format    toon (default), json, or csv (csv uses absolute paths)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := render.ParseFormat(flagListFormat, render.TOON, render.JSON, render.CSV)
		if err != nil {
			return err
		}
		return listcmd.Run(cmd.Context(), listcmd.Options{
			Root:    args[0],
			HashKey: vip.GetString(config.KeyHashMetaKey),
			Meta:    splitCSVFlag(flagListMeta),
			SortBy:  flagListSortBy,
			Select:  flagListSelect,
			Format:  format,
			Stdout:  cmd.OutOrStdout(),
			Stderr:  cmd.ErrOrStderr(),
		})
	},
}

func splitCSVFlag(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func init() {
	f := listCmd.Flags()
	f.StringVar(&flagListMeta, "meta", "", "extra metadata columns, comma separated")
	f.StringVar(&flagListSortBy, "sort-by", "", "sort keys, e.g. 'rating desc, size desc'")
	f.StringVar(&flagListSelect, "select", "", "filter expression")
	f.StringVar(&flagListFormat, "format", "toon", "output format: toon, json, csv")
	rootCmd.AddCommand(listCmd)
}
