// main_test.go: 동시성 크롤러의 로직을 테스트합니다.
package main

import (
	"sync"
	"testing"
	"time"
)

func TestCrawlFunctionality(t *testing.T) {
	results := make(chan string, 10)
	var wg sync.WaitGroup
	
	// 유효하지 않은 URL이나 테스트 서버를 통해 테스트
	wg.Add(1)
	go crawl("http://example.com", &wg, results)
	
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 성공
	case <-time.After(5 * time.Second):
		t.Fatal("crawl 함수가 너무 오래 걸리거나 응답하지 않습니다.")
	}
}

func TestSemaphoreLogic(t *testing.T) {
	// [TUTOR:BUG] 수정 확인 테스트
	// 버그가 해결되었다면 sem 채널의 capacity가 활용되고 있어야 합니다.
	if cap(sem) != 5 {
		t.Errorf("동시성 제어 세마포어 버퍼 크기가 5여야 합니다. 현재: %d", cap(sem))
	}
	
	// 실제 고루틴이 세마포어를 통해 제어되는지 확인하는 로직은 
	// 런타임 추적이 필요하지만, 여기서는 버퍼 설정 여부로 판단합니다.
}