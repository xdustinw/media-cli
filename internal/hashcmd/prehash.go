package hashcmd

import (
	"context"
	"log/slog"

	"github.com/xdustinw/media-cli/internal/media"
)

// HashInPlace fingerprints each file with the default (auto) method — try
// ffmpeg-10m, fall back to md5-10m — and renames it to
// <name>.<first 6 of hash>.<ext>. It never writes metadata and never prompts;
// files that already carry a short hash in their name, or that cannot be
// hashed, are skipped (the latter logged). It returns how many files it
// renamed.
//
// It exists so `mc copy` / `mc move` / `mc dedupe` can offer to hash the
// unhashed files in a folder before comparing by content.
func HashInPlace(ctx context.Context, files []string, nameLength int, log *slog.Logger) (int, error) {
	if log == nil {
		log = slog.Default()
	}
	if nameLength <= 0 || nameLength > 32 {
		nameLength = 6
	}

	renamed := 0
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return renamed, err
		}
		if media.ShortHashInName(f, nameLength) != "" {
			continue
		}
		h, _, err := MethodAuto.resolve(f, media.KindOf(f))
		if err != nil {
			log.Warn("prehash: could not hash, skipped", "file", f, "err", err)
			continue
		}
		newPath := media.HashedName(f, h[:nameLength])
		if newPath == f {
			continue
		}
		if err := media.SwapInPlace(f, newPath, false); err != nil {
			log.Warn("prehash: rename failed", "file", f, "err", err)
			continue
		}
		log.Info("prehash", "from", f, "to", newPath)
		renamed++
	}
	return renamed, nil
}
