import { create } from 'zustand'
import type { User } from '@supabase/supabase-js'

export interface FileEntry {
  name: string
  path: string
  isDir: boolean
  children?: FileEntry[]
}

export interface OpenTab {
  path: string
  content: string
}

export interface FeedbackMessage {
  id: string
  content: string
  timestamp: string
  isStreaming?: boolean
  revision?: number
  semanticHash?: string
  files?: string[]
  testStatus?: ReviewTestStatus
}

export type ReviewStatus = 'idle' | 'ready' | 'requesting' | 'reviewing' | 'canceled' | 'error'
export type ReviewTestStatus = 'not_run' | 'passed' | 'failed'
export type TestScope = 'full' | 'targeted'

export interface TestResult {
  passed: boolean
  summary: string
  scope?: TestScope
  inputHash?: string
}

export interface ReviewFeedbackReplay {
  revision: number
  semanticHash: string
  files: string[]
  content: string
  completedAt: string
}

export function canUseReviewControl(status: ReviewStatus, hasTestContext = false): boolean {
  return hasTestContext || status === 'ready' || status === 'error' || status === 'requesting' || status === 'reviewing'
}

export interface ProjectStatus {
  loaded: boolean
  dir: string
  language: string
  currentStep: string
  goal: string
  concept: string
  tasks: string
  content: string
  skillLevel: string
  totalSteps: number
  currentStepNum: number
}

export type SkillLevel = 'newbie' | 'normal' | 'experienced'

export interface ChatMessage {
  role: 'user' | 'ai'
  content: string
}

export interface QuizItem {
  key: string
  filename: string
  markerType: string   // "hole" | "bug"
  markerIndex: number
  question: string
  hints: string[]      // 3단계: 개념 → 구조 → 구체 힌트
}

export type QuizData = Record<string, QuizItem>

export interface UserSettings {
  user_id: string
  base_dir?: string
  language: string
  skill_level: string
  language_supported?: boolean
}

export interface DiagnosticItem {
  filePath: string
  fileName: string
  message: string
  severity: 1 | 2 | 3 | 4
  startLine: number
  startColumn: number
}

export interface AppState {
  // Auth
  user: User | null
  setUser: (user: User | null) => void

  // User settings
  userSettings: UserSettings | null
  setUserSettings: (s: UserSettings | null) => void

  // Project
  projectDir: string
  projectStatus: ProjectStatus | null
  setProjectDir: (dir: string) => void
  setProjectStatus: (status: ProjectStatus) => void
  clearProjectStatus: () => void

  // Files
  fileTree: FileEntry[]
  openFile: string | null
  openFileContent: string
  openFileReadOnly: boolean
  changedFiles: Set<string>
  openTabs: OpenTab[]
  pendingSemanticSync: boolean
  workspaceMutationLocked: boolean
  projectSessionEpoch: number
  setFileTree: (tree: FileEntry[]) => void
  setOpenFile: (path: string | null) => void
  setOpenFileContent: (content: string) => void
  setOpenFileReadOnly: (v: boolean) => void
  markFileChanged: (path: string) => void
  markFileSaved: (path: string) => void
  addTab: (path: string, content: string) => void
  closeTab: (path: string) => void
  updateTabContent: (path: string, content: string) => void
  setPendingSemanticSync: (pending: boolean) => void
  setWorkspaceMutationLocked: (locked: boolean) => void
  flushEditor: (() => Promise<boolean>) | null
  setEditorFlush: (flush: (() => Promise<boolean>) | null) => void

  // Feedback
  feedbackMessages: FeedbackMessage[]
  currentStreaming: string
  isStreaming: boolean
  lastSync: string | null
  reviewStatus: ReviewStatus
  reviewRevision: number
  activeReviewRevision: number | null
  reviewServerSessionID: number | null
  reviewRequestID: string | null
  reviewSemanticHash: string
  reviewFiles: string[]
  reviewTestStatus: ReviewTestStatus
  reviewError: string | null
  feedbackUnread: boolean
  addFeedbackChunk: (chunk: string, revision?: number) => void
  setReviewIdle: (revision?: number, semanticHash?: string, files?: string[], requestID?: string) => void
  markReviewReady: (revision: number, semanticHash?: string, files?: string[], testStatus?: ReviewTestStatus, requestID?: string) => void
  markReviewRequesting: (revision?: number, semanticHash?: string, files?: string[], testStatus?: ReviewTestStatus) => void
  claimReviewRequest: (hasTestContext?: boolean) => boolean
  startFeedback: (revision?: number, semanticHash?: string, files?: string[], testStatus?: ReviewTestStatus, requestID?: string) => void
  cancelFeedback: (revision?: number) => void
  failFeedback: (message: string, revision?: number) => void
  endFeedback: (revision?: number) => void
  replayFeedback: (feedback: ReviewFeedbackReplay) => void
  setFeedbackRead: () => void
  setReviewTestStatus: (status: ReviewTestStatus) => void
  setLastSync: (time: string) => void
  setReviewRequestID: (requestID: string | null) => void
  adoptReviewServerSession: (sessionID: number) => void
  resetReviewState: () => void

  // Step complete
  stepComplete: boolean
  setStepComplete: (v: boolean) => void

  // Project complete (all steps done)
  projectComplete: boolean
  setProjectComplete: (v: boolean) => void

  // Diagnostics (Problems panel)
  diagnostics: DiagnosticItem[]
  setDiagnostics: (items: DiagnosticItem[]) => void

  // Pending navigate (Problems panel → editor jump)
  pendingNavigate: { path: string; line: number; column: number } | null
  setPendingNavigate: (v: { path: string; line: number; column: number } | null) => void

  // Snapshots
  snapshots: string[]
  setSnapshots: (s: string[]) => void

  // Test result
  testResult: TestResult | null
  setTestResult: (r: TestResult | null) => void
  testOutput: string
  setTestOutput: (output: string) => void

  // Skill level
  skillLevel: SkillLevel
  setSkillLevel: (level: SkillLevel) => void

  // Quiz
  quizData: QuizData
  setQuizData: (data: QuizData) => void
  solvedHoles: Set<string>
  markHoleSolved: (key: string) => void
  clearSolvedHoles: () => void

  // Chat
  chatMessages: ChatMessage[]
  isChatStreaming: boolean
  currentChatStreaming: string
  addUserChatMessage: (content: string) => void
  startChatStream: () => void
  addChatChunk: (chunk: string) => void
  cancelChatStream: () => void
  endChatStream: () => void

  // Quick Open / Search Panel
  showQuickOpen: boolean
  setShowQuickOpen: (v: boolean) => void
  showSearchPanel: boolean
  setShowSearchPanel: (v: boolean) => void

  // Minimap
  showMinimap: boolean
  setShowMinimap: (v: boolean) => void

  // Toast notifications
  toasts: Array<{ id: string; message: string; type: 'error' | 'success' | 'info' }>
  addToast: (message: string, type?: 'error' | 'success' | 'info') => void
  removeToast: (id: string) => void

  // WebSocket status
  wsStatus: 'connected' | 'reconnecting' | 'disconnected'
  setWsStatus: (s: 'connected' | 'reconnecting' | 'disconnected') => void

  // Atomically clear every project-scoped value before switching projects or users.
  resetWorkspace: () => void
}

type WorkspaceState = Pick<AppState,
  | 'projectDir'
  | 'projectStatus'
  | 'fileTree'
  | 'openFile'
  | 'openFileContent'
  | 'openFileReadOnly'
  | 'changedFiles'
  | 'openTabs'
  | 'pendingSemanticSync'
  | 'workspaceMutationLocked'
  | 'projectSessionEpoch'
  | 'flushEditor'
  | 'feedbackMessages'
  | 'currentStreaming'
  | 'isStreaming'
  | 'lastSync'
  | 'reviewStatus'
  | 'reviewRevision'
  | 'activeReviewRevision'
  | 'reviewServerSessionID'
  | 'reviewRequestID'
  | 'reviewSemanticHash'
  | 'reviewFiles'
  | 'reviewTestStatus'
  | 'reviewError'
  | 'feedbackUnread'
  | 'stepComplete'
  | 'projectComplete'
  | 'diagnostics'
  | 'pendingNavigate'
  | 'snapshots'
  | 'testResult'
  | 'testOutput'
  | 'quizData'
  | 'solvedHoles'
  | 'chatMessages'
  | 'isChatStreaming'
  | 'currentChatStreaming'
  | 'showQuickOpen'
  | 'showSearchPanel'
  | 'toasts'
>

function createWorkspaceState(): WorkspaceState {
  return {
    projectDir: '',
    projectStatus: null,
    fileTree: [],
    openFile: null,
    openFileContent: '',
    openFileReadOnly: false,
    changedFiles: new Set(),
    openTabs: [],
    pendingSemanticSync: false,
    workspaceMutationLocked: false,
    projectSessionEpoch: 0,
    flushEditor: null,
    feedbackMessages: [],
    currentStreaming: '',
    isStreaming: false,
    lastSync: null,
    reviewStatus: 'idle',
    reviewRevision: 0,
    activeReviewRevision: null,
    reviewServerSessionID: null,
    reviewRequestID: null,
    reviewSemanticHash: '',
    reviewFiles: [],
    reviewTestStatus: 'not_run',
    reviewError: null,
    feedbackUnread: false,
    stepComplete: false,
    projectComplete: false,
    diagnostics: [],
    pendingNavigate: null,
    snapshots: [],
    testResult: null,
    testOutput: '',
    quizData: {},
    solvedHoles: new Set(),
    chatMessages: [],
    isChatStreaming: false,
    currentChatStreaming: '',
    showQuickOpen: false,
    showSearchPanel: false,
    toasts: [],
  }
}

export const useStore = create<AppState>((set, get) => ({
  // Auth
  user: null,
  setUser: (user) => set({ user }),

  // User settings
  userSettings: null,
  setUserSettings: (s) => set({ userSettings: s }),

  // Project
  projectDir: '',
  projectStatus: null,
  setProjectDir: (dir) => set({ projectDir: dir }),
  setProjectStatus: (status) => set({ projectStatus: status, projectDir: status.dir }),
  clearProjectStatus: () => set({ projectStatus: null, projectDir: '' }),

  // Files
  fileTree: [],
  openFile: null,
  openFileContent: '',
  openFileReadOnly: false,
  changedFiles: new Set(),
  openTabs: [],
  pendingSemanticSync: false,
  workspaceMutationLocked: false,
  projectSessionEpoch: 0,
  flushEditor: null,
  setFileTree: (tree) => set({ fileTree: tree }),
  setOpenFile: (path) => set({ openFile: path, openFileReadOnly: false }),
  setOpenFileContent: (content) => {
    const { openFile, openTabs } = get()
    if (openFile) {
      set({
        openFileContent: content,
        openTabs: openTabs.map(t => t.path === openFile ? { ...t, content } : t),
      })
    } else {
      set({ openFileContent: content })
    }
  },
  setOpenFileReadOnly: (v) => set({ openFileReadOnly: v }),
  markFileChanged: (path) => {
    const s = new Set(get().changedFiles)
    s.add(path)
    set({ changedFiles: s })
  },
  markFileSaved: (path) => {
    const s = new Set(get().changedFiles)
    s.delete(path)
    set({ changedFiles: s })
  },
  addTab: (path, content) => {
    const { openTabs } = get()
    const exists = openTabs.find(t => t.path === path)
    if (exists) {
      set({ openFile: path, openFileContent: exists.content, openFileReadOnly: false })
    } else {
      set({
        openTabs: [...openTabs, { path, content }],
        openFile: path,
        openFileContent: content,
        openFileReadOnly: false,
      })
    }
  },
  closeTab: (path) => {
    const { openTabs, openFile } = get()
    const idx = openTabs.findIndex(t => t.path === path)
    const newTabs = openTabs.filter(t => t.path !== path)
    if (openFile === path) {
      const next = newTabs[idx] ?? newTabs[idx - 1] ?? null
      set({
        openTabs: newTabs,
        openFile: next?.path ?? null,
        openFileContent: next?.content ?? '',
        openFileReadOnly: false,
      })
    } else {
      set({ openTabs: newTabs })
    }
  },
  updateTabContent: (path, content) => {
    const { openTabs, openFile } = get()
    set({
      openTabs: openTabs.map(t => t.path === path ? { ...t, content } : t),
      ...(openFile === path ? { openFileContent: content } : {}),
    })
  },
  setEditorFlush: (flushEditor) => set({ flushEditor }),
  setPendingSemanticSync: (pendingSemanticSync) => set({ pendingSemanticSync }),
  setWorkspaceMutationLocked: (workspaceMutationLocked) => set({ workspaceMutationLocked }),

  // Feedback
  feedbackMessages: [],
  currentStreaming: '',
  isStreaming: false,
  lastSync: null,
  reviewStatus: 'idle',
  reviewRevision: 0,
  activeReviewRevision: null,
  reviewServerSessionID: null,
  reviewRequestID: null,
  reviewSemanticHash: '',
  reviewFiles: [],
  reviewTestStatus: 'not_run',
  reviewError: null,
  feedbackUnread: false,
  setReviewIdle: (revision, semanticHash, files, requestID) =>
    set((s) => {
      const idleRevision = revision ?? s.reviewRevision
      if (idleRevision < s.reviewRevision) return {}
      return {
        reviewStatus: 'idle',
        reviewRevision: idleRevision,
        activeReviewRevision: null,
        reviewRequestID: requestID ?? s.reviewRequestID,
        reviewSemanticHash: semanticHash ?? s.reviewSemanticHash,
        reviewFiles: files ?? [],
        reviewError: null,
        isStreaming: false,
        currentStreaming: '',
      }
    }),
  markReviewReady: (revision, semanticHash, files, testStatus, requestID) =>
    set((s) => {
      if (revision < s.reviewRevision) return {}
      return {
        reviewStatus: 'ready',
        reviewRevision: revision,
        activeReviewRevision: null,
        reviewRequestID: requestID ?? s.reviewRequestID,
        reviewSemanticHash: semanticHash ?? s.reviewSemanticHash,
        reviewFiles: files ?? s.reviewFiles,
        reviewTestStatus: testStatus ?? s.reviewTestStatus,
        reviewError: null,
        isStreaming: false,
        currentStreaming: '',
      }
    }),
  markReviewRequesting: (revision, semanticHash, files, testStatus) =>
    set((s) => {
      const requestedRevision = revision ?? s.reviewRevision
      if (requestedRevision < s.reviewRevision) return {}
      return {
        reviewStatus: 'requesting',
        reviewRevision: requestedRevision,
        activeReviewRevision: requestedRevision,
        reviewRequestID: null,
        reviewSemanticHash: semanticHash ?? s.reviewSemanticHash,
        reviewFiles: files ?? s.reviewFiles,
        reviewTestStatus: testStatus ?? s.reviewTestStatus,
        reviewError: null,
      }
    }),
  claimReviewRequest: (hasTestContext = false) => {
    let claimed = false
    set((s) => {
      if (s.reviewStatus === 'requesting' || s.reviewStatus === 'reviewing' ||
          !canUseReviewControl(s.reviewStatus, hasTestContext)) {
        return {}
      }
      claimed = true
      return {
        reviewStatus: 'requesting',
        activeReviewRevision: s.reviewRevision,
        reviewRequestID: null,
        reviewError: null,
      }
    })
    return claimed
  },
  startFeedback: (revision, semanticHash, files, testStatus, requestID) =>
    set((s) => {
      const startedRevision = revision ?? s.activeReviewRevision ?? s.reviewRevision
      if (startedRevision < s.reviewRevision) return {}
      return {
        isStreaming: true,
        currentStreaming: '',
        reviewStatus: 'reviewing',
        reviewRevision: startedRevision,
        activeReviewRevision: startedRevision,
        reviewRequestID: requestID ?? s.reviewRequestID,
        reviewSemanticHash: semanticHash ?? s.reviewSemanticHash,
        reviewFiles: files ?? s.reviewFiles,
        reviewTestStatus: testStatus ?? s.reviewTestStatus,
        reviewError: null,
      }
    }),
  cancelFeedback: (revision) =>
    set((s) => {
      if (revision !== undefined && s.activeReviewRevision !== null && revision !== s.activeReviewRevision) return {}
      const wasActive = s.isStreaming || s.reviewStatus === 'requesting' || s.reviewStatus === 'reviewing'
      return {
        isStreaming: false,
        currentStreaming: '',
        activeReviewRevision: null,
        reviewRequestID: s.reviewRequestID,
        ...(wasActive ? { reviewStatus: 'canceled' as const } : {}),
      }
    }),
  failFeedback: (message, revision) =>
    set((s) => {
      if (revision !== undefined && revision < s.reviewRevision) return {}
      return {
        isStreaming: false,
        currentStreaming: '',
        activeReviewRevision: null,
        reviewRequestID: s.reviewRequestID,
        reviewStatus: 'error',
        reviewError: message,
      }
    }),
  addFeedbackChunk: (chunk, revision) =>
    set((s) => {
      if (!s.isStreaming) return {}
      if (revision !== undefined && revision !== s.activeReviewRevision) return {}
      return { currentStreaming: s.currentStreaming + chunk, feedbackUnread: true }
    }),
  endFeedback: (revision) =>
    set((s) => {
      if (!s.isStreaming) return {}
      if (revision !== undefined && revision !== s.activeReviewRevision) return {}
      const content = s.currentStreaming.trim()
      const previous = s.feedbackMessages.at(-1)
      const duplicate = content !== '' && previous?.content.trim() === content
      const nextMessages = content === '' || duplicate
        ? s.feedbackMessages
        : [
            ...s.feedbackMessages,
            {
              id: `${Date.now()}-${s.activeReviewRevision ?? s.reviewRevision}`,
              content,
              timestamp: new Date().toISOString(),
              revision: s.activeReviewRevision ?? s.reviewRevision,
              semanticHash: s.reviewSemanticHash,
              files: s.reviewFiles,
              testStatus: s.reviewTestStatus,
            },
          ].slice(-20)
      return {
        isStreaming: false,
        feedbackMessages: nextMessages,
        currentStreaming: '',
        reviewStatus: 'idle',
        activeReviewRevision: null,
        reviewRequestID: s.reviewRequestID,
        reviewError: null,
      }
    }),
  replayFeedback: (feedback) =>
    set((s) => {
      const content = feedback.content.trim()
      if (!content) return {}
      const duplicate = s.feedbackMessages.some((message) =>
        message.revision === feedback.revision &&
        message.semanticHash === feedback.semanticHash &&
        message.content.trim() === content,
      )
      if (duplicate) return {}
      const newestRevision = s.feedbackMessages.reduce(
        (latest, message) => Math.max(latest, message.revision ?? 0),
        0,
      )
      if (newestRevision > feedback.revision) return {}
      return {
        feedbackMessages: [
          ...s.feedbackMessages,
          {
            id: `replay-${feedback.revision}-${feedback.semanticHash}`,
            content,
            timestamp: feedback.completedAt,
            revision: feedback.revision,
            semanticHash: feedback.semanticHash,
            files: feedback.files,
          },
        ].slice(-20),
        feedbackUnread: true,
      }
    }),
  setFeedbackRead: () => set({ feedbackUnread: false }),
  setReviewTestStatus: (reviewTestStatus) => set({ reviewTestStatus }),
  setLastSync: (time) => set({ lastSync: time }),
  setReviewRequestID: (reviewRequestID) => set({ reviewRequestID }),
  adoptReviewServerSession: (reviewServerSessionID) => set({
    feedbackMessages: [],
    currentStreaming: '',
    isStreaming: false,
    lastSync: null,
    reviewStatus: 'idle',
    reviewRevision: 0,
    activeReviewRevision: null,
    reviewServerSessionID,
    reviewRequestID: null,
    reviewSemanticHash: '',
    reviewFiles: [],
    reviewTestStatus: 'not_run',
    reviewError: null,
    feedbackUnread: false,
    pendingSemanticSync: false,
    stepComplete: false,
    testResult: null,
    testOutput: '',
  }),
  resetReviewState: () => set((s) => ({
    feedbackMessages: [],
    currentStreaming: '',
    isStreaming: false,
    lastSync: null,
    reviewStatus: 'idle',
    reviewRevision: 0,
    activeReviewRevision: null,
    reviewServerSessionID: null,
    reviewRequestID: null,
    reviewSemanticHash: '',
    reviewFiles: [],
    reviewTestStatus: 'not_run',
    reviewError: null,
    feedbackUnread: false,
    pendingSemanticSync: false,
    // Force a fresh socket for same-directory watcher restarts. Frames queued
    // on the previous connection cannot leak old review/test state into the
    // newly reset revision space.
    projectSessionEpoch: s.projectSessionEpoch + 1,
  })),

  // Step complete
  stepComplete: false,
  setStepComplete: (v) => set({ stepComplete: v }),

  // Project complete
  projectComplete: false,
  setProjectComplete: (v) => set({ projectComplete: v }),

  // Diagnostics
  diagnostics: [],
  setDiagnostics: (items) => set({ diagnostics: items }),

  // Pending navigate
  pendingNavigate: null,
  setPendingNavigate: (v) => set({ pendingNavigate: v }),

  // Test result
  testResult: null,
  setTestResult: (r) => set({ testResult: r, ...(r === null ? { testOutput: '' } : {}) }),
  testOutput: '',
  setTestOutput: (testOutput) => set({ testOutput }),

  // Snapshots
  snapshots: [],
  setSnapshots: (s) => set({ snapshots: s }),

  // Skill level
  skillLevel: 'normal',
  setSkillLevel: (level) => set({ skillLevel: level }),

  // Quiz
  quizData: {},
  setQuizData: (data) => set({ quizData: data }),
  solvedHoles: new Set(),
  markHoleSolved: (key) => {
    const s = new Set(get().solvedHoles)
    s.add(key)
    set({ solvedHoles: s })
  },
  clearSolvedHoles: () => set({ solvedHoles: new Set() }),

  // Chat
  chatMessages: [],
  isChatStreaming: false,
  currentChatStreaming: '',
  addUserChatMessage: (content) =>
    set((s) => ({ chatMessages: [...s.chatMessages, { role: 'user', content }] })),
  startChatStream: () => set({ isChatStreaming: true, currentChatStreaming: '' }),
  addChatChunk: (chunk) =>
    set((s) => s.isChatStreaming ? { currentChatStreaming: s.currentChatStreaming + chunk } : {}),
  cancelChatStream: () => set({ isChatStreaming: false, currentChatStreaming: '' }),
  endChatStream: () =>
    set((s) => {
      if (!s.isChatStreaming) return {}
      return {
        isChatStreaming: false,
        chatMessages: [
          ...s.chatMessages,
          { role: 'ai' as const, content: s.currentChatStreaming },
        ],
        currentChatStreaming: '',
      }
    }),

  // Quick Open / Search Panel
  showQuickOpen: false,
  setShowQuickOpen: (v) => set({ showQuickOpen: v }),
  showSearchPanel: false,
  setShowSearchPanel: (v) => set({ showSearchPanel: v }),

  // Minimap
  showMinimap: false,
  setShowMinimap: (v) => set({ showMinimap: v }),

  // Toast
  toasts: [],
  addToast: (message, type = 'info') => {
    const id = Date.now().toString() + Math.random()
    set((s) => ({ toasts: [...s.toasts, { id, message, type }] }))
    setTimeout(() => {
      set((s) => ({ toasts: s.toasts.filter(t => t.id !== id) }))
    }, 4000)
  },
  removeToast: (id) => set((s) => ({ toasts: s.toasts.filter(t => t.id !== id) })),

  // WebSocket status
  wsStatus: 'disconnected',
  setWsStatus: (s) => set({ wsStatus: s }),

  resetWorkspace: () => set(createWorkspaceState()),
}))
