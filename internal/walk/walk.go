// Package walk turns a directory tree into a deterministic, filtered list of
// candidate files plus a complete record of everything it skipped and why.
package walk

import (
	_ "embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evan-william/codepack-lz/internal/model"
)

//go:embed default-ignore.txt
var defaultIgnoreText string

// IgnoreFileName is the per-repo ignore file honored at the pack root.
const IgnoreFileName = ".codepackignore"

// Options controls filtering. Zero value means: default ignores on, no
// include/exclude globs, no size cap.
type Options struct {
	Include         []string // if non-empty, only files matching one of these are packed
	Exclude         []string // extra globs to exclude
	NoDefaultIgnore bool     // disable the embedded default ignore list
	MaxFileSize     int64    // skip files larger than this; 0 = unlimited
	OutputPath      string   // absolute path of the output file, for self-exclusion
}

// Entry is a candidate file that passed every name/size filter. Content
// filters (binary detection) run later, when the file is read.
type Entry struct {
	RelPath string // slash-separated, relative to the root
	AbsPath string
	Size    int64
}

// Result is the deterministic outcome of a walk: entries and skips are both
// sorted by path ascending.
type Result struct {
	Root    string // base name of the walked root directory
	AbsRoot string
	Entries []Entry
	Skips   []model.Skip
}

// Collect walks root and applies the filter chain. Filter precedence per
// file: symlink, self-output, default ignore, .codepackignore, --exclude,
// --include, size cap. The first matching filter records the skip reason.
func Collect(root string, opts Options) (*Result, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", root, err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	defaultRules := &Ruleset{}
	if !opts.NoDefaultIgnore {
		defaultRules, err = ParseRules(strings.NewReader(defaultIgnoreText), "default-ignore")
		if err != nil {
			return nil, err // unreachable unless the embedded list is broken
		}
	}

	ignoreRules := &Ruleset{}
	if f, err := os.Open(filepath.Join(absRoot, IgnoreFileName)); err == nil {
		ignoreRules, err = ParseRules(f, IgnoreFileName)
		closeErr := f.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s: %w", IgnoreFileName, closeErr)
		}
	}

	include, err := compileGlobs(opts.Include, "--include")
	if err != nil {
		return nil, err
	}
	exclude, err := compileGlobs(opts.Exclude, "--exclude")
	if err != nil {
		return nil, err
	}

	// Self-exclusion: never pack the file we are writing to, even when the
	// default ignore list (which covers *.codepack.txt) is disabled.
	outputRel := ""
	if opts.OutputPath != "" {
		if rel, err := filepath.Rel(absRoot, opts.OutputPath); err == nil && !strings.HasPrefix(rel, "..") {
			outputRel = filepath.ToSlash(rel)
		}
	}

	res := &Result{Root: filepath.Base(absRoot), AbsRoot: absRoot}

	err = filepath.WalkDir(absRoot, func(p string, entry fs.DirEntry, walkErr error) error {
		if p == absRoot {
			return walkErr
		}
		rel, relErr := filepath.Rel(absRoot, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		if walkErr != nil {
			// Record unreadable paths instead of aborting the whole pack.
			res.Skips = append(res.Skips, model.Skip{Path: rel, Reason: model.SkipUnreadable})
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if entry.IsDir() {
			switch {
			case defaultRules.Match(rel, true):
				res.Skips = append(res.Skips, model.Skip{Path: rel + "/", Reason: model.SkipDefaultIgnore})
				return filepath.SkipDir
			case ignoreRules.Match(rel, true):
				res.Skips = append(res.Skips, model.Skip{Path: rel + "/", Reason: model.SkipIgnoreFile})
				return filepath.SkipDir
			case exclude.matchDir(rel):
				res.Skips = append(res.Skips, model.Skip{Path: rel + "/", Reason: model.SkipExcludeFlag})
				return filepath.SkipDir
			}
			return nil
		}

		// Symlinks are never followed: a link could point outside the root or
		// create cycles, and its target is not part of this tree's content.
		if entry.Type()&fs.ModeSymlink != 0 {
			res.Skips = append(res.Skips, model.Skip{Path: rel, Reason: model.SkipSymlink})
			return nil
		}

		fi, err := entry.Info()
		if err != nil {
			res.Skips = append(res.Skips, model.Skip{Path: rel, Reason: model.SkipUnreadable})
			return nil
		}
		size := fi.Size()

		switch {
		case outputRel != "" && rel == outputRel:
			res.Skips = append(res.Skips, model.Skip{Path: rel, Reason: model.SkipSelfOutput, Size: size})
		case defaultRules.Match(rel, false):
			res.Skips = append(res.Skips, model.Skip{Path: rel, Reason: model.SkipDefaultIgnore, Size: size})
		case ignoreRules.Match(rel, false):
			res.Skips = append(res.Skips, model.Skip{Path: rel, Reason: model.SkipIgnoreFile, Size: size})
		case exclude.match(rel):
			res.Skips = append(res.Skips, model.Skip{Path: rel, Reason: model.SkipExcludeFlag, Size: size})
		case len(include) > 0 && !include.match(rel):
			res.Skips = append(res.Skips, model.Skip{Path: rel, Reason: model.SkipNotIncluded, Size: size})
		case opts.MaxFileSize > 0 && size > opts.MaxFileSize:
			res.Skips = append(res.Skips, model.Skip{Path: rel, Reason: model.SkipMaxFileSize, Size: size})
		default:
			res.Entries = append(res.Entries, Entry{RelPath: rel, AbsPath: p, Size: size})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}

	// Walk order varies by OS and filesystem; sorted order is the contract.
	sort.Slice(res.Entries, func(i, j int) bool { return res.Entries[i].RelPath < res.Entries[j].RelPath })
	sort.Slice(res.Skips, func(i, j int) bool { return res.Skips[i].Path < res.Skips[j].Path })
	return res, nil
}
