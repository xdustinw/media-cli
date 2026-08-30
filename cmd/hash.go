package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/hashcmd"
)

var flagAssumeYes bool

var hashCmd = &cobra.Command{
	Use:   "hash <file or folder>",
	Short: "Compute the metadata-independent MD5 of media files and tag them",
	Long: `hash calculates a metadata-independent MD5 for each media file:

  video (.mp4 .mkv .mov .m4v .webm .avi)  - over the encoded video+audio
                                            elementary streams; container
                                            metadata (ratings, tags) is ignored
  images (.jpg .jpeg .png .gif .webp)      - over the decoded pixel data; EXIF,
                                            XMP, ICC and text chunks are ignored

Two files that differ only in metadata produce the same hash.

Given a directory, matching files are processed recursively. After listing
"<hash> - <path>" for each file, you are asked to confirm writing the hash as a
freeform tag ('mc.hash=<hash>') and renaming each file to

    <name>.<first 6 of hash>.<ext>

Pass -y to skip the confirmation.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("yes") {
			vip.Set(config.KeyAssumeYes, flagAssumeYes)
		}

		in := bufio.NewReader(cmd.InOrStdin())
		opts := hashcmd.Options{
			Target:      args[0],
			Extensions:  vip.GetStringSlice(config.KeyMediaExts),
			MetadataKey: vip.GetString(config.KeyHashMetaKey),
			NameLength:  vip.GetInt(config.KeyHashNameLen),
			AssumeYes:   vip.GetBool(config.KeyAssumeYes),
			Stdout:      cmd.OutOrStdout(),
			Stderr:      cmd.ErrOrStderr(),
			Confirm: func(prompt string) (bool, error) {
				fmt.Fprint(cmd.OutOrStdout(), prompt)
				line, err := in.ReadString('\n')
				if err != nil && line == "" {
					return false, nil
				}
				answer := strings.ToLower(strings.TrimSpace(line))
				return answer == "y" || answer == "yes", nil
			},
		}
		return hashcmd.Run(cmd.Context(), opts)
	},
}

func init() {
	hashCmd.Flags().BoolVarP(&flagAssumeYes, "yes", "y", false, "skip confirmation and apply changes")
	_ = vip.BindPFlag(config.KeyAssumeYes, hashCmd.Flags().Lookup("yes"))
	rootCmd.AddCommand(hashCmd)
}
