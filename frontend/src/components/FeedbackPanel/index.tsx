import { useState, useEffect, useRef } from 'react'
import type { KeyboardEvent } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useStore } from '../../store'
import { useProject } from '../../hooks/useProject'
import './FeedbackPanel.css'

type Tab = 'feedback' | 'tasks' | 'chat'

export default function FeedbackPanel() {
  const [tab, setTab] = useState<Tab>('tasks')
  const [advancing, setAdvancing] = useState(false)
  const [chatInput, setChatInput] = useState('')
  const [showSnapshotMenu, setShowSnapshotMenu] = useState(false)
  const [restoringStep, setRestoringStep] = useState<string | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const chatScrollRef = useRef<HTMLDivElement>(null)
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
    clearProjectStatus,
    skillLevel,
    setQuizData,
    clearSolvedHoles,
    openFileContent,
    chatMessages,
    isChatStreaming,
    currentChatStreaming,
    addUserChatMessage,
    startChatStream,
    addChatChunk,
    endChatStream,
    testResult,
    snapshots,
    setSnapshots,
  } = useStore()
  const { advanceToNextStep, completeMission, refreshFileTree, loadQuizData, sendChat, listSnapshots, restoreSnapshot } = useProject()

  async function handleNextStep() {
    setAdvancing(true)
    try {
      const data = await advanceToNextStep()
      if (data.done) {
        setStepComplete(false)
        await completeMission()
        setProjectComplete(true)
        return
      }
      if (data.loaded) {
        setProjectStatus(data)
        clearSolvedHoles()
        await refreshFileTree(data.dir)
        if (skillLevel === 'newbie') {
          const quiz = await loadQuizData()
          setQuizData(quiz)
        }
        // 스냅샷 목록 갱신
        const snaps = await listSnapshots()
        setSnapshots(snaps)
      }
      setStepComplete(false)
    } catch (e) {
      console.error('Next step error:', e)
    } finally {
      setAdvancing(false)
    }
  }

  async function handleRestoreSnapshot(step: string) {
    setRestoringStep(step)
    setShowSnapshotMenu(false)
    try {
      await restoreSnapshot(step)
      // 파일 트리 갱신
      if (projectStatus?.dir) {
        await refreshFileTree(projectStatus.dir)
      }
    } catch (e) {
      console.error('Restore snapshot error:', e)
    } finally {
      setRestoringStep(null)
    }
  }

  async function handleSendChat() {
    const msg = chatInput.trim()
    if (!msg || isChatStreaming) return

    setChatInput('')
    addUserChatMessage(msg)
    startChatStream()

    const stream = await sendChat(msg, openFileContent, chatMessages)
    if (!stream) {
      endChatStream()
      return
    }

    const reader = stream.getReader()
    const decoder = new TextDecoder()
    let buf = ''

    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop() ?? ''
        for (const line of lines) {
          if (line.startsWith('data: ')) {
            const raw = line.slice(6)
            try {
              const parsed = JSON.parse(raw)
              if (parsed.text) addChatChunk(parsed.text)
            } catch { /* skip */ }
          }
        }
      }
    } finally {
      endChatStream()
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

  // 스트리밍 시작 시 피드백 탭으로 자동 전환
  if (isStreaming && tab !== 'feedback') setTab('feedback')

  // 새 피드백 or 스트리밍 시작 시 맨 아래로 스크롤
  useEffect(() => {
    if (tab === 'feedback' && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [feedbackMessages.length, isStreaming, tab])

  // 채팅 메시지 추가 시 맨 아래로 스크롤
  useEffect(() => {
    if (tab === 'chat' && chatScrollRef.current) {
      chatScrollRef.current.scrollTop = chatScrollRef.current.scrollHeight
    }
  }, [chatMessages.length, isChatStreaming, tab])

  return (
    <div className="feedback-panel">
      {/* 탭 헤더 */}
      <div className="panel-tabs">
        <button
          className={`panel-tab ${tab === 'tasks' ? 'active' : ''}`}
          onClick={() => setTab('tasks')}
        >
          📋 과제
        </button>
        <button
          className={`panel-tab ${tab === 'feedback' ? 'active' : ''}`}
          onClick={() => setTab('feedback')}
        >
          AI 피드백
          {isStreaming && <span className="tab-dot" />}
        </button>
        <button
          className={`panel-tab ${tab === 'chat' ? 'active' : ''}`}
          onClick={() => setTab('chat')}
        >
          💬 채팅
          {isChatStreaming && <span className="tab-dot" />}
        </button>
        {lastSync && (
          <span className="panel-sync">{formatTime(lastSync)}</span>
        )}
      </div>

      {projectComplete && (
        <div className="project-complete-banner">
          <div className="project-complete-icon">🎉</div>
          <div className="project-complete-text">
            <strong>모든 단계를 완료했습니다!</strong>
            <p>수고하셨습니다. 전체 커리큘럼을 성공적으로 마쳤습니다.</p>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              className="next-step-btn"
              onClick={() => { setProjectComplete(false); clearProjectStatus() }}
            >
              대시보드로 →
            </button>
            <button onClick={() => setProjectComplete(false)} className="dismiss-btn">닫기</button>
          </div>
        </div>
      )}

      {stepComplete && testResult?.passed && (
        <div className="step-complete-banner">
          <span>이 단계를 완료했습니다!</span>
          <div className="step-complete-actions">
            <button
              onClick={handleNextStep}
              disabled={advancing}
              className="next-step-btn"
            >
              {advancing ? 'AI가 다음 단계 생성 중...' : '다음 단계로 →'}
            </button>
            {snapshots.length > 0 && (
              <div className="snapshot-dropdown">
                <button
                  className="snapshot-btn"
                  onClick={() => setShowSnapshotMenu(!showSnapshotMenu)}
                  disabled={!!restoringStep}
                >
                  {restoringStep ? '복원 중...' : '이전 단계 복원 ↩'}
                </button>
                {showSnapshotMenu && (
                  <div className="snapshot-menu">
                    {snapshots.map((s) => (
                      <button
                        key={s}
                        className="snapshot-menu-item"
                        onClick={() => handleRestoreSnapshot(s)}
                      >
                        {s}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
            <button onClick={() => setStepComplete(false)} className="dismiss-btn">닫기</button>
          </div>
        </div>
      )}

      {stepComplete && !testResult?.passed && (
        <div className="step-incomplete-banner">
          <span>테스트를 통과해야 다음 단계로 넘어갈 수 있어요.</span>
          {testResult && <span className="test-summary">{testResult.summary}</span>}
          <button onClick={() => setStepComplete(false)} className="dismiss-btn">닫기</button>
        </div>
      )}

      {/* 과제 탭 */}
      {tab === 'tasks' && (
        <div className="feedback-content">
          {!projectStatus?.loaded ? (
            <div className="feedback-empty"><p>프로젝트를 먼저 로드하세요.</p></div>
          ) : (
            <div className="tasks-panel">
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
      {tab === 'feedback' && (
        <div className="feedback-content" ref={scrollRef}>
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

      {/* 채팅 탭 */}
      {tab === 'chat' && (
        <div className="chat-panel">
          <div className="chat-messages" ref={chatScrollRef}>
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
