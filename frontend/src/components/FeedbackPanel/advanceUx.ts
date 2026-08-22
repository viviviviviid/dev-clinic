interface AdvanceButtonState {
  pendingSemanticSync: boolean
  advancing: boolean
  recovering: boolean
  finalStep: boolean
}

export function advanceButtonLabel(state: AdvanceButtonState): string {
  if (state.pendingSemanticSync) return '변경 확인 중…'
  if (state.advancing) return state.finalStep ? '완료 처리 중…' : '다음 단계 생성 중…'
  if (state.recovering) return '단계 상태 다시 불러오기'
  return state.finalStep ? '미션 완료하기 →' : '다음 단계로 →'
}
