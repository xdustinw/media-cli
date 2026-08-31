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
	flagAssumeYes  bool
	flagHashForce  bool
	flagHashNR     bool
	flagHashMethod string
	flagHashSelect string
)

var hashCmd = &cobra.Command{
	Use:   "hash [file or folder ...]",
	Short: "Fingerprint media files by content and rename them",
	Long: `hash fingerprints each media file and renames it to

    <name>.<first 6 of hash>.<ext>

(replacing any short hash already in the name). One or more files or folders may
be given (default: the current directory); folders are scanned recursively
unless --nr is passed.

The -m/--method flag selects the fingerprint:

  (default)   try ffmpeg-10m, falling back to md5-10m for a file ffmpeg cannot
              read
  ffmpeg-10m  md5 of the first ~10 MB of the encoded video+audio stream — fast
              on large files; rename only
  ffmpeg      md5 of the whole video+audio stream (for video) or the decoded
              pixels (for images); metadata is ignored, so two files that
              differ only in metadata match. This method also writes the value
              into the file as the 'mc.hash' tag
  md5 / sha   md5 / sha-256 of the raw file bytes (metadata included); rename only
  md5-10m /
  sha-10m     md5 / sha-256 of the first 10 MB of raw file bytes; rename only

Only the 'ffmpeg' method reads or writes file metadata; the others just rename.

--select filters the files (fields: name, path, ext, size, modifiedAt, kind);
you are shown the matches and asked to confirm before hashing, unless -y.

Files that already carry a valid 6-hex slot in their name are left untouched
without being hashed (for the 'ffmpeg' method: a file with a valid 'mc.hash' tag
is trusted). Pass -f/--force to re-hash them and re-check. Pass -y to skip
confirmations.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("yes") {
			vip.Set(config.KeyAssumeYes, flagAssumeYes)
		}

		method, err := hashcmd.ParseMethod(flagHashMethod)
		if err != nil {
			return err
		}

		targets := args
		if len(targets) == 0 {
			targets = []string{"."}
		}

		in := bufio.NewReader(cmd.InOrStdin())
		out := cmd.OutOrStdout()
		readLine := func(prompt string) string {
			fmt.Fprint(out, prompt)
			line, rerr := in.ReadString('\n')
			if rerr != nil && line == "" {
				return ""
			}
			return strings.ToLower(strings.TrimSpace(line))
		}
		opts := hashcmd.Options{
			Targets:     targets,
			Extensions:  vip.GetStringSlice(config.KeyMediaExts),
			Method:      method,
			Select:      flagHashSelect,
			MetadataKey: vip.GetString(config.KeyHashMetaKey),
			NameLength:  vip.GetInt(config.KeyHashNameLen),
			AssumeYes:   vip.GetBool(config.KeyAssumeYes),
			Force:       flagHashForce,
			Recursive:   !flagHashNR,
			Stdout:      out,
			Stderr:      cmd.ErrOrStderr(),
			Confirm: func(prompt string) (bool, error) {
				a := readLine(prompt)
				return a == "y" || a == "yes", nil
			},
			OnCollision: func(incoming, existing string) (hashcmd.CollisionAction, error) {
				for {
					switch readLine(fmt.Sprintf(
						"  ! %s already exists. [o]verwrite / [s]kip (keep both) / [d]elete un-hashed file ? (default d) ",
						existing)) {
					case "o", "overwrite":
						return hashcmd.CollisionOverwrite, nil
					case "s", "skip":
						return hashcmd.CollisionSkip, nil
					case "", "d", "delete":
						return hashcmd.CollisionDelete, nil
					}
					fmt.Fprintln(out, "  please answer o, s or d")
				}
			},
		}
		return hashcmd.Run(cmd.Context(), opts)
	},
}

func init() {
	hashCmd.Flags().BoolVarP(&flagAssumeYes, "yes", "y", false, "skip confirmation and apply changes")
	hashCmd.Flags().BoolVarP(&flagHashForce, "force", "f", false, "re-hash files that already have an mc.hash tag")
	hashCmd.Flags().BoolVar(&flagHashNR, "nr", false, "non-recursive: stay in each folder, skip subfolders")
	hashCmd.Flags().StringVarP(&flagHashMethod, "method", "m", "",
		"hash method: ffmpeg, ffmpeg-10m, md5, sha, md5-10m, sha-10m (default: ffmpeg-10m with md5-10m fallback)")
	hashCmd.Flags().StringVar(&flagHashSelect, "select", "", "only hash files matching this filter")
	_ = vip.BindPFlag(config.KeyAssumeYes, hashCmd.Flags().Lookup("yes"))
	rootCmd.AddCommand(hashCmd)
}
