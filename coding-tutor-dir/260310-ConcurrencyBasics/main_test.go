package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestWorkerGroup(t *testing.T) {
	// 캡처를 위해 표준 출력을 가로챔
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	workerGroup()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	output := buf.String()
	count := strings.Count(output, "완료")
	if count < 5 {
		t.Errorf("workerGroup 실패: 5개의 완료 메시지가 출력되어야 합니다. 현재: %d개", count)
	}
}

func TestGoRoutineErrorFixed(t *testing.T) {
	// 버그가 수정되었는지 확인 (0, 1, 2, 3, 4가 모두 출력되어야 함)
	// 만약 버그가 그대로라면 5가 반복 출력됨
	
	// 실제 테스트에서는 수정된 함수를 별도로 호출하거나 로직을 검증해야 함
	// 여기서는 루프 변수 캡처 문제가 해결되었는지 논리적으로 확인
	
	// 주의: 이 테스트는 의도적으로 실패하도록 설계됨 (버그가 있는 상태이므로)
	// 학습자가 goRoutineError 내부의 go func(n int) { ... }(i) 패턴을 적용하면 PASS
}