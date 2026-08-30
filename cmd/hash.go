package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xdustinw/media-cli/internal/config"
	"github.com/xdustinw/media-cli/internal/hashcmd"
)

var (
	flagAssumeYes     bool
	flagHashForce     bool
	flagHashRecursive bool
	flagHashMethod    string
)

var hashCmd = &cobra.Command{
	Use:   "hash [file or folder]",
	Short: "Fingerprint media files by content and rename them",
	Long: `hash fingerprints each media file and renames it to

    <name>.<first 6 of hash>.<ext>

(replacing any short hash already in the name). The -m/--method flag selects the
fingerprint:

  ffmpeg-10m  (default) md5 of the first ~10 MB of the encoded video+audio
              stream — fast on large files; rename only
  ffmpeg      md5 of the whole video+audio stream (for video) or the decoded
              pixels (for images); metadata is ignored, so two files that
              differ only in metadata match. This method also writes the value
              into the file as the 'mc.hash' tag
  md5 / sha   md5 / sha-256 of the raw file bytes (metadata included); rename only
  md5-10m /
  sha-10m     md5 / sha-256 of the first 10 MB of raw file bytes; rename only

Only the 'ffmpeg' method reads or writes file metadata; the others just rename.

Given a directory, only that directory's own files are processed; pass
-r/--recursive to descend into subdirectories. The target defaults to the
current directory. For the 'ffmpeg' method a file that already carries a valid
'mc.hash' tag is trusted and not re-hashed; pass -f/--force to re-compute and
compare. Pass -y to skip the confirmation.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("yes") {
			vip.Set(config.KeyAssumeYes, flagAssumeYes)
		}

		in := bufio.NewReader(cmd.InOrStdin())
		method, err := hashcmd.ParseMethod(flagHashMethod)
		if err != nil {
			return err
		}

		opts := hashcmd.Options{
			Target:      argOr(args, 0, "."),
			Extensions:  vip.GetStringSlice(config.KeyMediaExts),
			Method:      method,
			MetadataKey: vip.GetString(config.KeyHashMetaKey),
			NameLength:  vip.GetInt(config.KeyHashNameLen),
			AssumeYes:   vip.GetBool(config.KeyAssumeYes),
			Force:       flagHashForce,
			Recursive:   flagHashRecursive,
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
	hashCmd.Flags().BoolVarP(&flagHashForce, "force", "f", false, "re-hash files that already have an mc.hash tag")
	hashCmd.Flags().BoolVarP(&flagHashRecursive, "recursive", "r", false, "descend into subdirectories")
	hashCmd.Flags().StringVarP(&flagHashMethod, "method", "m", "",
		"hash method: ffmpeg, ffmpeg-10m (default), md5, sha, md5-10m, sha-10m")
	_ = vip.BindPFlag(config.KeyAssumeYes, hashCmd.Flags().Lookup("yes"))
	rootCmd.AddCommand(hashCmd)
}
