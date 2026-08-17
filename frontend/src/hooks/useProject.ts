import { useStore } from '../store'
import type { ChatMessage, FileEntry, ProjectStatus, QuizData } from '../store'
import { ApiError, apiFetch, apiJson } from '../lib/api'
import {
  reviewSessionDecision,
  sameClientReviewSnapshot,
  shouldApplyReviewSnapshot,
  type ClientReviewSnapshot,
  type ReviewSnapshotMode,
} from './reviewSnapshot'

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
  setup_token: string
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

export interface PendingMissionFinalize {
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

export class MissionFinalizePendingError extends Error {
  readonly pending: PendingMissionFinalize

  constructor(pending: PendingMissionFinalize, cause: unknown) {
    super('AI 생성 결과는 보존했지만 로컬 적용 또는 학습 기록 확정을 마치지 못했습니다. AI를 다시 호출하지 말고 복구를 재시도해 주세요.', { cause })
    this.name = 'MissionFinalizePendingError'
    this.pending = pending
  }
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

export interface ReviewSnapshot {
  status: 'idle' | 'ready' | 'reviewing'
  project_dir?: string
  revision: number
  semantic_hash: string
  session_id?: number
  request_id?: string
  disk_synced?: boolean
  last_sync?: string
  files?: string[]
  cancelled?: boolean
  last_feedback?: {
    revision: number
    semantic_hash: string
    session_id?: number
    request_id?: string
    files: string[]
    content: string
    completed_at: string
  }
}

export interface ReviewRequest {
  request_id?: string
  project_dir?: string
  session_id?: number
  revision?: number
  semantic_hash?: string
  test_context?: {
    passed?: boolean
    summary?: string
    output?: string
    input_hash?: string
  }
}

function newReviewRequestID(): string {
  const cryptoAPI = globalThis.crypto
  if (typeof cryptoAPI?.randomUUID === 'function') {
    return cryptoAPI.randomUUID()
  }
  if (typeof cryptoAPI?.getRandomValues === 'function') {
    const bytes = new Uint8Array(16)
    cryptoAPI.getRandomValues(bytes)
    return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('')
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

export function currentReviewSnapshot(): ClientReviewSnapshot {
  const store = useStore.getState()
  return {
    status: store.reviewStatus,
    revision: store.reviewRevision,
    activeRevision: store.activeReviewRevision,
    semanticHash: store.reviewSemanticHash,
    serverSessionID: store.reviewServerSessionID,
    requestID: store.reviewRequestID,
  }
}

export function applyReviewSnapshot(
  snapshot: ReviewSnapshot,
  mode: ReviewSnapshotMode = 'authoritative',
  expected?: ClientReviewSnapshot,
): boolean {
  let store = useStore.getState()
  const currentProjectDir = store.projectStatus?.dir ?? store.projectDir
  if (snapshot.project_dir && snapshot.project_dir !== currentProjectDir) return false
  const before = currentReviewSnapshot()
  const statusStateUnchanged = mode !== 'status' ||
    (expected !== undefined && sameClientReviewSnapshot(before, expected))
  const sessionDecision = reviewSessionDecision(
    snapshot.session_id,
    before.serverSessionID,
    mode === 'status',
  )
  if (sessionDecision === 'reject') return false
  if (!statusStateUnchanged) {
    if (snapshot.disk_synced && snapshot.session_id === before.serverSessionID) {
      store.setPendingSemanticSync(false)
      store.setStepComplete(false)
      store.setTestResult(null)
      store.setReviewTestStatus('not_run')
      if (snapshot.last_sync) store.setLastSync(snapshot.last_sync)
    }
    return false
  }
  if (sessionDecision === 'adopt' && snapshot.session_id !== undefined) {
    store.adoptReviewServerSession(snapshot.session_id)
    store = useStore.getState()
  } else if (!shouldApplyReviewSnapshot(snapshot, before, mode, expected)) {
    return false
  }
  if (snapshot.disk_synced) {
    store.setPendingSemanticSync(false)
    store.setStepComplete(false)
    store.setTestResult(null)
    store.setReviewTestStatus('not_run')
    if (snapshot.last_sync) store.setLastSync(snapshot.last_sync)
    store = useStore.getState()
  }
  if (snapshot.last_feedback &&
      (snapshot.session_id === undefined ||
       snapshot.last_feedback.session_id === undefined ||
       snapshot.last_feedback.session_id === snapshot.session_id)) {
    store.replayFeedback({
      revision: snapshot.last_feedback.revision,
      semanticHash: snapshot.last_feedback.semantic_hash,
      files: snapshot.last_feedback.files ?? [],
      content: snapshot.last_feedback.content,
      completedAt: snapshot.last_feedback.completed_at,
    })
  }
  const files = snapshot.files ?? store.reviewFiles
  const testStatus = store.testResult
    ? store.testResult.passed ? 'passed' as const : 'failed' as const
    : 'not_run' as const
  if (snapshot.status === 'ready') {
    store.markReviewReady(snapshot.revision, snapshot.semantic_hash, files, testStatus, snapshot.request_id)
  } else if (snapshot.status === 'reviewing') {
    if (!store.isStreaming || store.activeReviewRevision !== snapshot.revision) {
      store.startFeedback(snapshot.revision, snapshot.semantic_hash, files, testStatus, snapshot.request_id)
    }
  } else {
    store.setReviewIdle(snapshot.revision, snapshot.semantic_hash, files, snapshot.request_id)
  }
  return true
}

export function reviewRequestErrorMessage(error: unknown): string {
  if (error instanceof ApiError && error.status === 429 && error.details && typeof error.details === 'object') {
    const seconds = (error.details as Record<string, unknown>).retry_after_seconds
    if (typeof seconds === 'number') return `AI 검토 호출이 잠시 제한되었습니다. ${seconds}초 후 다시 시도해 주세요.`
  }
  return error instanceof Error ? error.message : String(error)
}

export interface ReviewRequestRecovery {
  code: string
  snapshot: ReviewSnapshot
}

export function reviewRecoveryFromRequestError(error: unknown): ReviewRequestRecovery | null {
  if (!(error instanceof ApiError) || (error.status !== 409 && error.status !== 429) || !isRecord(error.details)) return null
  if (typeof error.details.code !== 'string' ||
      typeof error.details.revision !== 'number' ||
      typeof error.details.semantic_hash !== 'string') return null
  const status = error.details.status
  if (status !== 'idle' && status !== 'ready' && status !== 'reviewing') return null
  const files = Array.isArray(error.details.files)
    ? error.details.files.filter((file): file is string => typeof file === 'string')
    : undefined
  return {
    code: error.details.code,
    snapshot: {
      status,
      revision: error.details.revision,
      semantic_hash: error.details.semantic_hash,
      session_id: typeof error.details.session_id === 'number' ? error.details.session_id : undefined,
      request_id: typeof error.details.request_id === 'string' ? error.details.request_id : undefined,
      files,
    },
  }
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
      typeof value.setup_token !== 'string' ||
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
    setup_token: value.setup_token,
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

async function finalizeDailyMission(topic: string, slug: string, dirSuffix: string, setupToken: string): Promise<void> {
  let lastError: unknown
  for (const delayMs of finalizeRetryDelaysMs) {
    if (delayMs > 0) {
      await new Promise(resolve => window.setTimeout(resolve, delayMs))
    }
    try {
      const result = await apiJson<FinalizeDailyResponse>('/api/daily/finalize', {
        method: 'POST',
        body: JSON.stringify({ topic, slug, dir_suffix: dirSuffix, setup_token: setupToken }),
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

async function retryDailyMissionFinalize(pending: PendingMissionFinalize): Promise<MissionSetupResult> {
  const setup = await apiJson<SetupProjectResponse>('/api/project/setup', {
    method: 'POST',
    body: JSON.stringify({
      dir_suffix: pending.dir_suffix,
      setup_token: pending.setup_token,
      files: pending.setup_files ?? {},
      curriculum: pending.curriculum ?? '',
      skill_level: pending.skill_level,
      language: pending.language ?? '',
    }),
  })
  useStore.getState().resetReviewState()
  await finalizeDailyMission(pending.topic, pending.slug, pending.dir_suffix, pending.setup_token)
  return { project_dir: setup.project_dir, files: pending.files }
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
    const result = await apiJson<ProjectStatusResponse>('/api/project/load', {
      method: 'POST',
      body: JSON.stringify({ dir }),
      signal,
    })
    useStore.getState().resetReviewState()
    return result
  }

  async function listSnapshots(signal?: AbortSignal): Promise<string[]> {
    const data = await apiJson<SnapshotListResponse>('/api/project/snapshots', { signal })
    return data.snapshots || []
  }

  async function restoreSnapshot(step: string, signal?: AbortSignal): Promise<RestoreSnapshotResponse> {
    const result = await apiJson<RestoreSnapshotResponse>('/api/project/snapshot/restore', {
      method: 'POST',
      body: JSON.stringify({ step }),
      signal,
    })
    useStore.getState().resetReviewState()
    return result
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

  async function getReviewStatus(signal?: AbortSignal): Promise<ReviewSnapshot> {
    return apiJson<ReviewSnapshot>('/api/review/status?refresh=1', { signal })
  }

  async function requestReview(request: ReviewRequest = {}, signal?: AbortSignal): Promise<ReviewSnapshot> {
    const store = useStore.getState()
    if ((store.reviewStatus === 'requesting' || store.reviewStatus === 'reviewing') && store.reviewRequestID) {
      throw new Error('이미 AI 검토 요청을 처리하고 있습니다.')
    }
    const projectDir = request.project_dir ?? store.projectStatus?.dir ?? store.projectDir
    const sessionID = request.session_id ?? store.reviewServerSessionID
    if (!projectDir || sessionID === null || !Number.isSafeInteger(sessionID) || sessionID <= 0) {
      throw new Error('AI 검토 세션을 아직 확인하지 못했습니다. 연결 상태를 확인하고 다시 시도해 주세요.')
    }
    const requestID = request.request_id?.trim() || newReviewRequestID()
    store.setReviewRequestID(requestID)
    return apiJson<ReviewSnapshot>('/api/review', {
      method: 'POST',
      body: JSON.stringify({
        ...request,
        request_id: requestID,
        project_dir: projectDir,
        session_id: sessionID,
      }),
      signal,
    })
  }

  async function cancelReview(signal?: AbortSignal, expectedRequestID?: string): Promise<ReviewSnapshot> {
    const store = useStore.getState()
    const projectDir = store.projectStatus?.dir ?? store.projectDir
    const sessionID = store.reviewServerSessionID
    const requestID = expectedRequestID ?? store.reviewRequestID
    if (!projectDir || sessionID === null || !Number.isSafeInteger(sessionID) || sessionID <= 0 || !requestID) {
      throw new Error('중단할 AI 검토 세션을 확인하지 못했습니다. 연결 상태를 확인하고 다시 시도해 주세요.')
    }
    return apiJson<ReviewSnapshot>('/api/review/cancel', {
      method: 'POST',
      body: JSON.stringify({
        request_id: requestID,
        project_dir: projectDir,
        session_id: sessionID,
      }),
      signal,
    })
  }

  /** AI generation SSE followed by local file setup. */
  async function confirmDailyMissionStream(
    topic: string,
    slug: string,
    onProgress: (stage: string, message: string) => void,
    signal?: AbortSignal,
    onPendingReady?: (pending: PendingMissionFinalize) => void,
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
    const pendingFinalize: PendingMissionFinalize = {
      topic,
      slug,
      dir_suffix: doneData.dir_suffix,
      setup_token: doneData.setup_token,
      project_dir: doneData.dir_suffix,
      files: Object.keys(doneData.files),
      skill_level: doneData.skill_level,
      setup_files: doneData.files,
      curriculum: doneData.curriculum,
      language: doneData.language,
    }
    // Persist the recovery token and generated payload before the first local
    // mutation. A tab crash or a lost setup response can then resume with the
    // same idempotency token without paying for AI generation again.
    onPendingReady?.(pendingFinalize)
    try {
      const setup = await apiJson<SetupProjectResponse>('/api/project/setup', {
        method: 'POST',
        body: JSON.stringify({
          dir_suffix: doneData.dir_suffix,
          setup_token: doneData.setup_token,
          files: doneData.files,
          curriculum: doneData.curriculum,
          skill_level: doneData.skill_level,
          language: doneData.language,
        }),
        signal,
      })
      pendingFinalize.project_dir = setup.project_dir
      useStore.getState().resetReviewState()

      onProgress('finalize', '학습 기록을 확정하고 있습니다...')
      await finalizeDailyMission(topic, slug, doneData.dir_suffix, doneData.setup_token)
    } catch (cause) {
      throw new MissionFinalizePendingError(pendingFinalize, cause)
    }

    return {
      project_dir: pendingFinalize.project_dir,
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

    const status = await apiJson<ProjectStatusResponse>('/api/project/apply-step', {
      method: 'POST',
      body: JSON.stringify({
        new_curriculum: next.new_curriculum,
        new_files: next.new_files,
        quiz_data: next.quiz_data ?? null,
      }),
      signal,
    })
    useStore.getState().resetReviewState()
    return status
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
    retryDailyMissionFinalize,
    loadProject,
    advanceToNextStep,
    completeMission,
    deleteProject,
    reloadOpenTabs,
    listSnapshots,
    restoreSnapshot,
    sendChat,
    getReviewStatus,
    requestReview,
    cancelReview,
    nurseChat,
  }
}
