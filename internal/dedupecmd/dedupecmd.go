// Package dedupecmd implements `mc dedupe`: find files that share a ".<6-hex>"
// short hash in their name (written by `mc hash`) across one or more folders and
// delete all but one copy of each.
//
// Which copy is kept is decided interactively (the default) or by a rule:
// longer-name, newer, older, or "protect the n-th folder" (f<n>). The full set
// of deletions is shown as a TOON preview and confirmed before anything is
// removed (unless -y).
package dedupecmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xdustinw/media-cli/internal/media"
	"github.com/xdustinw/media-cli/internal/mediainfo"
	"github.com/xdustinw/media-cli/internal/query"
	"github.com/xdustinw/media-cli/internal/toon"
)

const hashLen = 6

// Keep is the rule for choosing which copy of a duplicate set to keep.
type Keep string

const (
	KeepInteractive Keep = ""            // ask per set (default)
	KeepLongerName  Keep = "longer-name" // keep the longest file name
	KeepNewer       Keep = "newer"       // keep the newest mtime
	KeepOlder       Keep = "older"       // keep the oldest mtime
	// A value of the form "folder<n>" protects the n-th folder given on the
	// command line: its copies are never deleted, and a duplicate anywhere else
	// is. Build one with KeepFolder.
)

// KeepFolder returns the rule that protects the n-th folder (1-based).
func KeepFolder(n int) Keep { return Keep("folder" + strconv.Itoa(n)) }

// folderIndex returns the 0-based folder this rule protects, and true, when the
// rule is a "folder<n>" rule.
func (k Keep) folderIndex() (int, bool) {
	rest, ok := strings.CutPrefix(string(k), "folder")
	if !ok || rest == "" {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 {
		return 0, false
	}
	return n - 1, true
}

// ParseKeep accepts i/l/n/o and their long spellings, and folder<n> / f<n>;
// "" stays interactive.
func ParseKeep(s string) (Keep, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	switch t {
	case "", "i", "interactive":
		return KeepInteractive, nil
	case "l", "longer-name", "longer-filename", "longest-name":
		return KeepLongerName, nil
	case "n", "newer", "newer-wins":
		return KeepNewer, nil
	case "o", "older", "older-wins":
		return KeepOlder, nil
	}
	for _, prefix := range []string{"folder", "f"} {
		if rest, ok := strings.CutPrefix(t, prefix); ok && rest != "" {
			if n, err := strconv.Atoi(rest); err == nil && n >= 1 {
				return KeepFolder(n), nil
			}
		}
	}
	return "", fmt.Errorf("unknown --keep %q (want interactive|longer-name|newer|older|f<n>)", s)
}

func (k Keep) label() string {
	switch k {
	case KeepLongerName:
		return "longer name wins"
	case KeepNewer:
		return "newer wins"
	case KeepOlder:
		return "older wins"
	case KeepInteractive:
		return "interactive"
	}
	if n, ok := k.folderIndex(); ok {
		return fmt.Sprintf("folder %d kept intact", n+1)
	}
	return string(k)
}

// Options configures a Run.
type Options struct {
	Folders   []string
	Keep      Keep
	Select    string
	Recursive bool

	AssumeYes bool
	Stdout    io.Writer
	Stderr    io.Writer
	Confirm   func(prompt string) (bool, error)
	// Ask presents one duplicate set and returns the 1-based index of the file
	// to keep, or 0 to leave the whole set alone.
	Ask func(prompt string, count int) (int, error)
	// PreHash fingerprints and renames the given unhashed files in place (see
	// hashcmd.HashInPlace). When nil, unhashed files are grouped by name.
	PreHash func(ctx context.Context, files []string) (int, error)
	Logger  *slog.Logger
}

type fileInfo struct {
	path    string
	rel     string
	size    int64
	modTime time.Time
	folder  int // index into Options.Folders that first yielded this file
}

type dupSet struct {
	key   string // short hash, or (fallback) the shared file name
	files []fileInfo
	del   []int // indices in files to delete; empty => set left alone
}

// keeper returns the display path of a surviving file in the set (the first one
// not slated for deletion), for the preview's "keep" column.
func (s dupSet) keeper() string {
	doomed := make(map[int]bool, len(s.del))
	for _, i := range s.del {
		doomed[i] = true
	}
	for i, f := range s.files {
		if !doomed[i] {
			return f.rel
		}
	}
	return ""
}

// Run finds duplicate sets, resolves which copies to keep, previews the
// deletions and (on confirmation) removes the rest.
func Run(ctx context.Context, o Options) error {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	if len(o.Folders) == 0 {
		o.Folders = []string{"."}
	}
	folderIdx, folderMode := o.Keep.folderIndex()
	if folderMode && folderIdx >= len(o.Folders) {
		return fmt.Errorf("--keep f%d: only %d folder(s) given", folderIdx+1, len(o.Folders))
	}

	sel, err := query.ParseSelect(o.Select)
	if err != nil {
		return fmt.Errorf("--select: %w", err)
	}

	// Gather every file across the folders (with its filesystem facts).
	gather := func() ([]fileInfo, error) {
		var all []fileInfo
		seen := map[string]struct{}{}
		for fIdx, folder := range o.Folders {
			files, ferr := media.WalkFiles(ctx, folder, o.Recursive)
			if ferr != nil {
				return nil, fmt.Errorf("reading %s: %w", folder, ferr)
			}
			base := media.DisplayBase(folder)
			for _, f := range files {
				if _, dup := seen[f]; dup {
					continue
				}
				facts, e := media.StatFacts(f)
				if e != nil {
					continue
				}
				if sel != nil && !sel.Match(facts) {
					continue
				}
				seen[f] = struct{}{}
				all = append(all, fileInfo{
					path: f, rel: media.RelTo(base, f), size: facts.Size,
					modTime: facts.ModTime, folder: fIdx,
				})
			}
		}
		return all, nil
	}

	all, err := gather()
	if err != nil {
		return err
	}

	// Offer to hash the files that carry no short hash.
	byName := false
	var noHash []string
	for _, fi := range all {
		if media.ShortHashInName(fi.path, hashLen) == "" {
			noHash = append(noHash, fi.path)
		}
	}
	if len(noHash) > 0 {
		doHash := o.AssumeYes && o.PreHash != nil
		if !o.AssumeYes && o.PreHash != nil {
			doHash = confirmed(o, fmt.Sprintf(
				"%d file(s) have no content hash. Hash them first? [y/N]  (n = group by file name) ", len(noHash)))
		}
		if doHash {
			n, herr := o.PreHash(ctx, noHash)
			if herr != nil {
				return fmt.Errorf("hashing unhashed files: %w", herr)
			}
			fmt.Fprintf(o.Stderr, "  hashed %d file(s)\n", n)
			if all, err = gather(); err != nil {
				return err
			}
		} else {
			byName = true
			fmt.Fprintln(o.Stderr, "  grouping by file name (unhashed files not fingerprinted)")
		}
	}

	// Group into duplicate sets.
	groups := map[string][]fileInfo{}
	for _, fi := range all {
		key := media.ShortHashInName(fi.path, hashLen)
		if key == "" {
			if !byName {
				continue
			}
			key = "name:" + filepath.Base(fi.path)
		}
		groups[key] = append(groups[key], fi)
	}

	var sets []dupSet
	for k, fs := range groups {
		if len(fs) < 2 {
			continue
		}
		sort.Slice(fs, func(i, j int) bool { return fs[i].path < fs[j].path })
		sets = append(sets, dupSet{key: strings.TrimPrefix(k, "name:"), files: fs})
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].key < sets[j].key })

	if len(sets) == 0 {
		fmt.Fprintln(o.Stdout, "No duplicate sets found.")
		return nil
	}

	// Decide which copies to delete in each set.
	for i := range sets {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch {
		case folderMode:
			sets[i].del = pickByFolder(folderIdx, sets[i].files)
		case o.Keep == KeepInteractive:
			idx, aerr := askKeep(o, sets[i])
			if aerr != nil {
				return aerr
			}
			if idx >= 1 {
				sets[i].del = allExcept(len(sets[i].files), idx-1)
			}
		default:
			sets[i].del = allExcept(len(sets[i].files), pickByRule(o.Keep, sets[i].files))
		}
	}

	// Build the deletion list.
	type del struct{ set, file int }
	var dels []del
	for si, s := range sets {
		for _, fi := range s.del {
			dels = append(dels, del{si, fi})
		}
	}
	if len(dels) == 0 {
		fmt.Fprintln(o.Stdout, "Nothing to delete.")
		return nil
	}

	// Preview.
	doc := &toon.Document{}
	doc.AddField("keep", o.Keep.label())
	doc.AddField("duplicate-sets", fmt.Sprint(len(sets)))
	tbl := toon.Table{Name: "delete", Columns: []string{"set", "keep", "delete"}}
	for _, d := range dels {
		s := sets[d.set]
		tbl.Rows = append(tbl.Rows, []string{s.key, s.keeper(), s.files[d.file].rel})
	}
	doc.AddTable(tbl)
	fmt.Fprint(o.Stdout, doc.String())

	if !o.AssumeYes {
		ok := false
		if o.Confirm != nil {
			var cErr error
			ok, cErr = o.Confirm(fmt.Sprintf("\nDelete %d file(s)? [y/N] ", len(dels)))
			if cErr != nil {
				return cErr
			}
		}
		if !ok {
			fmt.Fprintln(o.Stdout, "Aborted; nothing deleted.")
			return nil
		}
	}

	start := time.Now()
	var removed, failed int
	var bytes int64
	for _, d := range dels {
		if err := ctx.Err(); err != nil {
			return err
		}
		f := sets[d.set].files[d.file]
		if err := os.Remove(f.path); err != nil {
			failed++
			fmt.Fprintf(o.Stderr, "  ! %s: %v\n", f.rel, err)
			continue
		}
		removed++
		bytes += f.size
		fmt.Fprintf(o.Stdout, "  ✓ deleted %s\n", f.rel)
		log.Info("dedupe", "deleted", f.path, "key", sets[d.set].key)
	}

	fmt.Fprintln(o.Stderr, media.Summary(removed, bytes, time.Since(start)))
	if failed > 0 {
		return fmt.Errorf("%d file(s) could not be deleted", failed)
	}
	return nil
}

// confirmed runs o.Confirm; a nil callback or any error counts as "no".
func confirmed(o Options, prompt string) bool {
	if o.Confirm == nil {
		return false
	}
	ok, err := o.Confirm(prompt)
	return ok && err == nil
}

// allExcept returns [0,n) with keep removed.
func allExcept(n, keep int) []int {
	out := make([]int, 0, n-1)
	for i := 0; i < n; i++ {
		if i != keep {
			out = append(out, i)
		}
	}
	return out
}

// pickByFolder returns the indices to delete when the n-th folder is protected:
// every file outside that folder, but only when the set actually has a copy
// inside it (otherwise the set is left untouched).
func pickByFolder(folderIdx int, files []fileInfo) []int {
	protected := false
	for _, f := range files {
		if f.folder == folderIdx {
			protected = true
			break
		}
	}
	if !protected {
		return nil
	}
	var out []int
	for i, f := range files {
		if f.folder != folderIdx {
			out = append(out, i)
		}
	}
	return out
}

// askKeep renders one set and asks which copy to keep (0 => skip the set).
func askKeep(o Options, s dupSet) (int, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "\nduplicate set %s — %d copies:\n", s.key, len(s.files))
	for i, f := range s.files {
		fmt.Fprintf(&b, "  %d) %s   (%s, %s)\n", i+1, f.rel,
			mediainfo.HumanSize(f.size), f.modTime.Format("2006-01-02"))
	}
	fmt.Fprintf(&b, "keep which? [1-%d, s = skip this set] ", len(s.files))
	if o.Ask == nil {
		return 0, nil
	}
	return o.Ask(b.String(), len(s.files))
}

// pickByRule returns the index of the file to keep under a non-interactive rule.
func pickByRule(k Keep, files []fileInfo) int {
	best := 0
	for i := 1; i < len(files); i++ {
		switch k {
		case KeepLongerName:
			if len(filepath.Base(files[i].path)) > len(filepath.Base(files[best].path)) {
				best = i
			}
		case KeepNewer:
			if files[i].modTime.After(files[best].modTime) {
				best = i
			}
		case KeepOlder:
			if files[i].modTime.Before(files[best].modTime) {
				best = i
			}
		}
	}
	return best
}
