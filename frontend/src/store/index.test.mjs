import assert from 'node:assert/strict'
import test from 'node:test'

import { useStore } from './index.ts'

test('resetWorkspace clears project-scoped state in one store update', () => {
  useStore.getState().resetWorkspace()
  const priorChangedFiles = new Set(['/projects/old/main.ts'])
  const priorSolvedHoles = new Set(['main.ts:hole:0'])
  useStore.setState({
    projectDir: '/projects/old',
    projectStatus: {
      loaded: true,
      dir: '/projects/old',
      language: 'typescript',
      currentStep: 'Step 2',
      goal: 'goal',
      concept: 'concept',
      tasks: 'tasks',
      content: 'content',
      skillLevel: 'experienced',
      totalSteps: 3,
      currentStepNum: 2,
    },
    fileTree: [{ name: 'main.ts', path: '/projects/old/main.ts', isDir: false }],
    openFile: '/projects/old/main.ts',
    openFileContent: 'old project content',
    openFileReadOnly: true,
    changedFiles: priorChangedFiles,
    openTabs: [{ path: '/projects/old/main.ts', content: 'old project content' }],
    feedbackMessages: [{ id: '1', content: 'old feedback', timestamp: '2026-01-01' }],
    currentStreaming: 'partial feedback',
    isStreaming: true,
    lastSync: '2026-01-01',
    stepComplete: true,
    projectComplete: true,
    diagnostics: [{
      filePath: '/projects/old/main.ts',
      fileName: 'main.ts',
      message: 'old diagnostic',
      severity: 1,
      startLine: 1,
      startColumn: 1,
    }],
    pendingNavigate: { path: '/projects/old/main.ts', line: 1, column: 1 },
    snapshots: ['old-snapshot'],
    testResult: { passed: true, summary: 'old result' },
    quizData: {
      old: {
        key: 'old',
        filename: 'main.ts',
        markerType: 'hole',
        markerIndex: 0,
        question: 'old question',
        hints: ['one'],
      },
    },
    solvedHoles: priorSolvedHoles,
    chatMessages: [{ role: 'user', content: 'old chat' }],
    isChatStreaming: true,
    currentChatStreaming: 'partial chat',
    showQuickOpen: true,
    showSearchPanel: true,
    toasts: [{ id: 'old', message: 'old toast', type: 'error' }],
    skillLevel: 'experienced',
    showMinimap: true,
    wsStatus: 'connected',
  })

  let notifications = 0
  const unsubscribe = useStore.subscribe(() => { notifications += 1 })
  useStore.getState().resetWorkspace()
  unsubscribe()

  const state = useStore.getState()
  assert.equal(notifications, 1)
  assert.equal(state.projectDir, '')
  assert.equal(state.projectStatus, null)
  assert.deepEqual(state.fileTree, [])
  assert.equal(state.openFile, null)
  assert.equal(state.openFileContent, '')
  assert.equal(state.openFileReadOnly, false)
  assert.deepEqual([...state.changedFiles], [])
  assert.notEqual(state.changedFiles, priorChangedFiles)
  assert.deepEqual(state.openTabs, [])
  assert.deepEqual(state.feedbackMessages, [])
  assert.equal(state.currentStreaming, '')
  assert.equal(state.isStreaming, false)
  assert.equal(state.lastSync, null)
  assert.equal(state.stepComplete, false)
  assert.equal(state.projectComplete, false)
  assert.deepEqual(state.diagnostics, [])
  assert.equal(state.pendingNavigate, null)
  assert.deepEqual(state.snapshots, [])
  assert.equal(state.testResult, null)
  assert.deepEqual(state.quizData, {})
  assert.deepEqual([...state.solvedHoles], [])
  assert.notEqual(state.solvedHoles, priorSolvedHoles)
  assert.deepEqual(state.chatMessages, [])
  assert.equal(state.isChatStreaming, false)
  assert.equal(state.currentChatStreaming, '')
  assert.equal(state.showQuickOpen, false)
  assert.equal(state.showSearchPanel, false)
  assert.deepEqual(state.toasts, [])

  // User preferences and transport state are not owned by a workspace reset.
  assert.equal(state.skillLevel, 'experienced')
  assert.equal(state.showMinimap, true)
  assert.equal(state.wsStatus, 'connected')
})

test('project status keeps the websocket project identity in sync', () => {
  const state = useStore.getState()
  state.setProjectStatus({
    loaded: true,
    dir: '/projects/current',
    language: 'typescript',
    currentStep: 'Step 1',
    goal: '',
    concept: '',
    tasks: '',
    content: '',
    skillLevel: 'normal',
    totalSteps: 2,
    currentStepNum: 1,
  })
  assert.equal(useStore.getState().projectDir, '/projects/current')

  useStore.getState().clearProjectStatus()
  assert.equal(useStore.getState().projectStatus, null)
  assert.equal(useStore.getState().projectDir, '')
})

test('stream cancellation drops partial content without appending a message', () => {
  useStore.getState().resetWorkspace()

  useStore.getState().startFeedback()
  useStore.getState().addFeedbackChunk('partial feedback')
  useStore.getState().cancelFeedback()
  useStore.getState().addFeedbackChunk('late feedback')
  useStore.getState().endFeedback()
  assert.equal(useStore.getState().isStreaming, false)
  assert.equal(useStore.getState().currentStreaming, '')
  assert.deepEqual(useStore.getState().feedbackMessages, [])

  useStore.getState().startChatStream()
  useStore.getState().addChatChunk('partial chat')
  useStore.getState().cancelChatStream()
  useStore.getState().addChatChunk('late chat')
  useStore.getState().endChatStream()
  assert.equal(useStore.getState().isChatStreaming, false)
  assert.equal(useStore.getState().currentChatStreaming, '')
  assert.deepEqual(useStore.getState().chatMessages, [])
})
