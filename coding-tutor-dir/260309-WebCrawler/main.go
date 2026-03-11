// main.go: 고루틴과 워커 풀 패턴을 사용한 동시성 웹 크롤러입니다.
package main

import (
	"fmt"
	"net/http"
	"sync"

	"golang.org/x/net/html"
)

// Semaphore: 동시 실행 고루틴 개수를 제한하기 위한 채널
var sem = make(chan struct{}, 5)

func main() {
	urls := []string{"http://example.com", "http://google.com", "http://golang.org"}
	results := make(chan string)
	var wg sync.WaitGroup

	for _, url := range urls {
		wg.Add(1)

		// [TUTOR:BUG] main - 동시성 제어 로직 미흡
		// 🔍 이 코드는 실행은 되지만 너무 많은 요청을 동시에 보냅니다.
		//    시스템 자원 부족으로 연결이 거부되거나 서버로부터 429 에러를 받을 수 있습니다.
		//    현재는 워커 풀 제한(sem)을 전혀 사용하지 않고 있습니다.
		go crawl(url, &wg, results)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for link := range results {
		fmt.Println("Found link:", link)
	}
}

func crawl(url string, wg *sync.WaitGroup, results chan<- string) {
	defer wg.Done()
	resp, err := http.Get(url)
	if err == nil {
		defer resp.Body.Close()
		links := extractLinks(resp.Body)
		for _, link := range links {
			results <- link
		}
	}
	//
	// 📌 목표: 주어진 URL에서 HTML을 읽어오고 링크를 찾아 results 채널에 전달합니다.
	//          작업이 끝나면 반드시 WaitGroup의 Done을 호출해야 합니다.
	//
	// 💡 단계별 접근법:
	//   1. http.Get으로 URL에 요청을 보냅니다. (에러 체크 필수)
	//   2. defer를 사용하여 resp.Body를 닫고 wg.Done()을 호출합니다.
	//   3. html.Parse로 응답 본문을 파싱합니다.
	//   4. 재귀적으로 노드를 탐색하여 <a> 태그의 href 속성을 추출합니다.
	//   5. 추출된 링크를 results 채널에 보냅니다.
	//
	// 🔧 사용할 것들: http.Get, html.Parse, wg.Done(), channel 연산
	//    패턴:
	//    defer wg.Done()
	//    resp, err := http.Get(url)
	//    doc, _ := html.Parse(resp.Body)
	//    var visit func(*html.Node)
	//    visit = func(n *html.Node) { ... }

	return
}
