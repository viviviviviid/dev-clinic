export interface QuizSubmissionResult {
  applied: boolean
  error: string
}

export async function applyQuizCandidate(
  code: string,
  apply: () => Promise<boolean>,
): Promise<QuizSubmissionResult> {
  if (!code.trim()) {
    return { applied: false, error: '코드를 입력해 주세요.' }
  }

  try {
    if (await apply()) return { applied: true, error: '' }
    return {
      applied: false,
      error: '코드를 적용하지 못했습니다. 입력을 확인하고 다시 시도하세요.',
    }
  } catch {
    return {
      applied: false,
      error: '코드 적용 중 오류가 발생했습니다. 입력은 그대로 보존됩니다.',
    }
  }
}

export async function persistQuizCandidate(
  save: () => Promise<boolean>,
  commit: () => void,
  isSourceUnchanged: () => boolean = () => true,
): Promise<boolean> {
  if (!await save()) return false
  // Saving can yield while Monaco accepts another edit. Never replace that
  // newer editor value with a candidate derived from an older snapshot.
  if (!isSourceUnchanged()) return false
  commit()
  return true
}
