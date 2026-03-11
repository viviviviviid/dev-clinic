// main.go: 고루틴 기초 학습을 위한 메인 진입점 및 WaitGroup 실습 코드
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	fmt.Println("=== Step 1: Worker Group 실행 ===")
	workerGroup()

	fmt.Println("\n=== Step 1: Go Routine Error 해결 ===")
	goRoutineError()
}

func workerGroup() {
	// [TUTOR:HOLE] 5개의 고루틴을 생성하여 각각 1초간 작업 후 메시지 출력
	//
	// 📌 목표: 메인 프로세스가 5개의 고루틴이 모두 완료될 때까지 안전하게 대기하도록 만듭니다.
	//          WaitGroups를 사용하여 동기화 흐름을 제어하는 방법을 익힙니다.
	//
	// 💡 단계별 접근법:
	//   1. sync.WaitGroup을 선언합니다.
	//   2. for 루프를 5번 반복하며, 각 고루틴 시작 전 wg.Add(1)을 호출합니다.
	//   3. 고루틴 내부에서 작업이 끝난 후 wg.Done()을 호출합니다.
	//   4. 메인 함수 마지막에서 모든 작업이 끝나길 기다리는 wg.Wait()를 호출합니다.
	//
	// 🔧 사용할 것들: sync.WaitGroup, go 키워드, for 루프, time.Sleep(time.Second)
	//    패턴: var wg sync.WaitGroup, wg.Add(1), wg.Done(), wg.Wait()
}

func goRoutineError() {
	// [TUTOR:BUG] goRoutineError
	// 🔍 이 코드는 실행은 되지만 결과가 예상과 다릅니다.
	//    루프 변수 i의 값이 고루틴이 실행되는 시점에 참조되어, 0~4가 아닌 5가 5번 출력되는 문제가 발생합니다.
	//    클로저(Closure)의 변수 캡처 특성 때문에 발생하는 동시성 버그입니다.
	for i := 0; i < 5; i++ {
		go func() {
			fmt.Printf("루프 값: %d\n", i)
		}()
	}
	time.Sleep(time.Second * 1)
}
