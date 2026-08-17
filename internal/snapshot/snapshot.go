package snapshot

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/coding-tutor/internal/pathguard"
)

const (
	maxSnapshotFiles      = 512
	maxSnapshotFileBytes  = 4 << 20
	maxSnapshotTotalBytes = 32 << 20
)

var managedSourceExts = map[string]bool{
	".go": true, ".py": true, ".rs": true, ".sol": true,
	".ts": true, ".tsx": true, ".mts": true, ".cts": true,
	".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".html": true, ".css": true, ".scss": true, ".proto": true, ".sql": true,
}

var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, ".venv": true, "venv": true,
	"coverage": true,
}

func safeStepLabel(stepLabel string) (string, error) {
	label := strings.TrimSpace(stepLabel)
	if label == "" || label == "." || label == ".." {
		return "", errors.New("invalid snapshot label")
	}
	if strings.ContainsAny(label, `/\\`) || filepath.IsAbs(label) || filepath.VolumeName(label) != "" {
		return "", errors.New("snapshot label must not contain path separators")
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return "", errors.New("snapshot label contains control characters")
		}
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return '_'
		}
		return r
	}, label), nil
}

func snapshotDir(projectDir, stepLabel string) (string, error) {
	safe, err := safeStepLabel(stepLabel)
	if err != nil {
		return "", err
	}
	snapshotsRoot, err := pathguard.ResolveChildNoSymlinks(projectDir, ".snapshots")
	if err != nil {
		return "", fmt.Errorf("resolve snapshot root: %w", err)
	}
	if info, statErr := os.Lstat(snapshotsRoot); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("snapshot root is not a regular directory")
		}
		return pathguard.ResolveChildNoSymlinks(snapshotsRoot, safe)
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	return filepath.Join(snapshotsRoot, safe), nil
}

// Save copies managed source/runtime files plus TUTORSYS.md and quiz.json into
// projectDir/.snapshots/{stepLabel}/.
func Save(projectDir, stepLabel string) error {
	projectDir, err := pathguard.Resolve(projectDir, projectDir)
	if err != nil {
		return err
	}
	files, err := collectManagedFiles(projectDir, true)
	if err != nil {
		return err
	}
	snapshotsRoot, err := pathguard.ResolveChildNoSymlinks(projectDir, ".snapshots")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(snapshotsRoot, 0o755); err != nil {
		return err
	}
	rootInfo, err := os.Lstat(snapshotsRoot)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("snapshot root is not a regular directory")
	}
	dst, err := snapshotDir(projectDir, stepLabel)
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp(snapshotsRoot, ".snapshot-tmp-")
	if err != nil {
		return err
	}
	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			_ = os.RemoveAll(staging)
		}
	}()

	for _, rel := range sortedManagedPaths(files) {
		file := files[rel]
		dest, err := pathguard.ResolveChildNoSymlinks(staging, rel)
		if err != nil {
			return err
		}
		if err := copyFile(file.path, dest); err != nil {
			return err
		}
	}

	if existing, statErr := os.Lstat(dst); statErr == nil {
		if !existing.IsDir() || existing.Mode()&os.ModeSymlink != 0 {
			return errors.New("snapshot destination is not a regular directory")
		}
		backup, err := os.MkdirTemp(snapshotsRoot, ".snapshot-backup-")
		if err != nil {
			return err
		}
		if err := os.Remove(backup); err != nil {
			return err
		}
		if err := os.Rename(dst, backup); err != nil {
			return err
		}
		if err := os.Rename(staging, dst); err != nil {
			_ = os.Rename(backup, dst)
			return err
		}
		cleanupStaging = false
		_ = os.RemoveAll(backup)
		return nil
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if err := os.Rename(staging, dst); err != nil {
		return err
	}
	cleanupStaging = false
	return nil
}

// Restore makes the project's managed file set match the selected snapshot.
// Unmanaged user files and ignored dependency/build directories are preserved.
func Restore(projectDir, stepLabel string) error {
	projectDir, err := pathguard.Resolve(projectDir, projectDir)
	if err != nil {
		return err
	}
	src, err := snapshotDir(projectDir, stepLabel)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("snapshot is not a directory")
	}

	// Preflight both trees completely before mutating the project. This makes a
	// malformed/symlinked/oversized tree fail closed without a partial restore.
	snapshotFiles, err := collectManagedFiles(src, false)
	if err != nil {
		return fmt.Errorf("preflight snapshot: %w", err)
	}
	currentFiles, err := collectManagedFiles(projectDir, true)
	if err != nil {
		return fmt.Errorf("preflight current project: %w", err)
	}
	for rel := range snapshotFiles {
		if _, err := pathguard.ResolveChildNoSymlinks(projectDir, rel); err != nil {
			return fmt.Errorf("preflight restore destination %q: %w", rel, err)
		}
	}

	for _, rel := range sortedManagedPaths(currentFiles) {
		if _, keep := snapshotFiles[rel]; keep {
			continue
		}
		current := currentFiles[rel]
		path, err := pathguard.ResolveChildNoSymlinks(projectDir, rel)
		if err != nil || path != current.path {
			return fmt.Errorf("managed file changed before delete: %s", rel)
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || !os.SameFile(current.info, info) {
			return fmt.Errorf("managed file changed before delete: %s", rel)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}

	for _, rel := range sortedManagedPaths(snapshotFiles) {
		dest, err := pathguard.ResolveChildNoSymlinks(projectDir, rel)
		if err != nil {
			return err
		}
		if err := copyFile(snapshotFiles[rel].path, dest); err != nil {
			return err
		}
	}
	return nil
}

// List returns the step labels stored under projectDir/.snapshots/.
func List(projectDir string) ([]string, error) {
	dir, err := pathguard.ResolveChildNoSymlinks(projectDir, ".snapshots")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var labels []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".snapshot-") {
			labels = append(labels, strings.ReplaceAll(entry.Name(), "_", " "))
		}
	}
	sort.Strings(labels)
	return labels, nil
}

type managedFile struct {
	path string
	info os.FileInfo
}

func collectManagedFiles(root string, skipSnapshots bool) (map[string]managedFile, error) {
	canonicalRoot, err := pathguard.Resolve(root, root)
	if err != nil {
		return nil, err
	}
	result := make(map[string]managedFile)
	fileCount := 0
	totalBytes := int64(0)
	err = filepath.Walk(canonicalRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(canonicalRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if skipSnapshots && (rel == ".snapshots" || strings.HasPrefix(rel, ".snapshots"+string(os.PathSeparator))) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if ignoredDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if ignoredDirs[info.Name()] {
			return nil
		}
		managed := isManagedSnapshotFile(rel)
		if info.Mode()&os.ModeSymlink != 0 {
			if managed {
				return fmt.Errorf("managed tree entry %q is a symbolic link", rel)
			}
			return nil
		}
		if !managed {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("managed entry %q is not a regular file", rel)
		}
		if info.Size() < 0 || info.Size() > maxSnapshotFileBytes || fileCount >= maxSnapshotFiles || totalBytes+info.Size() > maxSnapshotTotalBytes {
			return errors.New("snapshot exceeds size budget")
		}
		resolved, err := pathguard.Resolve(canonicalRoot, path)
		if err != nil || resolved != path {
			return fmt.Errorf("managed entry %q changed during preflight", rel)
		}
		result[rel] = managedFile{path: path, info: info}
		fileCount++
		totalBytes += info.Size()
		return nil
	})
	return result, err
}

func isManagedSnapshotFile(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	if managedSourceExts[strings.ToLower(filepath.Ext(base))] {
		return true
	}
	if base == "tutorsys.md" || base == "quiz.json" {
		return true
	}
	if strings.HasPrefix(base, "package") && strings.HasSuffix(base, ".json") {
		return true
	}
	if strings.HasPrefix(base, "tsconfig") && strings.HasSuffix(base, ".json") {
		return true
	}
	if strings.HasPrefix(base, "jsconfig") && strings.HasSuffix(base, ".json") {
		return true
	}
	if strings.HasPrefix(base, "jest.config.") || strings.HasPrefix(base, "babel.config.") {
		return true
	}
	switch base {
	case "go.mod", "go.sum", "go.work", "go.work.sum",
		"cargo.toml", "cargo.lock",
		"yarn.lock", "pnpm-lock.yaml", "npm-shrinkwrap.json",
		"pyproject.toml", "pytest.ini", "tox.ini", "setup.cfg", "setup.py",
		"pipfile", "pipfile.lock", "poetry.lock":
		return true
	}
	return strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt")
}

func sortedManagedPaths(files map[string]managedFile) []string {
	paths := make([]string, 0, len(files))
	for rel := range files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	return paths
}

func copyFile(src, dst string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !srcInfo.Mode().IsRegular() || srcInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("snapshot source is not a regular file")
	}
	if srcInfo.Size() > maxSnapshotFileBytes {
		return errors.New("snapshot file exceeds size limit")
	}
	if dstInfo, err := os.Lstat(dst); err == nil {
		if dstInfo.Mode()&os.ModeSymlink != 0 || !dstInfo.Mode().IsRegular() {
			return errors.New("snapshot destination is not a regular file")
		}
		if os.SameFile(srcInfo, dstInfo) {
			return errors.New("snapshot source and destination are the same file")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, srcInfo.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, io.LimitReader(in, maxSnapshotFileBytes+1))
	return err
}
