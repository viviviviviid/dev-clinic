package diff

import (
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func Unified(oldContent, newContent, filename string) string {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(oldContent, newContent, false)
	dmp.DiffCleanupSemantic(diffs)

	if len(diffs) == 0 {
		return ""
	}

	// Check if there are any actual changes
	hasChange := false
	for _, d := range diffs {
		if d.Type != diffmatchpatch.DiffEqual {
			hasChange = true
			break
		}
	}
	if !hasChange {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- %s (이전)\n", filename))
	sb.WriteString(fmt.Sprintf("+++ %s (현재)\n", filename))

	for _, d := range diffs {
		lines := strings.Split(d.Text, "\n")
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			for _, l := range lines {
				if l != "" {
					sb.WriteString("+ " + l + "\n")
				}
			}
		case diffmatchpatch.DiffDelete:
			for _, l := range lines {
				if l != "" {
					sb.WriteString("- " + l + "\n")
				}
			}
		case diffmatchpatch.DiffEqual:
			// Show limited context
			for _, l := range lines {
				if l != "" {
					sb.WriteString("  " + l + "\n")
				}
			}
		}
	}

	return sb.String()
}

func HasChanges(oldContent, newContent string) bool {
	return oldContent != newContent
}
