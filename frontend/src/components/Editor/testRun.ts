export interface TestRunRequest {
  endpoint: string
  title: string
}

export function createTestRunRequest(testName?: string): TestRunRequest {
  if (!testName) {
    return { endpoint: '/api/test', title: '전체 테스트 결과' }
  }
  return {
    endpoint: `/api/test?func=${encodeURIComponent(testName)}`,
    title: `테스트: ${testName}`,
  }
}
