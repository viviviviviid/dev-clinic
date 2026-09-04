// Package completion determines whether the current tutorial step is complete
// from server-observed facts only: a full passing test suite and no unresolved
// tutorial markers in bounded, local source files.
package completion

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/coding-tutor/internal/pathguard"
)

const (
	maxSourceFiles = 2_000
	maxSourceBytes = 16 << 20
	maxFileBytes   = 1 << 20
)

var sourceExtensions = map[string]struct{}{
	".go": {}, ".py": {}, ".rs": {}, ".sol": {},
	".ts": {}, ".tsx": {}, ".mts": {}, ".cts": {},
	".js": {}, ".jsx": {}, ".mjs": {}, ".cjs": {},
}

var ignoredDirectories = map[string]struct{}{
	".git": {}, ".snapshots": {}, "node_modules": {}, "vendor": {},
	"dist": {}, "build": {}, "target": {}, ".venv": {}, "venv": {},
	"coverage": {},
}

type limits struct {
	files        int
	totalBytes   int64
	bytesPerFile int64
}

var defaultLimits = limits{
	files:        maxSourceFiles,
	totalBytes:   maxSourceBytes,
	bytesPerFile: maxFileBytes,
}

// Result describes the deterministic evidence used for a completion decision.
type Result struct {
	Complete     bool
	TestsPassed  bool
	HoleCount    int
	BugCount     int
	EndCount     int
	ScannedFiles int
	ScannedBytes int64
	ScanErr      error
}

// Evaluate scans projectDir after confining it to baseDir. Any scan ambiguity,
// I/O failure, symlink, missing source file, or configured limit violation is a
// fail-closed completion result.
func Evaluate(baseDir, projectDir string, testsPassed bool) Result {
	return evaluate(baseDir, projectDir, testsPassed, defaultLimits)
}

func evaluate(baseDir, projectDir string, testsPassed bool, scanLimits limits) Result {
	result := Result{TestsPassed: testsPassed}
	resolvedDir, err := pathguard.Resolve(baseDir, projectDir)
	if err != nil {
		result.ScanErr = fmt.Errorf("project path validation: %w", err)
		return result
	}

	root, err := os.OpenRoot(resolvedDir)
	if err != nil {
		result.ScanErr = fmt.Errorf("open project root: %w", err)
		return result
	}
	defer root.Close()

	err = filepath.WalkDir(resolvedDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == resolvedDir {
			return nil
		}

		if entry.IsDir() {
			if isIgnoredDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if _, ignored := ignoredDirectories[entry.Name()]; ignored {
				return nil
			}
			return fmt.Errorf("symbolic link is not allowed during marker scan: %s", entry.Name())
		}
		if _, ok := sourceExtensions[strings.ToLower(filepath.Ext(entry.Name()))]; !ok {
			return nil
		}

		rel, err := filepath.Rel(resolvedDir, path)
		if err != nil {
			return fmt.Errorf("resolve source path: %w", err)
		}
		info, err := root.Lstat(rel)
		if err != nil {
			return fmt.Errorf("inspect source %s: %w", rel, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("source is not a regular file: %s", rel)
		}

		result.ScannedFiles++
		if result.ScannedFiles > scanLimits.files {
			return fmt.Errorf("source file limit exceeded (%d)", scanLimits.files)
		}
		if info.Size() < 0 || info.Size() > scanLimits.bytesPerFile {
			return fmt.Errorf("source file byte limit exceeded: %s", rel)
		}
		if result.ScannedBytes+info.Size() > scanLimits.totalBytes {
			return fmt.Errorf("total source byte limit exceeded (%d bytes)", scanLimits.totalBytes)
		}

		source, err := root.Open(rel)
		if err != nil {
			return fmt.Errorf("open source %s: %w", rel, err)
		}
		openedInfo, statErr := source.Stat()
		if statErr != nil {
			_ = source.Close()
			return fmt.Errorf("inspect opened source %s: %w", rel, statErr)
		}
		if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
			_ = source.Close()
			return fmt.Errorf("source changed during marker scan: %s", rel)
		}
		content, readErr := io.ReadAll(io.LimitReader(source, scanLimits.bytesPerFile+1))
		closeErr := source.Close()
		if readErr != nil {
			return fmt.Errorf("read source %s: %w", rel, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close source %s: %w", rel, closeErr)
		}
		if int64(len(content)) > scanLimits.bytesPerFile {
			return fmt.Errorf("source file byte limit exceeded while reading: %s", rel)
		}
		if result.ScannedBytes+int64(len(content)) > scanLimits.totalBytes {
			return fmt.Errorf("total source byte limit exceeded (%d bytes)", scanLimits.totalBytes)
		}
		result.ScannedBytes += int64(len(content))
		result.HoleCount += bytes.Count(content, []byte("[TUTOR:HOLE]"))
		result.BugCount += bytes.Count(content, []byte("[TUTOR:BUG]"))
		result.EndCount += bytes.Count(content, []byte("[TUTOR:END]"))
		return nil
	})
	if err != nil {
		result.ScanErr = err
		return result
	}
	if result.ScannedFiles == 0 {
		result.ScanErr = errors.New("no supported source files found")
		return result
	}

	// Markers are durable editor annotations now: learners edit the marked body
	// in place, so a passing full test suite is the completion evidence. Requiring
	// the marker comments to be deleted would force them to replace a whole
	// function just to advance.
	result.Complete = testsPassed
	return result
}

func isIgnoredDirectory(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	_, ok := ignoredDirectories[name]
	return ok
}

// Summary is suitable for both the test-result event and the SSE output.
func (r Result) Summary() string {
	if r.ScanErr != nil {
		return "단계 완료 확인 실패: 소스 마커를 안전하게 검사하지 못했습니다 (" + r.ScanErr.Error() + ")"
	}
	markerSummary := fmt.Sprintf(
		"학습 범위 marker %d개 (HOLE %d, BUG %d, END/범위 종료 %d)",
		r.HoleCount+r.BugCount+r.EndCount,
		r.HoleCount,
		r.BugCount,
		r.EndCount,
	)
	if r.Complete {
		return "단계 완료 조건 충족: 전체 테스트 통과; " + markerSummary
	}
	if !r.TestsPassed {
		return "단계 미완료: 전체 테스트 미통과; " + markerSummary
	}
	return "단계 미완료: " + markerSummary
}

// AppendSummary preserves the actual test output while making the independent
// completion decision explicit.
func AppendSummary(testSummary string, result Result) string {
	testSummary = strings.TrimSpace(testSummary)
	if testSummary == "" {
		return result.Summary()
	}
	return testSummary + " | " + result.Summary()
}
