import { useState, useEffect, useRef } from 'react'
import type { KeyboardEvent } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useStore } from '../../store'
import type { ProjectStatus } from '../../store'
import { useProject } from '../../hooks/useProject'
import { getErrorMessage, isAbortError } from '../../lib/errors'
import Confetti from '../Confetti'
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
    setTestResult,
    snapshots,
    setSnapshots,
    addToast,
  } = useStore()
  const { advanceToNextStep, completeMission, refreshStatus, refreshFileTree, reloadOpenTabs, loadQuizData, sendChat, listSnapshots, restoreSnapshot } = useProject()

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
    if (advanceAbortRef.current) return
    const abort = new AbortController()
    advanceAbortRef.current = abort
    setAdvancing(true)
    try {
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
    }
  }

  async function handleRestoreSnapshot(step: string) {
    if (restoreAbortRef.current) return
    const confirmed = window.confirm(
      '이 스냅샷으로 복원하면 현재 소스 변경과 스냅샷 이후 생성된 파일이 되돌아갑니다. 복구가 어려울 수 있습니다. 계속할까요?',
    )
    if (!confirmed) return

    const abort = new AbortController()
    restoreAbortRef.current = abort
    setRestoringStep(step)
    setShowSnapshotMenu(false)
    try {
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
    cancelChatStream()
  }, [cancelChatStream])

  // 스트리밍 중에는 피드백을 즉시 보여주되 사용자가 고른 탭은 보존합니다.
  const activeTab: Tab = isStreaming ? 'feedback' : tab

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
    if (stepComplete && testResult?.passed && !prevStepComplete.current) {
      setShowConfetti(true)
    }
    prevStepComplete.current = !!(stepComplete && testResult?.passed)
  }, [stepComplete, testResult?.passed])

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
          onClick={() => setTab('feedback')}
        >
          AI 피드백
          {isStreaming && <span className="tab-dot" />}
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
              disabled={!!restoringStep}
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
                  >
                    {snapshot}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

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

      {stepComplete && testResult?.passed && (
        <div className="step-complete-banner" role="status">
          <span>이 단계를 완료했습니다!</span>
          <div className="step-complete-actions">
            <button
              onClick={handleNextStep}
              disabled={advancing}
              className="next-step-btn"
            >
              {advancing
                ? '처리 중…'
                : pendingStepStatus || pendingCompletion
                  ? '단계 상태 다시 불러오기'
                  : '다음 단계로 →'}
            </button>
            <button onClick={() => setStepComplete(false)} className="dismiss-btn">닫기</button>
          </div>
        </div>
      )}

      {stepComplete && !testResult?.passed && (
        <div className="step-incomplete-banner" role="status">
          <span>테스트를 통과해야 다음 단계로 넘어갈 수 있어요.</span>
          {testResult && <span className="test-summary">{testResult.summary}</span>}
          <button onClick={() => setStepComplete(false)} className="dismiss-btn">닫기</button>
        </div>
      )}

      {/* 과제 탭 */}
      {activeTab === 'tasks' && (
        <div className="feedback-content">
          {!projectStatus?.loaded ? (
            <div className="feedback-empty"><p>프로젝트를 먼저 로드하세요.</p></div>
          ) : (
            <div className="tasks-panel">
              {/* 진행률 */}
              {projectStatus.totalSteps > 0 && (
                <section className="tasks-section tasks-progress-section">
                  <div className="tasks-progress-header">
                    <span className="tasks-progress-label">
                      {projectStatus.currentStepNum} / {projectStatus.totalSteps} 단계
                    </span>
                    <span className="tasks-progress-pct">
                      {Math.round((projectStatus.currentStepNum / projectStatus.totalSteps) * 100)}%
                    </span>
                  </div>
                  <div className="tasks-progress-bar">
                    <div
                      className="tasks-progress-fill"
                      style={{ width: `${(projectStatus.currentStepNum / projectStatus.totalSteps) * 100}%` }}
                    />
                  </div>
                </section>
              )}

              {/* 목표 */}
              <section className="tasks-section">
                <h3 className="tasks-section-title">🎯 학습 목표</h3>
                <p className="tasks-goal">{projectStatus.goal || '—'}</p>
                <div className="tasks-meta">
                  <span className="tasks-badge lang">{projectStatus.language}</span>
                  <span className="tasks-badge step">{projectStatus.currentStep}</span>
                </div>
              </section>

              {/* 개념 설명 */}
              {projectStatus.concept && (
                <section className="tasks-section">
                  <h3 className="tasks-section-title">📖 개념 설명</h3>
                  <div className="tasks-concept">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>
                      {projectStatus.concept}
                    </ReactMarkdown>
                  </div>
                </section>
              )}

              {/* 현재 과제 */}
              {projectStatus.tasks && (
                <section className="tasks-section">
                  <h3 className="tasks-section-title">✏️ 현재 과제</h3>
                  <div className="tasks-list">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>
                      {projectStatus.tasks}
                    </ReactMarkdown>
                  </div>
                </section>
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
              <p>파일을 수정하면</p>
              <p>AI가 자동으로 피드백을 제공합니다.</p>
              <p className="feedback-empty-hint">(수정 후 3초 대기)</p>
            </div>
          )}

          {feedbackMessages.map((msg) => (
            <div key={msg.id} className="feedback-message">
              <div className="feedback-message-time">{formatTime(msg.timestamp)}</div>
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
                <span className="streaming-dot" /> 분석 중...
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
