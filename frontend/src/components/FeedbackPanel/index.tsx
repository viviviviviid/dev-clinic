import { useState, useEffect, useRef } from 'react'
import type { KeyboardEvent } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { canUseReviewControl, useStore } from '../../store'
import type { ProjectStatus } from '../../store'
import {
  applyReviewSnapshot,
  reviewRecoveryFromRequestError,
  reviewRequestErrorMessage,
  useProject,
} from '../../hooks/useProject'
import type { ReviewRequest } from '../../hooks/useProject'
import { getErrorMessage, isAbortError } from '../../lib/errors'
import Confetti from '../Confetti'
import { advanceButtonLabel } from './advanceUx'
import { ChatSseParser } from './chatSse'
import './FeedbackPanel.css'

type Tab = 'feedback' | 'tasks' | 'chat'

export default function FeedbackPanel() {
  const [tab, setTab] = useState<Tab>('tasks')
  const [advancing, setAdvancing] = useState(false)
  const [chatInput, setChatInput] = useState('')
  const [showSnapshotMenu, setShowSnapshotMenu] = useState(false)
  const [restoringStep, setRestoringStep] = useState<string | null>(null)
  const [showConfetti, setShowConfetti] = useState(false)
  const [pendingStepStatus, setPendingStepStatus] = useState<ProjectStatus | null>(null)
  const [pendingCompletion, setPendingCompletion] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const chatScrollRef = useRef<HTMLDivElement>(null)
  const chatAbortRef = useRef<AbortController | null>(null)
  const advanceAbortRef = useRef<AbortController | null>(null)
  const restoreAbortRef = useRef<AbortController | null>(null)
  const reviewAbortRef = useRef<AbortController | null>(null)
  const [reviewAction, setReviewAction] = useState<'start' | 'cancel' | null>(null)
  const {
    feedbackMessages,
    currentStreaming,
    isStreaming,
    lastSync,
    stepComplete,
    setStepComplete,
    projectComplete,
    setProjectComplete,
    projectStatus,
    setProjectStatus,
    resetWorkspace,
    skillLevel,
    setQuizData,
    clearSolvedHoles,
    openFileContent,
    chatMessages,
    isChatStreaming,
    currentChatStreaming,
    addUserChatMessage,
    startChatStream,
    cancelChatStream,
    addChatChunk,
    endChatStream,
    testResult,
    testOutput,
    setTestResult,
    snapshots,
    setSnapshots,
    addToast,
    pendingSemanticSync,
    workspaceMutationLocked,
    reviewStatus,
    reviewRevision,
    reviewFiles,
    reviewTestStatus,
    reviewError,
    feedbackUnread,
    failFeedback,
    setFeedbackRead,
  } = useStore()
  const {
    advanceToNextStep,
    completeMission,
    refreshStatus,
    refreshFileTree,
    reloadOpenTabs,
    loadQuizData,
    sendChat,
    listSnapshots,
    restoreSnapshot,
    requestReview,
    cancelReview,
  } = useProject()
  const requestReviewRef = useRef(requestReview)
  useEffect(() => { requestReviewRef.current = requestReview }, [requestReview])
  const cancelReviewRef = useRef(cancelReview)
  useEffect(() => { cancelReviewRef.current = cancelReview }, [cancelReview])
  const finalStep = Boolean(projectStatus && projectStatus.totalSteps > 0 && projectStatus.currentStepNum >= projectStatus.totalSteps)
  const recoveringStep = Boolean(pendingStepStatus || pendingCompletion)

  async function syncAppliedStep(data: ProjectStatus, signal: AbortSignal) {
    setProjectStatus(data)
    clearSolvedHoles()
    await refreshFileTree(data.dir, signal)
    await reloadOpenTabs(signal)
    if (skillLevel === 'newbie') {
      const quiz = await loadQuizData(signal)
      setQuizData(quiz)
    }
    const snaps = await listSnapshots(signal)
    setSnapshots(snaps)
    setTestResult(null)
    setStepComplete(false)
    setPendingStepStatus(null)
  }

  async function finishMission() {
    await completeMission()
    setPendingCompletion(false)
    setStepComplete(false)
    setProjectComplete(true)
  }

  async function handleNextStep() {
    const beforeFlush = useStore.getState()
    if (beforeFlush.workspaceMutationLocked) return
    if (beforeFlush.pendingSemanticSync) {
      addToast('저장된 변경의 의미를 확인한 뒤 다음 단계로 이동할 수 있습니다.', 'info')
      return
    }
    if (!beforeFlush.stepComplete || beforeFlush.testResult?.passed !== true || beforeFlush.testResult.scope !== 'full') {
      addToast('최신 코드로 전체 테스트를 통과한 뒤 다음 단계로 이동할 수 있습니다.', 'info')
      return
    }
    if (advanceAbortRef.current) return
    const abort = new AbortController()
    advanceAbortRef.current = abort
    setAdvancing(true)
    useStore.getState().setWorkspaceMutationLocked(true)
    try {
      await new Promise<void>((resolve) => window.setTimeout(resolve, 0))
      const flushEditor = useStore.getState().flushEditor
      if (!flushEditor || !await flushEditor()) {
        throw new Error('최신 코드를 저장하지 못해 다음 단계로 이동하지 않았습니다.')
      }
      const afterFlush = useStore.getState()
      if (afterFlush.pendingSemanticSync || !afterFlush.stepComplete ||
          afterFlush.testResult?.passed !== true || afterFlush.testResult.scope !== 'full') {
        throw new Error('저장 중 코드나 테스트 상태가 변경되었습니다. 전체 테스트를 다시 실행해 주세요.')
      }
      if (pendingCompletion) {
        await finishMission()
        return
      }
      if (pendingStepStatus) {
        await syncAppliedStep(pendingStepStatus, abort.signal)
        return
      }

      const data = await advanceToNextStep(abort.signal)
      if (data.done) {
        setPendingCompletion(true)
        await finishMission()
        return
      }
      if (data.loaded) {
        setPendingStepStatus(data)
        await syncAppliedStep(data, abort.signal)
      }
    } catch (error: unknown) {
      if (!isAbortError(error)) {
        const prefix = pendingStepStatus || pendingCompletion ? '단계 상태 복구 실패' : '다음 단계 생성 실패'
        addToast(`${prefix}: ${getErrorMessage(error)}`, 'error')
      }
    } finally {
      if (advanceAbortRef.current === abort) {
        advanceAbortRef.current = null
        setAdvancing(false)
      }
      useStore.getState().setWorkspaceMutationLocked(false)
    }
  }

  async function handleRestoreSnapshot(step: string) {
    if (restoreAbortRef.current || useStore.getState().workspaceMutationLocked) return
    const confirmed = window.confirm(
      '이 스냅샷으로 복원하면 현재 소스 변경과 스냅샷 이후 생성된 파일이 되돌아갑니다. 복구가 어려울 수 있습니다. 계속할까요?',
    )
    if (!confirmed) return

    const abort = new AbortController()
    restoreAbortRef.current = abort
    setRestoringStep(step)
    setShowSnapshotMenu(false)
    useStore.getState().setWorkspaceMutationLocked(true)
    try {
      await new Promise<void>((resolve) => window.setTimeout(resolve, 0))
      const flushEditor = useStore.getState().flushEditor
      if (!flushEditor || !await flushEditor()) {
        throw new Error('최신 코드를 저장하지 못해 스냅샷을 복원하지 않았습니다.')
      }
      await restoreSnapshot(step, abort.signal)
      const status = await refreshStatus(abort.signal)
      await refreshFileTree(status.dir, abort.signal)
      await reloadOpenTabs(abort.signal)
      if (skillLevel === 'newbie') setQuizData(await loadQuizData(abort.signal))
      setSnapshots(await listSnapshots(abort.signal))
      clearSolvedHoles()
      setPendingStepStatus(null)
      setPendingCompletion(false)
      setTestResult(null)
      setStepComplete(false)
      addToast(`${step} 스냅샷으로 복원했습니다. 테스트를 다시 실행해 주세요.`, 'success')
    } catch (error: unknown) {
      if (!isAbortError(error)) addToast(`스냅샷 복원 실패: ${getErrorMessage(error)}`, 'error')
    } finally {
      if (restoreAbortRef.current === abort) {
        restoreAbortRef.current = null
        setRestoringStep(null)
      }
      useStore.getState().setWorkspaceMutationLocked(false)
    }
  }

  async function handleSendChat() {
    const msg = chatInput.trim()
    if (!msg || isChatStreaming) return

    setChatInput('')
    addUserChatMessage(msg)
    startChatStream()
    chatAbortRef.current?.abort()
    const abort = new AbortController()
    chatAbortRef.current = abort

    try {
      const stream = await sendChat(msg, openFileContent, chatMessages, abort.signal)
      const reader = stream.getReader()
      const decoder = new TextDecoder()
      const parser = new ChatSseParser()
      let receivedDone = false

      try {
        while (true) {
          const { done, value } = await reader.read()
          const events = done
            ? [...parser.push(decoder.decode()), ...parser.finish()]
            : parser.push(decoder.decode(value, { stream: true }))
          for (const event of events) {
            if (event.type === 'message') addChatChunk(event.text)
            if (event.type === 'done') receivedDone = true
          }
          if (done) break
        }
      } finally {
        reader.releaseLock()
      }

      if (!receivedDone) throw new Error('AI 채팅이 완료 신호 없이 종료되었습니다. 다시 시도해 주세요.')
      endChatStream()
    } catch (error: unknown) {
      if (isAbortError(error)) {
        cancelChatStream()
        return
      }
      const message = getErrorMessage(error)
      addChatChunk(`응답을 불러오지 못했습니다: ${message}`)
      addToast(`채팅 오류: ${message}`, 'error')
      endChatStream()
    } finally {
      if (chatAbortRef.current === abort) chatAbortRef.current = null
    }
  }

  function handleChatKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSendChat()
    }
  }

  async function handleManualReview(request: ReviewRequest = {}) {
    if (reviewAction || !projectStatus?.loaded) return
    const isActive = reviewStatus === 'requesting' || reviewStatus === 'reviewing'
    const hasTestContext = request.test_context !== undefined
    if (!canUseReviewControl(reviewStatus, hasTestContext)) {
      addToast('검토할 의미 있는 코드 변경이 없습니다.', 'info')
      return
    }
    const abort = new AbortController()
    reviewAbortRef.current?.abort()
    reviewAbortRef.current = abort
    setReviewAction(isActive ? 'cancel' : 'start')

    try {
      if (isActive) {
        const snapshot = await cancelReviewRef.current(abort.signal)
        applyReviewSnapshot(snapshot)
        addToast('AI 검토를 중단했습니다.', 'info')
        return
      }

      const flushEditor = useStore.getState().flushEditor
      if (!flushEditor || !await flushEditor()) {
        throw new Error('최신 코드를 저장하지 못해 AI 검토를 시작하지 않았습니다.')
      }
      if (abort.signal.aborted) return
      const latest = useStore.getState()
      if (!latest.claimReviewRequest(hasTestContext)) {
        addToast(
          latest.reviewStatus === 'requesting' || latest.reviewStatus === 'reviewing'
            ? '이미 AI 검토 요청을 처리하고 있습니다.'
            : '검토할 의미 있는 코드 변경이 없습니다.',
          'info',
        )
        return
      }
      const snapshot = await requestReviewRef.current(request, abort.signal)
      applyReviewSnapshot(snapshot, 'start')
    } catch (error: unknown) {
      if (isAbortError(error)) return
      const recovery = !isActive ? reviewRecoveryFromRequestError(error) : null
      if (recovery) {
        applyReviewSnapshot(recovery.snapshot)
        const message = reviewRequestErrorMessage(error)
        addToast(
          recovery.code === 'review_stale'
            ? '코드가 바뀌어 최신 revision으로 갱신했습니다. AI 검토를 다시 눌러 주세요.'
            : message,
          recovery.code === 'review_rate_limited' ? 'error' : 'info',
        )
        return
      }
      const message = reviewRequestErrorMessage(error)
      if (!isActive) failFeedback(message)
      addToast(`AI 검토 ${isActive ? '중단' : '요청'} 실패: ${message}`, 'error')
    } finally {
      if (reviewAbortRef.current === abort) {
        reviewAbortRef.current = null
        setReviewAction(null)
      }
    }
  }

  function handleFailureReview() {
    if (!testResult || testResult.passed) return
    if (!testResult.inputHash) {
      addToast('이 테스트 결과의 코드 버전을 확인할 수 없습니다. 테스트를 다시 실행해 주세요.', 'info')
      return
    }
    const scopeLabel = testResult.scope === 'targeted' ? '개별 테스트' : '전체 테스트'
    void handleManualReview({
      test_context: {
        passed: false,
        summary: `[${scopeLabel}] ${testResult.summary}`,
        output: testOutput || undefined,
        input_hash: testResult.inputHash,
      },
    })
  }

  function formatTime(iso: string) {
    try {
      return new Date(iso).toLocaleTimeString('ko-KR', {
        hour: '2-digit', minute: '2-digit', second: '2-digit',
      })
    } catch { return iso }
  }

  useEffect(() => () => {
    chatAbortRef.current?.abort()
    advanceAbortRef.current?.abort()
    restoreAbortRef.current?.abort()
    reviewAbortRef.current?.abort()
    cancelChatStream()
  }, [cancelChatStream])

  // AI 응답이 시작돼도 사용자가 읽고 있던 과제/채팅 탭을 바꾸지 않습니다.
  const activeTab: Tab = tab

  useEffect(() => {
    if (activeTab === 'feedback' && feedbackUnread) setFeedbackRead()
  }, [activeTab, feedbackUnread, setFeedbackRead])

  // 새 피드백 or 스트리밍 시작 시 맨 아래로 스크롤
  useEffect(() => {
    if (activeTab === 'feedback' && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [feedbackMessages.length, isStreaming, activeTab])

  // 채팅 메시지 추가 시 맨 아래로 스크롤
  useEffect(() => {
    if (activeTab === 'chat' && chatScrollRef.current) {
      chatScrollRef.current.scrollTop = chatScrollRef.current.scrollHeight
    }
  }, [chatMessages.length, isChatStreaming, activeTab])

  const prevStepComplete = useRef(false)
  useEffect(() => {
    const isCurrentStepComplete = stepComplete && testResult?.passed === true && testResult.scope === 'full'
    if (isCurrentStepComplete && !prevStepComplete.current) {
      setShowConfetti(true)
    }
    prevStepComplete.current = isCurrentStepComplete
  }, [stepComplete, testResult?.passed, testResult?.scope])

  const reviewBusy = reviewStatus === 'requesting' || reviewStatus === 'reviewing'
  const reviewCanStart = canUseReviewControl(reviewStatus) && !reviewBusy
  const reviewFileSummary = reviewFiles.length > 0
    ? `${reviewFiles[0]}${reviewFiles.length > 1 ? ` 외 ${reviewFiles.length - 1}개` : ''}`
    : '변경 파일 없음'
  const reviewTestLabel = pendingSemanticSync
    ? '변경 확인 중'
    : reviewTestStatus === 'passed'
      ? `${testResult?.scope === 'targeted' ? '개별' : '전체'} 테스트 통과`
      : reviewTestStatus === 'failed'
        ? `${testResult?.scope === 'targeted' ? '개별' : '전체'} 테스트 실패`
        : '테스트 미실행'
  const reviewStatusLabel = reviewStatus === 'ready'
    ? '검토 준비됨'
    : reviewStatus === 'requesting' ? '요청 중'
      : reviewStatus === 'reviewing' ? '검토 중'
        : reviewStatus === 'canceled' ? '검토 취소됨'
          : reviewStatus === 'error' ? '검토 오류' : '저장됨'

  return (
    <div className="feedback-panel">
      {/* 탭 헤더 */}
      <div className="panel-tabs" role="tablist" aria-label="학습 패널">
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === 'tasks'}
          className={`panel-tab ${activeTab === 'tasks' ? 'active' : ''}`}
          onClick={() => setTab('tasks')}
        >
          📋 과제
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === 'feedback'}
          className={`panel-tab ${activeTab === 'feedback' ? 'active' : ''}`}
          onClick={() => { setTab('feedback'); setFeedbackRead() }}
        >
          AI 피드백
          {feedbackUnread ? (
            <span className="tab-unread" aria-label="새 피드백 1개">1</span>
          ) : isStreaming ? (
            <span className="tab-dot" aria-label="AI 검토 중" />
          ) : null}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === 'chat'}
          className={`panel-tab ${activeTab === 'chat' ? 'active' : ''}`}
          onClick={() => setTab('chat')}
        >
          💬 채팅
          {isChatStreaming && <span className="tab-dot" />}
        </button>
        {lastSync && (
          <span className="panel-sync">{formatTime(lastSync)}</span>
        )}
        {snapshots.length > 0 && (
          <div className="snapshot-dropdown snapshot-dropdown-header">
            <button
              type="button"
              className="snapshot-btn"
              onClick={() => setShowSnapshotMenu(!showSnapshotMenu)}
              disabled={workspaceMutationLocked}
              aria-expanded={showSnapshotMenu}
            >
              {restoringStep ? '복원 중…' : '↩ 기록'}
            </button>
            {showSnapshotMenu && (
              <div className="snapshot-menu" role="menu" aria-label="이전 단계 스냅샷">
                {snapshots.map((snapshot) => (
                  <button
                    type="button"
                    role="menuitem"
                    key={snapshot}
                    className="snapshot-menu-item"
                    onClick={() => void handleRestoreSnapshot(snapshot)}
                    disabled={workspaceMutationLocked}
                  >
                    {snapshot}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      {projectStatus?.loaded && (
        <div className={`review-toolbar review-toolbar-${reviewStatus}`}>
          <div className="review-toolbar-copy" role="status" aria-live="polite">
            <div className="review-toolbar-status">
              <span className="review-state-dot" aria-hidden="true" />
              <strong>{reviewStatusLabel}</strong>
              <span>r{reviewRevision}</span>
            </div>
            <div className="review-toolbar-meta" title={reviewFiles.join(', ')}>
              <span>{reviewFileSummary}</span>
              <span aria-hidden="true">·</span>
              <span>{reviewTestLabel}</span>
            </div>
            {reviewError && <span className="review-toolbar-error">{reviewError}</span>}
          </div>
          <button
            type="button"
            className={`review-action-btn${reviewBusy ? ' reviewing' : ''}`}
            onClick={() => { void handleManualReview() }}
            disabled={reviewAction !== null || (!reviewBusy && !reviewCanStart)}
            aria-keyshortcuts="Control+Enter Meta+Enter"
            title="현재 저장된 코드에서 의미 있는 변경을 AI가 검토합니다. (⌘/Ctrl+Enter)"
          >
            {reviewAction
              ? reviewAction === 'cancel' ? '중단 중…' : '요청 중…'
              : reviewBusy ? '■ 검토 중단' : reviewCanStart ? '✦ AI 검토' : '변경 없음'}
          </button>
        </div>
      )}

      {projectComplete && (
        <div className="project-complete-banner" role="status">
          <div className="project-complete-icon">🎉</div>
          <div className="project-complete-text">
            <strong>모든 단계를 완료했습니다!</strong>
            <p>수고하셨습니다. 전체 커리큘럼을 성공적으로 마쳤습니다.</p>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              className="next-step-btn"
              onClick={resetWorkspace}
            >
              대시보드로 →
            </button>
            <button onClick={() => setProjectComplete(false)} className="dismiss-btn">닫기</button>
          </div>
        </div>
      )}

      {stepComplete && testResult?.passed && testResult.scope === 'full' && (
        <div className="step-complete-banner" role="status">
          <div className="step-complete-copy">
            <span>{pendingSemanticSync ? '저장된 변경을 확인하고 있습니다…' : finalStep ? '마지막 단계를 완료했습니다!' : '이 단계를 완료했습니다!'}</span>
            {advancing && !finalStep && !recoveringStep && (
              <small>다음 과제·코드·퀴즈를 만들고 있어요. 보통 1~2분 걸립니다.</small>
            )}
          </div>
          <div className="step-complete-actions">
            <button
              onClick={handleNextStep}
              disabled={advancing || pendingSemanticSync || workspaceMutationLocked}
              className="next-step-btn"
            >
              {advanceButtonLabel({
                pendingSemanticSync,
                advancing,
                recovering: recoveringStep,
                finalStep,
              })}
            </button>
            {advancing && !finalStep && !recoveringStep ? (
              <button onClick={() => advanceAbortRef.current?.abort()} className="dismiss-btn">생성 취소</button>
            ) : (
              <button onClick={() => setStepComplete(false)} className="dismiss-btn">닫기</button>
            )}
          </div>
        </div>
      )}

      {testResult && !testResult.passed && (
        <div className="step-incomplete-banner" role="status">
          <div className="test-failure-copy">
            <span>{testResult.scope === 'targeted' ? '개별 테스트가 실패했습니다.' : '전체 테스트가 실패했습니다.'}</span>
            <span className="test-summary" title={testResult.summary}>{testResult.summary}</span>
          </div>
          <button
            type="button"
            onClick={handleFailureReview}
            className="test-review-btn"
            disabled={reviewAction !== null || reviewBusy || pendingSemanticSync || !testResult.inputHash}
            title={testResult.inputHash ? '현재 코드와 일치하는 실패 결과를 AI에게 검토 요청합니다.' : '테스트를 다시 실행해 코드 버전을 확인하세요.'}
          >
            실패 원인 AI에 묻기
          </button>
        </div>
      )}

      {/* 과제 탭 */}
      {activeTab === 'tasks' && (
        <div className="feedback-content">
          {!projectStatus?.loaded ? (
            <div className="feedback-empty"><p>프로젝트를 먼저 로드하세요.</p></div>
          ) : (
            <div className="tasks-panel">
              {projectStatus.totalSteps > 0 && (
                <section className="tasks-section tasks-progress-section" aria-label="현재 학습 단계">
                  <span className="tasks-progress-label">
                    {projectStatus.totalSteps}단계 중 {projectStatus.currentStepNum}단계
                  </span>
                </section>
              )}

              {projectStatus.tasks && (
                <section className="tasks-section">
                  <h3 className="tasks-section-title">현재 과제</h3>
                  <div className="tasks-list">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>
                      {projectStatus.tasks}
                    </ReactMarkdown>
                  </div>
                </section>
              )}

              <section className="tasks-section">
                <h3 className="tasks-section-title">학습 목표</h3>
                <p className="tasks-goal">{projectStatus.goal || '—'}</p>
                <div className="tasks-meta">
                  <span className="tasks-badge lang">{projectStatus.language}</span>
                  <span className="tasks-badge step">{projectStatus.currentStep}</span>
                </div>
              </section>

              {projectStatus.concept && (
                <details className="tasks-section tasks-reference" key={`${projectStatus.dir}-${projectStatus.currentStep}`}>
                  <summary className="tasks-section-title">참고 개념</summary>
                  <div className="tasks-concept">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>
                      {projectStatus.concept}
                    </ReactMarkdown>
                  </div>
                </details>
              )}
            </div>
          )}
        </div>
      )}

      {/* AI 피드백 탭 */}
      {activeTab === 'feedback' && (
        <div className="feedback-content" ref={scrollRef} role="log" aria-live="polite" aria-label="AI 피드백">
          {feedbackMessages.length === 0 && !isStreaming && (
            <div className="feedback-empty">
              <p>코드는 자동으로 저장됩니다.</p>
              <p>검토가 필요할 때 <strong>AI 검토</strong>를 눌러 주세요.</p>
              <p className="feedback-empty-hint">단축키: ⌘/Ctrl+Enter</p>
            </div>
          )}

          {feedbackMessages.length >= 20 && (
            <p className="feedback-history-limit" role="note">최근 피드백 20개만 표시합니다.</p>
          )}

          {feedbackMessages.map((msg) => (
            <div key={msg.id} className="feedback-message">
              <div className="feedback-message-time">
                <span>{formatTime(msg.timestamp)}</span>
                {msg.revision !== undefined && <span>r{msg.revision}</span>}
                {msg.files && msg.files.length > 0 && (
                  <span title={msg.files.join(', ')}>
                    {msg.files[0]}{msg.files.length > 1 ? ` 외 ${msg.files.length - 1}개` : ''}
                  </span>
                )}
                {msg.testStatus && (
                  <span>
                    {msg.testStatus === 'passed' ? '테스트 통과' : msg.testStatus === 'failed' ? '테스트 실패' : '테스트 미실행'}
                  </span>
                )}
              </div>
              <div className="feedback-message-content">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {msg.content.replace('[STEP_COMPLETE]', '')}
                </ReactMarkdown>
              </div>
            </div>
          ))}

          {isStreaming && (
            <div className="feedback-message streaming">
              <div className="feedback-message-time">
                <span className="streaming-dot" /> 분석 중 · r{reviewRevision} · {reviewFileSummary}
              </div>
              <div className="feedback-message-content">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {currentStreaming}
                </ReactMarkdown>
              </div>
            </div>
          )}
        </div>
      )}

      {showConfetti && <Confetti onDone={() => setShowConfetti(false)} />}

      {/* 채팅 탭 */}
      {activeTab === 'chat' && (
        <div className="chat-panel">
          <div className="chat-messages" ref={chatScrollRef} role="log" aria-live="polite" aria-label="AI 튜터 채팅">
            {chatMessages.length === 0 && !isChatStreaming && (
              <div className="feedback-empty">
                <p>AI 튜터에게 질문하세요.</p>
                <p className="feedback-empty-hint">현재 열린 파일을 기반으로 답변합니다.</p>
              </div>
            )}

            {chatMessages.map((msg, i) => (
              <div key={i} className={`chat-msg chat-msg-${msg.role}`}>
                <div className="feedback-message-content">
                  {msg.role === 'ai' ? (
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.content}</ReactMarkdown>
                  ) : (
                    msg.content
                  )}
                </div>
              </div>
            ))}

            {isChatStreaming && (
              <div className="chat-msg chat-msg-ai">
                <div className="feedback-message-content">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{currentChatStreaming}</ReactMarkdown>
                  <span className="streaming-dot" />
                </div>
              </div>
            )}
          </div>

          <div className="chat-input-area">
            <textarea
              className="chat-textarea"
              value={chatInput}
              onChange={(e) => setChatInput(e.target.value)}
              onKeyDown={handleChatKeyDown}
              placeholder="질문을 입력하세요… (Enter 전송, Shift+Enter 줄바꿈)"
              aria-label="AI 튜터에게 보낼 질문"
              rows={3}
              disabled={isChatStreaming}
            />
            <button
              className="chat-send-btn"
              onClick={handleSendChat}
              disabled={isChatStreaming || !chatInput.trim()}
            >
              {isChatStreaming ? '…' : '전송'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
