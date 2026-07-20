package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// runSync overwrites every vendorDirs entry with an exact copy of
// canonicalDir.
func runSync() error {
	for _, dst := range vendorDirs {
		if err := copyTree(canonicalDir, dst); err != nil {
			return fmt.Errorf("syncing %s: %w", dst, err)
		}
		fmt.Println("synced", canonicalDir, "->", dst)
	}
	return nil
}

// copyTree replaces dst with an exact copy of the file tree rooted at src.
func copyTree(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// treesEqual reports whether src and dst contain the same set of relative
// paths with byte-identical content, and describes every mismatch found.
func treesEqual(src, dst string) (equal bool, diffs []string) {
	if _, err := os.Stat(dst); err != nil {
		return false, []string{fmt.Sprintf("%s does not exist (run: go run ./internal/skilltool sync)", dst)}
	}

	srcFiles := map[string][]byte{}
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		srcFiles[rel] = data
		return nil
	})
	if err != nil {
		return false, []string{err.Error()}
	}

	dstFiles := map[string][]byte{}
	err = filepath.WalkDir(dst, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dst, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dstFiles[rel] = data
		return nil
	})
	if err != nil {
		return false, []string{err.Error()}
	}

	for rel, want := range srcFiles {
		got, ok := dstFiles[rel]
		switch {
		case !ok:
			diffs = append(diffs, fmt.Sprintf("%s: missing %s", dst, rel))
		case !bytes.Equal(want, got):
			diffs = append(diffs, fmt.Sprintf("%s: %s content differs from %s", dst, rel, src))
		}
		delete(dstFiles, rel)
	}
	for rel := range dstFiles {
		diffs = append(diffs, fmt.Sprintf("%s: extra file %s not present in %s", dst, rel, src))
	}

	return len(diffs) == 0, diffs
}
