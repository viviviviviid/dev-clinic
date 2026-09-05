export interface ClinicFailure {
  message: string
  kind: 'auth' | 'connection' | 'http' | 'parse' | 'unknown'
  status: number | null
  code?: string
}

export function describeClinicFailure(failure: ClinicFailure) {
  const sessionError = failure.status === 401 || (failure.kind === 'auth' && failure.status === null)
  if (sessionError) return {
    sessionError,
    title: '로그인 정보를 다시 확인해 주세요',
    guidance: '로그인 정보 갱신을 눌러 다시 연결하세요. 계속 실패하면 Google 계정으로 다시 로그인해 주세요.',
    retryLabel: '로그인 정보 갱신',
  }
  if (failure.status === 403 && failure.code === 'account_not_allowed') return {
    sessionError,
    title: '이 계정은 clinic 사용이 허용되지 않았습니다',
    guidance: '아래 로그인 계정을 확인하고 허용된 Google 계정으로 전환해 주세요. 허용 목록을 변경했다면 clinic을 다시 실행하세요.',
    retryLabel: '다시 연결',
  }
  return {
    sessionError,
    title: failure.kind === 'connection' ? '로컬 clinic에 연결할 수 없습니다' : 'clinic 요청을 처리하지 못했습니다',
    guidance: failure.kind === 'connection'
      ? '이 화면을 연 Mac에서 clinic을 실행하고, Chrome의 로컬 네트워크 액세스 권한을 확인하세요.'
      : failure.status === 403
        ? 'clinic이 이 접속 주소를 허용하는지 ALLOWED_ORIGINS 설정을 확인하세요.'
        : failure.kind === 'parse'
          ? '화면과 clinic을 같은 버전으로 다시 빌드한 뒤 재시도하세요.'
          : 'clinic 로그를 확인한 뒤 재시도하세요.',
    retryLabel: '다시 연결',
  }
}
