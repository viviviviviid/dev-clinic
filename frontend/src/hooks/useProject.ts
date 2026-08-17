import { useStore } from '../store'
import type { ChatMessage, FileEntry, ProjectStatus, QuizData } from '../store'
import { ApiError, apiFetch, apiJson } from '../lib/api'

export interface TopicSuggestion {
  name: string
  slug: string
  difficulty: '상' | '중' | '하'
}

interface DailyMission {
  id: string
  date: string
  topic: string
  slug: string
  project_dir: string
  status: string
}

interface DailyMissionResponse {
  missions?: DailyMission[]
  topics?: TopicSuggestion[]
  error?: string
}

type RecoverablePromise<T> = Omit<Promise<T>, 'catch'> & {
  catch(onRejected: (reason: unknown) => Partial<T> | PromiseLike<Partial<T>>): Promise<T>
}

interface FileReadResponse {
  content?: string
}

interface SnapshotListResponse {
  snapshots?: string[]
}

interface RestoreSnapshotResponse {
  ok: boolean
  step: string
}

type ProjectStatusResponse = ProjectStatus & {
  done?: false
  error?: string
}

interface ProjectCompleteResponse {
  ok: boolean
}

interface ConfirmDailyDone {
  dir_suffix: string
  files: Record<string, string>
  curriculum: string
  skill_level: string
  language: string
}

interface MissionSetupResult {
  project_dir: string
  files: string[]
  error?: string
}

interface SetupProjectResponse {
  project_dir: string
}

interface FinalizeDailyResponse {
  ok: boolean
  created: boolean
}

interface ReadAllResponse {
  files: Record<string, string>
  curriculum: string
}

interface NextStepDoneResponse {
  done: true
  message: string
  loaded?: false
}

interface NextStepGeneratedResponse {
  done?: false
  new_curriculum: string
  new_files: Record<string, string>
  quiz_data?: QuizData | null
}

type AdvanceToNextStepResult = NextStepDoneResponse | ProjectStatusResponse

interface SseEvent {
  event: string
  data: unknown
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseSseEvent(block: string): SseEvent | null {
  let event = 'message'
  const dataLines: string[] = []

  for (const line of block.split('\n')) {
    if (line.startsWith('event:')) event = line.slice(6).trim()
    if (line.startsWith('data:')) dataLines.push(line.slice(5).trimStart())
  }
  if (dataLines.length === 0) return null

  const rawData = dataLines.join('\n')
  try {
    return { event, data: JSON.parse(rawData) as unknown }
  } catch (cause) {
    throw new ApiError('clinic의 SSE 응답을 해석할 수 없습니다.', { kind: 'parse', details: rawData, cause })
  }
}

function parseConfirmDone(value: unknown): ConfirmDailyDone {
  if (!isRecord(value) ||
      typeof value.dir_suffix !== 'string' ||
      typeof value.curriculum !== 'string' ||
      typeof value.skill_level !== 'string' ||
      typeof value.language !== 'string' ||
      !isRecord(value.files)) {
    throw new ApiError('프로젝트 생성 완료 응답 형식이 올바르지 않습니다.', { kind: 'parse', details: value })
  }

  const files: Record<string, string> = {}
  for (const [name, content] of Object.entries(value.files)) {
    if (typeof content !== 'string') {
      throw new ApiError(`생성된 파일 ${name}의 내용이 문자열이 아닙니다.`, { kind: 'parse', details: value })
    }
    files[name] = content
  }

  return {
    dir_suffix: value.dir_suffix,
    files,
    curriculum: value.curriculum,
    skill_level: value.skill_level,
    language: value.language,
  }
}

function requireStream(response: Response): ReadableStream<Uint8Array> {
  if (!response.body) {
    throw new ApiError('clinic이 스트림 응답을 반환하지 않았습니다.', {
      kind: 'parse',
      status: response.status,
    })
  }
  return response.body
}

const finalizeRetryDelaysMs = [0, 250, 750] as const

function shouldRetryFinalize(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.status === null) return true
  return error.status === 408 || error.status === 429 || error.status >= 500
}

async function finalizeDailyMission(topic: string, slug: string, dirSuffix: string): Promise<void> {
  let lastError: unknown
  for (const delayMs of finalizeRetryDelaysMs) {
    if (delayMs > 0) {
      await new Promise(resolve => window.setTimeout(resolve, delayMs))
    }
    try {
      const result = await apiJson<FinalizeDailyResponse>('/api/daily/finalize', {
        method: 'POST',
        body: JSON.stringify({ topic, slug, dir_suffix: dirSuffix }),
        keepalive: true,
      })
      if (!result.ok) throw new ApiError('학습 기록 확정 응답이 올바르지 않습니다.', { kind: 'parse', details: result })
      return
    } catch (error) {
      lastError = error
      if (!shouldRetryFinalize(error)) throw error
    }
  }

  const prior = lastError instanceof ApiError ? lastError : null
  throw new ApiError(
    '프로젝트 파일은 생성됐지만 학습 기록 확정 여부를 확인하지 못했습니다. Supabase 연결을 확인한 뒤 대시보드를 새로고침해 주세요.',
    {
      kind: prior?.kind ?? 'connection',
      status: prior?.status ?? undefined,
      details: prior?.details,
      cause: lastError,
    },
  )
}

export function useProject() {
  const { projectStatus, setProjectStatus, setFileTree } = useStore()

  async function refreshStatus(signal?: AbortSignal): Promise<ProjectStatusResponse> {
    const data = await apiJson<ProjectStatusResponse>('/api/project/status', { signal })
    setProjectStatus(data)
    return data
  }

  async function refreshFileTree(dir: string, signal?: AbortSignal): Promise<void> {
    const data = await apiJson<FileEntry[]>(`/api/fs/list?path=${encodeURIComponent(dir)}`, { signal })
    setFileTree(data || [])
  }

  async function readFile(path: string, signal?: AbortSignal): Promise<string> {
    const data = await apiJson<FileReadResponse>(`/api/fs/read?path=${encodeURIComponent(path)}`, { signal })
    return data.content || ''
  }

  async function writeFile(path: string, content: string): Promise<void> {
    await apiFetch('/api/fs/write', {
      method: 'POST',
      body: JSON.stringify({ path, content }),
    })
  }

  async function loadQuizData(signal?: AbortSignal): Promise<QuizData> {
    return apiJson<QuizData>('/api/quiz', { signal })
  }

  function getDailyMission(): RecoverablePromise<DailyMissionResponse> {
    return apiJson<DailyMissionResponse>('/api/daily') as RecoverablePromise<DailyMissionResponse>
  }

  async function getDailyHistory(): Promise<DailyMission[]> {
    return apiJson<DailyMission[]>('/api/daily/history')
  }

  /** Compatibility wrapper for the unused legacy DailyMission screen. */
  async function confirmDailyMission(topic: string, slug: string): Promise<MissionSetupResult> {
    return confirmDailyMissionStream(topic, slug, () => undefined)
  }

  async function loadProject(dir: string, signal?: AbortSignal): Promise<ProjectStatusResponse> {
    return apiJson<ProjectStatusResponse>('/api/project/load', {
      method: 'POST',
      body: JSON.stringify({ dir }),
      signal,
    })
  }

  async function listSnapshots(signal?: AbortSignal): Promise<string[]> {
    const data = await apiJson<SnapshotListResponse>('/api/project/snapshots', { signal })
    return data.snapshots || []
  }

  async function restoreSnapshot(step: string, signal?: AbortSignal): Promise<RestoreSnapshotResponse> {
    return apiJson<RestoreSnapshotResponse>('/api/project/snapshot/restore', {
      method: 'POST',
      body: JSON.stringify({ step }),
      signal,
    })
  }

  async function completeMission(): Promise<ProjectCompleteResponse> {
    const status = useStore.getState().projectStatus
    const projectDir = status?.dir || ''
    const [result] = await Promise.all([
      apiJson<ProjectCompleteResponse>('/api/project/complete', {
        method: 'POST',
        body: JSON.stringify({ project_dir: projectDir }),
      }),
      apiFetch('/api/project/stop-watcher', { method: 'POST', body: '{}' }),
    ])
    return result
  }

  async function deleteProject(projectDir: string): Promise<ProjectCompleteResponse> {
    await apiFetch('/api/project/files', {
      method: 'DELETE',
      body: JSON.stringify({ project_dir: projectDir }),
    })
    return apiJson<ProjectCompleteResponse>('/api/project', {
      method: 'DELETE',
      body: JSON.stringify({ project_dir: projectDir }),
    })
  }

  async function reloadOpenTabs(signal?: AbortSignal): Promise<void> {
    const { openTabs } = useStore.getState()
    await Promise.all(openTabs.map(async (tab) => {
      try {
        const content = await readFile(tab.path, signal)
        useStore.getState().updateTabContent(tab.path, content)
      } catch (error: unknown) {
        if (error instanceof ApiError && error.status === 404) {
          useStore.getState().closeTab(tab.path)
          return
        }
        throw error
      }
    }))
  }

  async function sendChat(
    message: string,
    fileContent: string,
    chatHistory: ChatMessage[],
    signal?: AbortSignal,
  ): Promise<ReadableStream<Uint8Array>> {
    const response = await apiFetch('/api/chat', {
      method: 'POST',
      body: JSON.stringify({ message, fileContent, chatHistory }),
      signal,
    })
    return requireStream(response)
  }

  /** AI generation SSE followed by local file setup. */
  async function confirmDailyMissionStream(
    topic: string,
    slug: string,
    onProgress: (stage: string, message: string) => void,
    signal?: AbortSignal,
  ): Promise<MissionSetupResult> {
    const response = await apiFetch('/api/daily/confirm-stream', {
      method: 'POST',
      body: JSON.stringify({ topic, slug }),
      signal,
    })
    const reader = requireStream(response).getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let doneData: ConfirmDailyDone | null = null

    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) {
          buffer += decoder.decode()
          break
        }
        buffer += decoder.decode(value, { stream: true })
        const blocks = buffer.split('\n\n')
        buffer = blocks.pop() ?? ''

        for (const block of blocks) {
          const parsed = parseSseEvent(block)
          if (!parsed) continue
          if (parsed.event === 'progress' && isRecord(parsed.data)) {
            if (typeof parsed.data.stage === 'string' && typeof parsed.data.message === 'string') {
              onProgress(parsed.data.stage, parsed.data.message)
            }
          } else if (parsed.event === 'done') {
            doneData = parseConfirmDone(parsed.data)
          } else if (parsed.event === 'error') {
            const message = isRecord(parsed.data) && typeof parsed.data.error === 'string'
              ? parsed.data.error
              : '프로젝트 생성 중 오류가 발생했습니다.'
            throw new ApiError(message, { kind: 'http', status: response.status, details: parsed.data })
          }
        }
      }
    } finally {
      reader.releaseLock()
    }

    if (!doneData) {
      throw new ApiError('프로젝트 생성 완료 이벤트를 받지 못했습니다.', { kind: 'parse' })
    }

    onProgress('watcher', '파일 감시자를 시작하고 있습니다...')
    const setup = await apiJson<SetupProjectResponse>('/api/project/setup', {
      method: 'POST',
      body: JSON.stringify({
        dir_suffix: doneData.dir_suffix,
        files: doneData.files,
        curriculum: doneData.curriculum,
        skill_level: doneData.skill_level,
        language: doneData.language,
      }),
      signal,
    })

    onProgress('finalize', '학습 기록을 확정하고 있습니다...')
    await finalizeDailyMission(topic, slug, doneData.dir_suffix)

    return {
      project_dir: setup.project_dir,
      files: Object.keys(doneData.files),
    }
  }

  async function advanceToNextStep(signal?: AbortSignal): Promise<AdvanceToNextStepResult> {
    const current = await apiJson<ReadAllResponse>('/api/project/read-all', { signal })
    const skillLevel = useStore.getState().skillLevel || 'normal'

    const next = await apiJson<NextStepDoneResponse | NextStepGeneratedResponse>('/api/project/nextstep', {
      method: 'POST',
      body: JSON.stringify({
        curriculum: current.curriculum,
        current_files: current.files,
        skill_level: skillLevel,
      }),
      signal,
    })

    if (next.done) return next

    return apiJson<ProjectStatusResponse>('/api/project/apply-step', {
      method: 'POST',
      body: JSON.stringify({
        new_curriculum: next.new_curriculum,
        new_files: next.new_files,
        quiz_data: next.quiz_data ?? null,
      }),
      signal,
    })
  }

  async function nurseChat(
    message: string,
    history: { role: string; content: string }[],
    pastTopics: string[],
    signal?: AbortSignal,
  ): Promise<ReadableStream<Uint8Array>> {
    const response = await apiFetch('/api/daily/nurse-chat', {
      method: 'POST',
      body: JSON.stringify({ message, history, pastTopics }),
      signal,
    })
    return requireStream(response)
  }

  return {
    projectStatus,
    refreshStatus,
    refreshFileTree,
    readFile,
    writeFile,
    loadQuizData,
    getDailyMission,
    getDailyHistory,
    confirmDailyMission,
    confirmDailyMissionStream,
    loadProject,
    advanceToNextStep,
    completeMission,
    deleteProject,
    reloadOpenTabs,
    listSnapshots,
    restoreSnapshot,
    sendChat,
    nurseChat,
  }
}
