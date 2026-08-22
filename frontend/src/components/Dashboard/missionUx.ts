export const MIN_EDITOR_WIDTH = 680

export function dailyIntroStorageKey(userID: string): string {
  return `coding-tutor.daily-intro.${userID}`
}

export interface PendingMissionFinalizeRecord {
  topic: string
  slug: string
  dir_suffix: string
  setup_token: string
  project_dir: string
  files: string[]
  skill_level: string
  setup_files?: Record<string, string>
  curriculum?: string
  language?: string
}

function isPendingMissionFinalizeRecord(value: unknown): value is PendingMissionFinalizeRecord {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const record = value as Record<string, unknown>
  return typeof record.topic === 'string' &&
    typeof record.slug === 'string' &&
    typeof record.dir_suffix === 'string' &&
    typeof record.setup_token === 'string' && /^[a-f0-9]{32}$/.test(record.setup_token) &&
    typeof record.project_dir === 'string' &&
    typeof record.skill_level === 'string' &&
    Array.isArray(record.files) && record.files.every((file) => typeof file === 'string') &&
    (record.setup_files === undefined || (
      typeof record.setup_files === 'object' && record.setup_files !== null && !Array.isArray(record.setup_files) &&
      Object.values(record.setup_files).every((content) => typeof content === 'string')
    )) &&
    (record.curriculum === undefined || typeof record.curriculum === 'string') &&
    (record.language === undefined || typeof record.language === 'string')
}

export function readPendingMissionFinalize(
  storage: Pick<Storage, 'getItem'>,
  key: string,
): PendingMissionFinalizeRecord | null {
  try {
    const raw = storage.getItem(key)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    return isPendingMissionFinalizeRecord(parsed) ? parsed : null
  } catch {
    return null
  }
}

export function writePendingMissionFinalize(
  storage: Pick<Storage, 'setItem'>,
  key: string,
  pending: PendingMissionFinalizeRecord,
): boolean {
  try {
    storage.setItem(key, JSON.stringify(pending))
    return true
  } catch {
    return false
  }
}

export function clearPendingMissionFinalize(
  storage: Pick<Storage, 'removeItem'>,
  key: string,
): boolean {
  try {
    storage.removeItem(key)
    return true
  } catch {
    return false
  }
}

export interface MissionGenerationStatus {
  icon: string
  title: string
  detail: string
  cancellable: boolean
}

const AI_STAGE_NUMBER: Record<string, number> = {
  curriculum: 1,
  code: 2,
  quiz: 3,
}

export function describeMissionGeneration(
  stage: string,
  detail: string,
  skillLevel: string,
): MissionGenerationStatus {
  const aiStage = AI_STAGE_NUMBER[stage]
  const totalAiStages = Math.max(skillLevel === 'newbie' ? 3 : 2, aiStage ?? 0)

  if (aiStage) {
    return {
      icon: stage === 'curriculum' ? '📚' : stage === 'code' ? '💻' : '📝',
      title: `AI가 ${aiStage}/${totalAiStages}단계를 생성 중입니다`,
      detail,
      cancellable: true,
    }
  }

  if (stage === 'finalize') {
    return {
      icon: '💾',
      title: '학습 기록을 확정하고 있습니다',
      detail,
      cancellable: false,
    }
  }

  if (stage === 'watcher') {
    return {
      icon: '📡',
      title: '로컬 파일을 안전하게 적용 중입니다',
      detail: `${detail} 적용이 완료되면 코딩 작업실로 이동합니다.`,
      cancellable: false,
    }
  }

  return {
    icon: '⚙️',
    title: '미션 생성을 준비하고 있습니다',
    detail,
    cancellable: true,
  }
}

export function isEditorWidthReady(viewportWidth: number): boolean {
  return viewportWidth >= MIN_EDITOR_WIDTH
}
