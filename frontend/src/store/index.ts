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
}

export type SkillLevel = 'newbie' | 'normal' | 'experienced'

export interface ChatMessage {
  role: 'user' | 'ai'
  content: string
}

export interface QuizOption {
  label: string
  isCorrect: boolean
}

export interface QuizItem {
  key: string
  filename: string
  markerType: string   // "hole" | "bug"
  markerIndex: number
  question: string
  hints: string[]      // 3단계: 개념 → 구조 → 거의 다
  options: QuizOption[]
  correctCode: string
}

export type QuizData = Record<string, QuizItem>

export interface UserSettings {
  user_id: string
  base_dir: string
  language: string
  skill_level: string
}

export interface DiagnosticItem {
  filePath: string
  fileName: string
  message: string
  severity: 1 | 2 | 3 | 4
  startLine: number
  startColumn: number
}

interface AppState {
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
  setFileTree: (tree: FileEntry[]) => void
  setOpenFile: (path: string | null) => void
  setOpenFileContent: (content: string) => void
  setOpenFileReadOnly: (v: boolean) => void
  markFileChanged: (path: string) => void
  markFileSaved: (path: string) => void
  addTab: (path: string, content: string) => void
  closeTab: (path: string) => void
  updateTabContent: (path: string, content: string) => void

  // Feedback
  feedbackMessages: FeedbackMessage[]
  currentStreaming: string
  isStreaming: boolean
  lastSync: string | null
  addFeedbackChunk: (chunk: string) => void
  startFeedback: () => void
  endFeedback: () => void
  setLastSync: (time: string) => void

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
  testResult: { passed: boolean; summary: string } | null
  setTestResult: (r: { passed: boolean; summary: string } | null) => void

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
  endChatStream: () => void
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
  setProjectStatus: (status) => set({ projectStatus: status }),
  clearProjectStatus: () => set({ projectStatus: null }),

  // Files
  fileTree: [],
  openFile: null,
  openFileContent: '',
  openFileReadOnly: false,
  changedFiles: new Set(),
  openTabs: [],
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

  // Feedback
  feedbackMessages: [],
  currentStreaming: '',
  isStreaming: false,
  lastSync: null,
  startFeedback: () =>
    set({ isStreaming: true, currentStreaming: '' }),
  addFeedbackChunk: (chunk) =>
    set((s) => ({ currentStreaming: s.currentStreaming + chunk })),
  endFeedback: () =>
    set((s) => ({
      isStreaming: false,
      feedbackMessages: [
        ...s.feedbackMessages,
        {
          id: Date.now().toString(),
          content: s.currentStreaming,
          timestamp: new Date().toISOString(),
        },
      ],
      currentStreaming: '',
    })),
  setLastSync: (time) => set({ lastSync: time }),

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
  setTestResult: (r) => set({ testResult: r }),

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
    set((s) => ({ currentChatStreaming: s.currentChatStreaming + chunk })),
  endChatStream: () =>
    set((s) => ({
      isChatStreaming: false,
      chatMessages: [
        ...s.chatMessages,
        { role: 'ai' as const, content: s.currentChatStreaming },
      ],
      currentChatStreaming: '',
    })),
}))
