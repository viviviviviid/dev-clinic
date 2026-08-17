// Package pathguard confines filesystem operations to a configured base directory.
package pathguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrOutsideBase = errors.New("path is outside base directory")

// Resolve returns candidate as a canonical absolute path after confirming that
// it is inside base. Existing symlinks are evaluated, including the closest
// existing parent when candidate does not exist yet.
func Resolve(base, candidate string) (string, error) {
	if strings.TrimSpace(base) == "" {
		return "", errors.New("base directory is empty")
	}
	if strings.TrimSpace(candidate) == "" {
		return "", errors.New("path is empty")
	}

	basePath, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolve base directory: %w", err)
	}
	basePath, err = resolveExistingPrefix(basePath)
	if err != nil {
		return "", fmt.Errorf("resolve base directory symlinks: %w", err)
	}

	target := candidate
	if !filepath.IsAbs(target) {
		target = filepath.Join(basePath, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	target, err = resolveExistingPrefix(target)
	if err != nil {
		return "", fmt.Errorf("resolve path symlinks: %w", err)
	}

	rel, err := filepath.Rel(basePath, target)
	if err != nil {
		return "", fmt.Errorf("compare path with base directory: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s", ErrOutsideBase, candidate)
	}

	return filepath.Clean(target), nil
}

// ResolveChild is Resolve with an additional guard that rejects the base
// directory itself. Use it for destructive operations.
func ResolveChild(base, candidate string) (string, error) {
	basePath, err := Resolve(base, base)
	if err != nil {
		return "", err
	}
	target, err := Resolve(basePath, candidate)
	if err != nil {
		return "", err
	}
	if target == basePath {
		return "", errors.New("path must be a child of base directory")
	}
	return target, nil
}

// ResolveChildNoSymlinks confines candidate to a child of base without
// following any existing symbolic-link component. The final path may be
// missing so callers can use it for safe creates and rename destinations.
// Callers that require an existing target must still Lstat the returned path.
func ResolveChildNoSymlinks(base, candidate string) (string, error) {
	if strings.TrimSpace(base) == "" {
		return "", errors.New("base directory is empty")
	}
	if strings.TrimSpace(candidate) == "" {
		return "", errors.New("path is empty")
	}

	basePath, err := Resolve(base, base)
	if err != nil {
		return "", err
	}
	baseInfo, err := os.Lstat(basePath)
	if err != nil {
		return "", fmt.Errorf("inspect base directory: %w", err)
	}
	if !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("base path is not a regular directory")
	}

	target := candidate
	if filepath.IsAbs(target) {
		// Preserve an absolute path expressed through the configured base alias
		// (for example /var versus /private/var on macOS), then walk the
		// canonical base below without following child symlinks.
		originalBase, absErr := filepath.Abs(base)
		candidateAbs, candidateErr := filepath.Abs(target)
		if absErr != nil || candidateErr != nil {
			return "", errors.New("resolve absolute path")
		}
		if rel, relErr := filepath.Rel(originalBase, candidateAbs); relErr == nil && isChildRelative(rel) {
			target = filepath.Join(basePath, rel)
		} else {
			target = candidateAbs
		}
	} else {
		target = filepath.Join(basePath, target)
	}
	target = filepath.Clean(target)
	rel, err := filepath.Rel(basePath, target)
	if err != nil || !isChildRelative(rel) {
		return "", fmt.Errorf("%w: %s", ErrOutsideBase, candidate)
	}

	current := basePath
	parts := strings.Split(rel, string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return target, nil
		}
		if statErr != nil {
			return "", fmt.Errorf("inspect path component %q: %w", current, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symbolic links are not allowed: %s", current)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("path component is not a directory: %s", current)
		}
	}
	return target, nil
}

func isChildRelative(rel string) bool {
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ResolveRelative confines a model- or client-provided relative filename to
// base. Parent traversal is rejected even when cleaning would remain in base.
func ResolveRelative(base, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", errors.New("absolute paths are not allowed")
	}
	for _, part := range strings.FieldsFunc(filepath.ToSlash(name), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return "", errors.New("parent traversal is not allowed")
		}
	}
	return ResolveChild(base, name)
}

func resolveExistingPrefix(path string) (string, error) {
	path = filepath.Clean(path)
	current := path
	var missing []string

	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
