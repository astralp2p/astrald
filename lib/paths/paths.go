package paths

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotAbsolute indicates that a path is relative when an absolute path was required.
var ErrNotAbsolute = errors.New("path is not absolute")

// PathUnder checks if path is equal to root or a descendant of root using the given separator.
func PathUnder(path, root string, sep rune) bool {
	if path == root {
		return true
	}
	if root == string(sep) {
		return true
	}
	return strings.HasPrefix(path, root+string(sep))
}

// WidestRoots filters the list to only roots that are not descendants of other roots.
func WidestRoots(roots []string) []string {
	var widest []string
	for _, root := range roots {
		underOther := false
		for _, other := range roots {
			if root != other && PathUnder(root, other, filepath.Separator) {
				underOther = true
				break
			}
		}
		if !underOther {
			widest = append(widest, root)
		}
	}
	return widest
}

// PathTrie provides efficient coverage checks for path subtrees.
type PathTrie struct {
	sep       rune
	children  map[string]*PathTrie
	isEnd     bool // marks this node as a subtree root; all descendants are covered
	coversAll bool // set when root is inserted
}

// NewPathTrie builds an immutable trie from absolute paths where each path covers itself and all descendants.
// The sep parameter specifies the path separator (e.g., filepath.Separator for filesystem, '/' for tree paths).
func NewPathTrie(paths []string, sep rune) (*PathTrie, error) {
	t := &PathTrie{sep: sep, children: make(map[string]*PathTrie)}
	for _, path := range paths {
		if err := t.insert(path); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// isAbsolute checks if path starts with the separator.
func (t *PathTrie) isAbsolute(path string) bool {
	return len(path) > 0 && rune(path[0]) == t.sep
}

// splitPath splits an absolute path into normalized segments, returning ErrNotAbsolute for relative paths.
func (t *PathTrie) splitPath(path string) ([]string, error) {
	if !t.isAbsolute(path) {
		return nil, ErrNotAbsolute
	}
	parts := strings.Split(path, string(t.sep))

	// Normalize: remove empty, ".", and resolve ".."
	var normalized []string
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(normalized) > 0 {
				normalized = normalized[:len(normalized)-1]
			}
		default:
			normalized = append(normalized, part)
		}
	}
	return normalized, nil
}

// insert adds path as a subtree root, returning ErrNotAbsolute for relative paths.
func (t *PathTrie) insert(path string) error {
	parts, err := t.splitPath(path)
	if err != nil {
		return err
	}

	// Root splits to [] or ["", ""]
	isRoot := len(parts) == 0 || (len(parts) == 2 && parts[0] == "" && parts[1] == "")
	if isRoot {
		t.coversAll = true
		return nil
	}

	node := t
	for _, part := range parts {
		if part == "" {
			continue
		}
		if node.children[part] == nil {
			node.children[part] = &PathTrie{sep: t.sep, children: make(map[string]*PathTrie)}
		}
		node = node.children[part]
	}
	node.isEnd = true
	return nil
}

// Covers reports whether path falls under any registered subtree root, returning ErrNotAbsolute for relative paths.
func (t *PathTrie) Covers(path string) (bool, error) {
	parts, err := t.splitPath(path)
	if err != nil {
		return false, err
	}

	if t.coversAll {
		return true, nil
	}

	node := t
	for _, part := range parts {
		if part == "" {
			continue
		}
		if node.isEnd {
			return true, nil
		}
		node = node.children[part]
		if node == nil {
			return false, nil
		}
	}
	return node.isEnd, nil
}

// WalkDir walks the directory tree from root using breadth-first traversal,
// calling fn for each regular file. Errors reading directories are skipped.
// This avoids keeping many directory handles open, unlike filepath.WalkDir.
func WalkDir(ctx context.Context, root string, fn func(path string, info os.FileInfo) error) error {
	queue := []string{root}

	for len(queue) > 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		dir := queue[0]
		queue = queue[1:]

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())

			if entry.IsDir() {
				queue = append(queue, path)
			} else if entry.Type().IsRegular() {
				info, err := entry.Info()
				if err != nil {
					continue // file deleted between ReadDir and Info
				}
				if err := fn(path, info); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
