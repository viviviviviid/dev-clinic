package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadedStatusIncludesCurrentStepIntroduction(t *testing.T) {
	const overview = "앞선 단계에서 데이터를 기록했습니다.\n\n이번에는 기록을 분석해 결과를 확인합니다."
	for _, current := range []string{"Step 2", "Step 2: 결과 확인"} {
		t.Run(current, func(t *testing.T) {
			dir := t.TempDir()
			content := "# TUTORSYS\n\n## 학습자 목표\n전체 목표\n\n## 커리큘럼 단계\n- [x] Step 1: 기록\n- [ ] Step 2: 결과 확인\n\n## 현재 단계\n" + current + "\n\n## 이 단계에서 추가하는 것\n" + overview + "\n\n## 현재 과제\n결과를 검증하세요.\n\n## 파일 구성\nmain.go"
			if err := os.WriteFile(filepath.Join(dir, "TUTORSYS.md"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			var manager Manager
			if err := manager.Load(dir); err != nil {
				t.Fatal(err)
			}
			status := manager.GetStatus()
			if status.StepTitle != "결과 확인" || status.StepOverview != overview {
				t.Fatalf("step introduction = %q, %q", status.StepTitle, status.StepOverview)
			}
			if status.Goal != "전체 목표" || status.Tasks != "결과를 검증하세요." || status.CurrentStep != current {
				t.Fatalf("step introduction changed other lesson fields: %#v", status)
			}
			encoded, err := json.Marshal(status)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			if fields["stepTitle"] != "결과 확인" || fields["stepOverview"] != overview {
				t.Fatalf("status API omitted the introduction: %s", encoded)
			}
			// Updating the lesson replaces the overview rather than retaining the
			// previous step's introduction; legacy documents need no migration.
			manager.Set(dir, strings.Replace(content, overview, "- main.go: 결과 출력 추가", 1))
			if got := manager.GetStatus().StepOverview; got != "- main.go: 결과 출력 추가" {
				t.Fatalf("legacy overview = %q", got)
			}
		})
	}
}

func TestStatusWithoutIntroductionKeepsCurrentStep(t *testing.T) {
	var manager Manager
	if status := manager.GetStatus(); status.Loaded || status.StepTitle != "" || status.StepOverview != "" {
		t.Fatalf("unloaded status: %#v", status)
	}
	manager.Set(t.TempDir(), "# TUTORSYS\n\n## 현재 단계\nStep 1\n\n## 현재 과제\n과제")
	status := manager.GetStatus()
	if status.StepTitle != "Step 1" || status.StepOverview != "" {
		t.Fatalf("legacy status: %#v", status)
	}
}
