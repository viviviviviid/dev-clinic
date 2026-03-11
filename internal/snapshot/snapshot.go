package snapshot

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var watchedExts = map[string]bool{
	".go":  true,
	".ts":  true,
	".tsx": true,
	".js":  true,
	".jsx": true,
	".rs":  true,
	".sol": true,
	".py":  true,
}

func snapshotDir(projectDir, stepLabel string) string {
	safe := strings.ReplaceAll(stepLabel, "/", "-")
	safe = strings.ReplaceAll(safe, " ", "_")
	return filepath.Join(projectDir, ".snapshots", safe)
}

// Save copies source files + TUTORSYS.md + quiz.json into
// projectDir/.snapshots/{stepLabel}/
func Save(projectDir, stepLabel string) error {
	dst := snapshotDir(projectDir, stepLabel)
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	return filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// スキップ: .snapshots 自身
		rel, _ := filepath.Rel(projectDir, path)
		if rel == ".snapshots" || strings.HasPrefix(rel, ".snapshots"+string(os.PathSeparator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}

		name := info.Name()
		ext := filepath.Ext(name)
		if !watchedExts[ext] && name != "TUTORSYS.md" && name != "quiz.json" {
			return nil
		}

		return copyFile(path, filepath.Join(dst, rel))
	})
}

// Restore copies files from projectDir/.snapshots/{stepLabel}/ back to projectDir.
func Restore(projectDir, stepLabel string) error {
	src := snapshotDir(projectDir, stepLabel)
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(projectDir, rel), 0755)
		}
		return copyFile(path, filepath.Join(projectDir, rel))
	})
}

// List returns the step labels stored under projectDir/.snapshots/.
func List(projectDir string) ([]string, error) {
	dir := filepath.Join(projectDir, ".snapshots")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var labels []string
	for _, e := range entries {
		if e.IsDir() {
			labels = append(labels, strings.ReplaceAll(e.Name(), "_", " "))
		}
	}
	sort.Strings(labels)
	return labels, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
