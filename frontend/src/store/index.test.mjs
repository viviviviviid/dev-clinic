import assert from 'node:assert/strict'
import test from 'node:test'

import { canUseReviewControl, useStore } from './index.ts'

test('manual review is disabled for idle code but test context remains independently actionable', () => {
  assert.equal(canUseReviewControl('idle'), false)
  assert.equal(canUseReviewControl('canceled'), false)
  assert.equal(canUseReviewControl('ready'), true)
  assert.equal(canUseReviewControl('error'), true)
  assert.equal(canUseReviewControl('reviewing'), true)
  assert.equal(canUseReviewControl('idle', true), true)
})

test('only one surface can atomically claim a manual review request', () => {
  useStore.getState().resetWorkspace()
  useStore.getState().markReviewReady(3, 'hash-3', ['main.ts'], 'not_run')

  assert.equal(useStore.getState().claimReviewRequest(), true)
  assert.equal(useStore.getState().claimReviewRequest(), false)
  assert.equal(useStore.getState().reviewStatus, 'requesting')
  assert.equal(useStore.getState().activeReviewRevision, 3)
})

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
    pendingSemanticSync: true,
    flushEditor: async () => {},
    feedbackMessages: [{ id: '1', content: 'old feedback', timestamp: '2026-01-01' }],
    currentStreaming: 'partial feedback',
    isStreaming: true,
    lastSync: '2026-01-01',
    reviewStatus: 'reviewing',
    reviewRevision: 7,
    activeReviewRevision: 7,
    reviewSemanticHash: 'old-hash',
    reviewFiles: ['main.ts'],
    reviewTestStatus: 'failed',
    reviewError: 'old error',
    feedbackUnread: true,
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
    testOutput: 'old test output',
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
  useStore.getState().setWorkspaceMutationLocked(true)
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
  assert.equal(state.pendingSemanticSync, false)
  assert.equal(state.workspaceMutationLocked, false)
  assert.equal(state.flushEditor, null)
  assert.deepEqual(state.feedbackMessages, [])
  assert.equal(state.currentStreaming, '')
  assert.equal(state.isStreaming, false)
  assert.equal(state.lastSync, null)
  assert.equal(state.reviewStatus, 'idle')
  assert.equal(state.reviewRevision, 0)
  assert.equal(state.activeReviewRevision, null)
  assert.equal(state.reviewSemanticHash, '')
  assert.deepEqual(state.reviewFiles, [])
  assert.equal(state.reviewTestStatus, 'not_run')
  assert.equal(state.reviewError, null)
  assert.equal(state.feedbackUnread, false)
  assert.equal(state.stepComplete, false)
  assert.equal(state.projectComplete, false)
  assert.deepEqual(state.diagnostics, [])
  assert.equal(state.pendingNavigate, null)
  assert.deepEqual(state.snapshots, [])
  assert.equal(state.testResult, null)
  assert.equal(state.testOutput, '')
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

test('review state rejects stale revisions and marks only real feedback unread', () => {
  useStore.getState().resetWorkspace()
  useStore.getState().markReviewReady(5, 'hash-5', ['main.ts'], 'not_run')
  useStore.getState().startFeedback(4, 'hash-4', ['old.ts'], 'failed')
  assert.equal(useStore.getState().reviewStatus, 'ready')

  useStore.getState().startFeedback(5, 'hash-5', ['main.ts'], 'failed')
  assert.equal(useStore.getState().feedbackUnread, false)
  useStore.getState().addFeedbackChunk('stale', 4)
  assert.equal(useStore.getState().currentStreaming, '')
  useStore.getState().addFeedbackChunk('current', 5)
  assert.equal(useStore.getState().currentStreaming, 'current')
  assert.equal(useStore.getState().feedbackUnread, true)

  useStore.getState().markReviewReady(6, 'hash-6', ['next.ts'], 'not_run')
  useStore.getState().endFeedback(5)
  const state = useStore.getState()
  assert.equal(state.reviewStatus, 'ready')
  assert.equal(state.reviewRevision, 6)
  assert.equal(state.currentStreaming, '')
  assert.deepEqual(state.feedbackMessages, [])
})

test('semantic sync gate stays closed after autosave until the server acknowledges the edit', () => {
  useStore.getState().resetWorkspace()
  useStore.getState().markFileChanged('/projects/current/main.ts')
  useStore.getState().setPendingSemanticSync(true)
  useStore.getState().markFileSaved('/projects/current/main.ts')
  assert.equal(useStore.getState().pendingSemanticSync, true)

  useStore.getState().setPendingSemanticSync(false)
  assert.equal(useStore.getState().pendingSemanticSync, false)
})

test('feedback history is deduplicated and capped at the newest 20 reviews', () => {
  useStore.getState().resetWorkspace()
  for (let revision = 1; revision <= 22; revision += 1) {
    useStore.getState().startFeedback(revision, `hash-${revision}`, [`file-${revision}.ts`], 'passed')
    useStore.getState().addFeedbackChunk(`feedback ${revision}`, revision)
    useStore.getState().endFeedback(revision)
  }

  let messages = useStore.getState().feedbackMessages
  assert.equal(messages.length, 20)
  assert.equal(messages[0].content, 'feedback 3')
  assert.equal(messages.at(-1).content, 'feedback 22')

  useStore.getState().startFeedback(23, 'hash-23', ['file-23.ts'], 'passed')
  useStore.getState().addFeedbackChunk('feedback 22', 23)
  useStore.getState().endFeedback(23)
  messages = useStore.getState().feedbackMessages
  assert.equal(messages.length, 20)
  assert.equal(messages.filter((message) => message.content === 'feedback 22').length, 1)
})

test('completed feedback replay recovers one missed result without duplicates', () => {
  useStore.getState().resetWorkspace()
  const replay = {
    revision: 7,
    semanticHash: 'hash-7',
    files: ['main.ts'],
    content: '놓친 검토 결과',
    completedAt: '2026-08-17T12:00:00Z',
  }
  useStore.getState().replayFeedback(replay)
  useStore.getState().replayFeedback(replay)

  const state = useStore.getState()
  assert.equal(state.feedbackMessages.length, 1)
  assert.equal(state.feedbackMessages[0].content, replay.content)
  assert.equal(state.feedbackMessages[0].revision, replay.revision)
  assert.equal(state.feedbackUnread, true)
})

test('watcher restart resets the project review session even when the directory is unchanged', () => {
  useStore.getState().resetWorkspace()
  useStore.getState().adoptReviewServerSession(7)
  useStore.getState().setReviewRequestID('request-old')
  useStore.getState().markReviewReady(5, 'old-hash', ['old.ts'], 'failed')
  useStore.getState().startFeedback(5, 'old-hash', ['old.ts'], 'failed')
  useStore.getState().addFeedbackChunk('old step feedback', 5)
  useStore.getState().setPendingSemanticSync(true)
  const previousEpoch = useStore.getState().projectSessionEpoch

  useStore.getState().resetReviewState()

  const state = useStore.getState()
  assert.equal(state.reviewStatus, 'idle')
  assert.equal(state.reviewRevision, 0)
  assert.equal(state.activeReviewRevision, null)
  assert.equal(state.reviewServerSessionID, null)
  assert.equal(state.reviewRequestID, null)
  assert.equal(state.reviewSemanticHash, '')
  assert.deepEqual(state.reviewFiles, [])
  assert.equal(state.reviewTestStatus, 'not_run')
  assert.equal(state.reviewError, null)
  assert.equal(state.isStreaming, false)
  assert.equal(state.currentStreaming, '')
  assert.deepEqual(state.feedbackMessages, [])
  assert.equal(state.feedbackUnread, false)
  assert.equal(state.pendingSemanticSync, false)
  assert.equal(state.projectSessionEpoch, previousEpoch + 1)
})

test('adopting a new server session clears stale review, test, and completion state', () => {
  useStore.getState().resetWorkspace()
  useStore.getState().adoptReviewServerSession(8)
  useStore.getState().markReviewReady(5, 'hash-5', ['main.ts'], 'passed', 'request-5')
  useStore.getState().setTestResult({ passed: true, summary: 'passed', scope: 'full' })
  useStore.getState().setStepComplete(true)
  useStore.getState().replayFeedback({
    revision: 5,
    semanticHash: 'hash-5',
    files: ['main.ts'],
    content: 'old feedback',
    completedAt: '2026-08-17T12:00:00Z',
  })

  useStore.getState().adoptReviewServerSession(9)

  const state = useStore.getState()
  assert.equal(state.reviewServerSessionID, 9)
  assert.equal(state.reviewRequestID, null)
  assert.equal(state.reviewStatus, 'idle')
  assert.equal(state.reviewRevision, 0)
  assert.equal(state.testResult, null)
  assert.equal(state.stepComplete, false)
  assert.deepEqual(state.feedbackMessages, [])
})
