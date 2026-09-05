import React, { useEffect, useMemo, useRef, useState } from 'react'
import { MissionFinalizePendingError, useProject } from '../../hooks/useProject'
import type { PendingMissionFinalize, TopicSuggestion } from '../../hooks/useProject'
import { useStore } from '../../store'
import { getErrorMessage, isAbortError } from '../../lib/errors'
import { readPreference, writePreference } from '../../lib/storage'
import { NurseSseParser } from './nurseSse'
import { rememberSuggestedTopics } from './topicDiversity'
import {
  MIN_EDITOR_WIDTH,
  clearPendingMissionFinalize,
  dailyIntroStorageKey,
  describeMissionGeneration,
  isEditorWidthReady,
  readPendingMissionFinalize,
  resumableMissions,
  writePendingMissionFinalize,
} from './missionUx'
import './Dashboard.css'

interface NurseChatMsg {
  role: 'user' | 'nurse'
  content: string
}

interface Props {
  onMissionReady: (projectDir: string, skillLevel: string) => Promise<void>
  onOpenSettings: () => void
}

interface MissionRecord {
  id: string
  date: string
  topic: string
  slug: string
  project_dir: string
  status: string
}

function friendlyError(msg: string): string {
  if (msg.toLowerCase().includes('user settings not found')) return '설정 정보가 없습니다.'
  return msg
}

function isSettingsError(msg: string): boolean {
  return msg.toLowerCase().includes('settings') || msg.toLowerCase().includes('설정')
}

function getDaysInMonth(year: number, month: number) {
  return new Date(year, month + 1, 0).getDate()
}

function getFirstDayOfWeek(year: number, month: number) {
  return new Date(year, month, 1).getDay()
}

function readLastAccessDate(): string | null {
  try {
    return window.localStorage.getItem('lastAccessDate')
  } catch {
    return null
  }
}

function missionFinalizeStorageKey(userID: string): string {
  return `coding-tutor.pending-mission-finalize.${userID}`
}

export default function DashboardScreen({ onMissionReady, onOpenSettings }: Props) {
  const {
    getDailyMission,
    getDailyHistory,
    confirmDailyMissionStream,
    retryDailyMissionFinalize,
    loadProject,
    deleteProject,
    nurseChat,
  } = useProject()
  const { userSettings, addToast } = useStore()
  const finalizeStorageKey = missionFinalizeStorageKey(userSettings?.user_id ?? 'unknown')
  const introStorageKey = dailyIntroStorageKey(userSettings?.user_id ?? 'unknown')

  const today = new Date()
  const todayStr = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`

  // 테스트 모드 확인
  const isTestMode = new URLSearchParams(window.location.search).has('test')
  const testScenarios: Array<'greeting' | 'same_day_failed' | 'yesterday_failed' | 'streak_failed'> = [
    'greeting',
    'same_day_failed',
    'yesterday_failed',
    'streak_failed',
  ]

  const [todayMissions, setTodayMissions] = useState<MissionRecord[]>([])
  const [allHistory, setAllHistory] = useState<MissionRecord[]>([])
  const activeMissions = useMemo(() => resumableMissions(allHistory, todayMissions), [allHistory, todayMissions])
  const latestMission = activeMissions[0]
  const [loading, setLoading] = useState(true)
  const [loadingMissionId, setLoadingMissionId] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [selectedDate, setSelectedDate] = useState<string>(todayStr)
  const [currentMonth, setCurrentMonth] = useState({ year: today.getFullYear(), month: today.getMonth() })
  const [vnVisible, setVnVisible] = useState(false)
  const [vnFading, setVnFading] = useState(false)
  const [vnStep, setVnStep] = useState(0)
  const [vnScenario, setVnScenario] = useState<'greeting' | 'same_day_failed' | 'yesterday_failed' | 'streak_failed'>('greeting')
  const [testScenarioIndex, setTestScenarioIndex] = useState(0)
  const [testModeActive, setTestModeActive] = useState(false)
  const [creatingProgress, setCreatingProgress] = useState<{stage: string; message: string} | null>(null)
  const [finalizeRetrying, setFinalizeRetrying] = useState(false)
  const [pendingMissionFinalize, setPendingMissionFinalize] = useState<PendingMissionFinalize | null>(() =>
    readPendingMissionFinalize(window.localStorage, finalizeStorageKey),
  )
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth)
  const [pendingNarrowTopic, setPendingNarrowTopic] = useState<TopicSuggestion | null>(null)

  // Nurse chat state
  const [nurseChatVisible, setNurseChatVisible] = useState(false)
  const [nurseChatFading, setNurseChatFading] = useState(false)
  const [nurseChatHistory, setNurseChatHistory] = useState<NurseChatMsg[]>([])
  const [nurseChatInput, setNurseChatInput] = useState('')
  const [nurseChatLoading, setNurseChatLoading] = useState(false)
  const [nurseRequestNotice, setNurseRequestNotice] = useState('')
  const [nurseChatSuggestedTopics, setNurseChatSuggestedTopics] = useState<TopicSuggestion[]>([])
  const [nurseChatMode, setNurseChatMode] = useState<'chat' | 'choice' | 'mission-select'>('chat')
  const [nurseMissionSelIdx, setNurseMissionSelIdx] = useState(0)
  const [pastTopics, setPastTopics] = useState<string[]>([])
  const nurseChatBottomRef = useRef<HTMLDivElement>(null)
  const nurseChatInputRef = useRef<HTMLInputElement>(null)
  const nurseDialogRef = useRef<HTMLDivElement>(null)
  const nurseAbortRef = useRef<AbortController | null>(null)
  const missionLoadRef = useRef<AbortController | null>(null)
  const missionCreationRef = useRef<AbortController | null>(null)
  const scheduledTimeoutsRef = useRef<Set<number>>(new Set())
  const mountedRef = useRef(true)
  const startupRef = useRef({ isTestMode, testScenarios, getDailyMission, getDailyHistory, today, todayStr, introStorageKey })
  const selectMissionRef = useRef<(mission: MissionRecord) => void>(() => undefined)
  const sendNurseMessageRef = useRef<(message: string, history: NurseChatMsg[]) => void>(() => undefined)

  function schedule(callback: () => void, delay: number) {
    const id = window.setTimeout(() => {
      scheduledTimeoutsRef.current.delete(id)
      callback()
    }, delay)
    scheduledTimeoutsRef.current.add(id)
  }

  // VN 시나리오별 대사/이미지 정의
  const vnSequence = {
    greeting: [
      { img: '/greeting.webp', text: '어서 오세요! 오늘도 열심히 해봐요. 응원하고 있을게요~' },
    ],
    same_day_failed: [
      { img: '/same_day_failed.webp', text: '어? 아직 오늘의 미션을 완료하지 않으셨잖아요! 마저 해야 합니다!' },
    ],
    yesterday_failed: [
      { img: '/punishment.webp', text: '어제 훈련을 빠지셨네요... 꾸준함이 재활의 핵심입니다!' },
      { img: '/punishment.webp', text: '오늘은 꼭 완료하셔야 해요. 화이팅! 💪' },
    ],
    streak_failed: [
      { img: '/streak_failed.webp', text: '어제 흐름이 끊겼네요. 오늘은 가볍게 다시 시작해봐요.' },
      { img: '/streak_failed.webp', text: '작은 단계 하나부터 다시 이어가면 됩니다.' },
    ],
  }

  const currentSlides = vnSequence[vnScenario]
  const currentSlide = currentSlides[vnStep]

  function showMissionMenu() {
    nurseAbortRef.current?.abort()
    nurseAbortRef.current = null
    setNurseChatLoading(false)
    setNurseRequestNotice('')
    setNurseChatFading(false)
    setNurseChatVisible(true)
    setNurseChatMode('choice')
    setNurseMissionSelIdx(0)
    setNurseChatSuggestedTopics([])
    setNurseChatHistory([{
      role: 'nurse',
      content: activeMissions.length > 0
        ? `진행 중인 미션이 ${activeMissions.length}개 있어요. 이어서 진행하거나 AI에게 새 주제를 추천받을 수 있어요.`
        : '진행 중인 미션이 없어요. 준비되면 AI에게 새 주제를 추천받아 보세요.',
    }])
  }

  function requestNurseRecommendations() {
    if (nurseChatLoading) return
    setNurseChatMode('chat')
    setNurseChatHistory([])
    setNurseChatSuggestedTopics([])
    setNurseRequestNotice('AI 추천 1회를 요청했습니다.')
    void sendNurseMessage('__init__', [])
  }

  function finishVnIntro() {
    if (!testModeActive) writePreference(introStorageKey, todayStr)
    setVnFading(true)
    schedule(() => {
      setVnVisible(false)
      setVnFading(false)
    }, 420)
  }

  function advanceVn() {
    if (vnFading) return
    if (vnStep < currentSlides.length - 1) {
      setVnStep(prev => prev + 1)
    } else if (testModeActive) {
      // 테스트 모드: 다음 시나리오로 넘어감
      if (testScenarioIndex < testScenarios.length - 1) {
        const nextScenario = testScenarios[testScenarioIndex + 1]
        setVnScenario(nextScenario)
        setVnStep(0)
        setTestScenarioIndex(prev => prev + 1)
      } else {
        finishVnIntro()
      }
    } else {
      finishVnIntro()
    }
  }

  async function sendNurseMessage(userMsg: string, currentHistory: NurseChatMsg[]) {
    nurseAbortRef.current?.abort()
    const abort = new AbortController()
    nurseAbortRef.current = abort
    setNurseChatLoading(true)
    if (userMsg !== '__init__') setNurseRequestNotice('AI 응답 1회를 요청했습니다.')
    const isInit = userMsg === '__init__'
    const histForApi = isInit ? [] : currentHistory
    const newHistory: NurseChatMsg[] = isInit
      ? []
      : [...currentHistory, { role: 'user' as const, content: userMsg }]

    try {
      const stream = await nurseChat(
        isInit ? '오늘 연습할 새 주제 3개를 추천해 주세요.' : userMsg,
        histForApi,
        pastTopics,
        abort.signal,
        isInit,
      )
      let nurseReply = ''
      setNurseChatHistory([...newHistory, { role: 'nurse', content: '' }])

      const reader = stream.getReader()
      const decoder = new TextDecoder()
      const parser = new NurseSseParser()
      let streamComplete = false

      const consumeEvents = (events: ReturnType<NurseSseParser['push']>) => {
        for (const event of events) {
          if (event.type === 'message') {
            nurseReply += event.text
            setNurseChatHistory([...newHistory, { role: 'nurse', content: nurseReply }])
          } else if (event.type === 'topics') {
            setNurseChatSuggestedTopics(event.topics)
            setPastTopics((current) => rememberSuggestedTopics(current, event.topics))
          } else if (event.type === 'done') {
            streamComplete = true
          }
        }
      }

      try {
        while (!streamComplete) {
          const { done, value } = await reader.read()
          if (done) {
            consumeEvents(parser.push(decoder.decode()))
            consumeEvents(parser.finish())
            break
          }
          consumeEvents(parser.push(decoder.decode(value, { stream: true })))
        }
        if (streamComplete) await reader.cancel().catch(() => undefined)
      } finally {
        reader.releaseLock()
      }

      if (!streamComplete) throw new Error('간호사 응답이 완료되기 전에 연결이 종료되었습니다.')
      setNurseChatHistory([...newHistory, { role: 'nurse', content: nurseReply }])
      setNurseRequestNotice('AI 응답을 받았습니다.')
    } catch (error: unknown) {
      if (!abort.signal.aborted) {
        const message = getErrorMessage(error)
        setNurseChatHistory([...newHistory, { role: 'nurse', content: `응답을 불러오지 못했습니다: ${message}` }])
        setError(message)
      }
    } finally {
      if (nurseAbortRef.current === abort) {
        nurseAbortRef.current = null
        setNurseChatLoading(false)
        schedule(() => {
          nurseChatBottomRef.current?.scrollIntoView({ behavior: 'smooth' })
          nurseChatInputRef.current?.focus()
        }, 50)
      }
    }
  }

  function handleNurseChatSubmit() {
    const msg = nurseChatInput.trim()
    if (!msg || nurseChatLoading) return
    setNurseChatInput('')
    setNurseChatSuggestedTopics([])
    void sendNurseMessage(msg, nurseChatHistory)
  }

  function cancelNurseRequest() {
    const abort = nurseAbortRef.current
    if (!abort) return
    abort.abort()
    nurseAbortRef.current = null
    setNurseChatLoading(false)
    setNurseRequestNotice('AI 요청을 취소했습니다. 입력을 바꿔 다시 요청할 수 있습니다.')
    setNurseChatHistory(prev => {
      const next = [...prev]
      if (next.at(-1)?.role === 'nurse' && !next.at(-1)?.content.trim()) next.pop()
      return next
    })
  }

  function closeNurseChat() {
    nurseAbortRef.current?.abort()
    nurseAbortRef.current = null
    setNurseChatLoading(false)
    setNurseRequestNotice('')
    setNurseChatFading(true)
    schedule(() => {
      setNurseChatVisible(false)
      setNurseChatFading(false)
      setNurseChatHistory([])
      setNurseChatSuggestedTopics([])
      setNurseChatMode('chat')
      setNurseMissionSelIdx(0)
    }, 300)
  }

  function confirmNurseTopic(t: TopicSuggestion) {
    if (!isEditorWidthReady(viewportWidth)) {
      setPendingNarrowTopic(t)
      return
    }
    setPendingNarrowTopic(null)
    closeNurseChat()
    schedule(() => void handleCreateNewMission(t.name, t.slug), 350)
  }

  const mainRef = useRef<HTMLDivElement>(null)
  const calendarRef = useRef<HTMLDivElement>(null)
  const initializerRef = useRef(false)
  selectMissionRef.current = (mission) => {
    closeNurseChat()
    schedule(() => void handleLoadMission(mission), 350)
  }
  sendNurseMessageRef.current = (message, history) => {
    void sendNurseMessage(message, history)
  }

  // 키보드: mission-select 모드
  useEffect(() => {
    if (!nurseChatVisible || nurseChatMode !== 'mission-select') return
    function handler(e: KeyboardEvent) {
      if (e.isComposing || (e.target instanceof HTMLElement && e.target.closest('input, textarea'))) return
      const focusedButton = e.target instanceof HTMLElement ? e.target.closest('button') : null
      if (e.key === 'Enter' && focusedButton) return
      if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
        if (focusedButton && !focusedButton.hasAttribute('data-mission-index')) return
        e.preventDefault()
        const next = Math.max(0, Math.min(activeMissions.length - 1, nurseMissionSelIdx + (e.key === 'ArrowDown' ? 1 : -1)))
        setNurseMissionSelIdx(next)
        nurseDialogRef.current?.querySelector<HTMLButtonElement>(`[data-mission-index="${next}"]`)?.focus()
      } else if (e.key === 'Enter') {
        e.preventDefault()
        const m = activeMissions[nurseMissionSelIdx]
        if (m) selectMissionRef.current(m)
      } else if (e.key === 'Escape') {
        setNurseChatMode('choice')
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [nurseChatVisible, nurseChatMode, nurseMissionSelIdx, activeMissions])

  // 키보드: choice 모드 (1=기존, 2=새로운)
  useEffect(() => {
    if (!nurseChatVisible || nurseChatMode !== 'choice') return
    function handler(e: KeyboardEvent) {
      if (e.isComposing || (e.target instanceof HTMLElement && e.target.closest('input, textarea'))) return
      if (e.key === '1') {
        if (activeMissions.length > 0) {
          setNurseChatMode('mission-select')
          setNurseMissionSelIdx(0)
        } else {
          sendNurseMessageRef.current('__init__', [])
          setNurseChatMode('chat')
          setNurseChatHistory([])
          setNurseRequestNotice('AI 추천 1회를 요청했습니다.')
        }
      } else if (e.key === '2' && activeMissions.length > 0) {
        sendNurseMessageRef.current('__init__', [])
        setNurseChatMode('chat')
        setNurseChatHistory([])
        setNurseRequestNotice('AI 추천 1회를 요청했습니다.')
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [nurseChatVisible, nurseChatMode, activeMissions.length])

  useEffect(() => {
    function handleResize() {
      setViewportWidth(window.innerWidth)
    }
    window.addEventListener('resize', handleResize, { passive: true })
    return () => window.removeEventListener('resize', handleResize)
  }, [])

  useEffect(() => {
    if (!nurseChatVisible || nurseChatFading) return
    const frame = window.requestAnimationFrame(() => {
      const dialog = nurseDialogRef.current
      const target = dialog?.querySelector<HTMLButtonElement>('.nurse-chat-topic-btn')
        ?? dialog?.querySelector<HTMLButtonElement>('.nurse-chat-cancel')
        ?? dialog?.querySelector<HTMLInputElement>('.nurse-chat-input')
        ?? dialog?.querySelector<HTMLButtonElement>('.nurse-chat-skip')
      target?.focus()
    })
    return () => window.cancelAnimationFrame(frame)
  }, [nurseChatFading, nurseChatLoading, nurseChatMode, nurseChatVisible])

  useEffect(() => {
    mountedRef.current = true
    const scheduledTimeouts = scheduledTimeoutsRef.current
    return () => {
      mountedRef.current = false
      const nurseAbort = nurseAbortRef.current
      const missionLoadAbort = missionLoadRef.current
      const missionCreationAbort = missionCreationRef.current
      nurseAbortRef.current = null
      missionLoadRef.current = null
      missionCreationRef.current = null
      nurseAbort?.abort()
      missionLoadAbort?.abort()
      missionCreationAbort?.abort()
      scheduledTimeouts.forEach(id => window.clearTimeout(id))
      scheduledTimeouts.clear()
    }
  }, [])

  useEffect(() => {
    if (initializerRef.current) return
    initializerRef.current = true
    const {
      isTestMode: initialTestMode,
      testScenarios: initialTestScenarios,
      getDailyMission: fetchDailyMission,
      getDailyHistory: fetchDailyHistory,
      today: initialToday,
      todayStr: initialTodayStr,
      introStorageKey: initialIntroStorageKey,
    } = startupRef.current

    // 테스트 모드: 바로 첫 시나리오 표시
    if (initialTestMode) {
      setTestModeActive(true)
      setVnFading(false)
      setVnScenario(initialTestScenarios[0])
      setVnStep(0)
      setVnVisible(true)
      setTestScenarioIndex(0)
      setLoading(false)
      return
    }

    Promise.all([
      fetchDailyMission(),
      fetchDailyHistory(),
    ])
      .then(([daily, hist]) => {
        if (!mountedRef.current) return
        const missions: MissionRecord[] = daily.missions || []
        setTodayMissions(missions)
        if (daily.error) setError(daily.error)
        const history: MissionRecord[] = Array.isArray(hist) ? hist : []
        setAllHistory(history)
        const seen = new Set<string>()
        const past: string[] = []
        for (const m of history) {
          if (!seen.has(m.topic)) { seen.add(m.topic); past.push(m.topic) }
        }
        setPastTopics(past)

        if (readPreference(initialIntroStorageKey) === initialTodayStr) {
          setVnVisible(false)
          writePreference('lastAccessDate', initialTodayStr)
          return
        }

        // 마지막 접속 날짜 확인
        const lastAccessDate = readLastAccessDate()
        const isSameDayAccess = lastAccessDate === initialTodayStr

        // 어제, 그저께 날짜 계산
        const yesterday = new Date(initialToday)
        yesterday.setDate(yesterday.getDate() - 1)
        const yesterdayStr = `${yesterday.getFullYear()}-${String(yesterday.getMonth() + 1).padStart(2, '0')}-${String(yesterday.getDate()).padStart(2, '0')}`

        const dayBeforeYesterday = new Date(initialToday)
        dayBeforeYesterday.setDate(dayBeforeYesterday.getDate() - 2)
        const dayBeforeYesterdayStr = `${dayBeforeYesterday.getFullYear()}-${String(dayBeforeYesterday.getMonth() + 1).padStart(2, '0')}-${String(dayBeforeYesterday.getDate()).padStart(2, '0')}`

        // 어제/그저께 완료 여부 확인 (status='completed'인 경우만 완료로 판단)
        const yesterdayMissions = history.filter(m => m.date === yesterdayStr)
        const dayBeforeYesterdayMissions = history.filter(m => m.date === dayBeforeYesterdayStr)
        const yesterdayCompleted = yesterdayMissions.some(m => m.status === 'completed')
        const dayBeforeYesterdayCompleted = dayBeforeYesterdayMissions.some(m => m.status === 'completed')

        // 오늘 미션 상태 확인
        const todayHasActive = missions.some(m => m.status === 'active')

        // 시나리오 결정
        let scenario: 'greeting' | 'same_day_failed' | 'yesterday_failed' | 'streak_failed'

        if (isSameDayAccess && todayHasActive) {
          // 같은 날 재접속했는데 미션 미완료
          scenario = 'same_day_failed'
        } else if (!isSameDayAccess && !yesterdayCompleted && !dayBeforeYesterdayCompleted) {
          // 새 날 접속하고 어제, 그저께 모두 미완료 (2일 이상 연속)
          scenario = 'streak_failed'
        } else if (!isSameDayAccess && !yesterdayCompleted) {
          // 새 날 접속하고 어제 미완료
          scenario = 'yesterday_failed'
        } else {
          // 그 외: 정상 인사
          scenario = 'greeting'
        }

        setVnFading(false)
        setVnScenario(scenario)
        setVnStep(0)
        setVnVisible(true)

        // 현재 접속 날짜 저장
        writePreference('lastAccessDate', initialTodayStr)
      })
      .catch((error: unknown) => {
        if (!mountedRef.current) return
        setVnVisible(false)
        setError(getErrorMessage(error))
      })
      .finally(() => {
        if (mountedRef.current) setLoading(false)
      })
  }, [])

  // 날짜별 미션 맵
  const missionsByDate = new Map<string, MissionRecord[]>()
  allHistory.forEach((m) => {
    const list = missionsByDate.get(m.date) || []
    if (!list.find((x) => x.id === m.id)) list.push(m)
    missionsByDate.set(m.date, list)
  })
  todayMissions.forEach((m) => {
    const list = missionsByDate.get(m.date) || []
    if (!list.find((x) => x.id === m.id)) list.push(m)
    missionsByDate.set(m.date, list)
  })

  const selectedMissions = missionsByDate.get(selectedDate) || []

  function handleDateClick(dateStr: string) {
    setSelectedDate(dateStr)
  }

  async function handleDeleteMission(mission: MissionRecord) {
    try {
      await deleteProject(mission.project_dir)
      setAllHistory(prev => prev.filter(m => m.id !== mission.id))
      setTodayMissions(prev => prev.filter(m => m.id !== mission.id))
    } catch (error: unknown) {
      setError(getErrorMessage(error))
    }
  }

  async function handleLoadMission(mission: MissionRecord) {
    if (missionLoadRef.current) return
    const abort = new AbortController()
    missionLoadRef.current = abort
    setLoadingMissionId(mission.id)
    try {
      const data = await loadProject(mission.project_dir, abort.signal)
      if (data.error) throw new Error(data.error)
      await onMissionReady(mission.project_dir, data.skillLevel || 'normal')
    } catch (error: unknown) {
      if (!isAbortError(error)) setError(getErrorMessage(error))
    } finally {
      if (missionLoadRef.current === abort) {
        missionLoadRef.current = null
        setLoadingMissionId(null)
      }
    }
  }

  async function handleCreateNewMission(topic: string, slug: string) {
    if (!topic || !slug || missionCreationRef.current) return
    if (pendingMissionFinalize) {
      setError('먼저 생성이 끝난 프로젝트의 학습 기록 확정을 재시도해 주세요.')
      return
    }
    const abort = new AbortController()
    missionCreationRef.current = abort
    setPendingNarrowTopic(null)
    setError('')
    setCreatingProgress({ stage: 'setup', message: '준비 중...' })
    try {
      const data = await confirmDailyMissionStream(topic, slug, (stage, message) => {
        if (missionCreationRef.current === abort && !abort.signal.aborted) {
          setCreatingProgress({ stage, message })
        }
      }, abort.signal, (pending) => {
        setPendingMissionFinalize(pending)
        if (!writePendingMissionFinalize(window.localStorage, finalizeStorageKey, pending)) {
          addToast('복구 정보를 브라우저에 저장하지 못했습니다. 생성이 끝날 때까지 이 탭을 닫지 마세요.', 'error')
        }
      })
      if (abort.signal.aborted) return
      if (!data?.project_dir) throw new Error('프로젝트 생성에 실패했습니다')
      clearPendingMissionFinalize(window.localStorage, finalizeStorageKey)
      setPendingMissionFinalize(null)
      await onMissionReady(data.project_dir, userSettings?.skill_level || 'normal')
    } catch (error: unknown) {
      if (error instanceof MissionFinalizePendingError) {
        setPendingMissionFinalize(error.pending)
        writePendingMissionFinalize(window.localStorage, finalizeStorageKey, error.pending)
        setError(error.message)
      } else if (!isAbortError(error)) {
        setError(getErrorMessage(error))
      }
    } finally {
      if (missionCreationRef.current === abort) {
        missionCreationRef.current = null
        setCreatingProgress(null)
      }
    }
  }

  async function handleRetryMissionFinalize() {
    if (!pendingMissionFinalize || finalizeRetrying) return
    const pending = pendingMissionFinalize
    setFinalizeRetrying(true)
    setError('')
    setCreatingProgress({ stage: 'watcher', message: '저장된 AI 생성 결과를 복구하고 학습 기록을 확정합니다.' })
    try {
      const data = await retryDailyMissionFinalize(pending)
      clearPendingMissionFinalize(window.localStorage, finalizeStorageKey)
      setPendingMissionFinalize(null)
      await onMissionReady(data.project_dir, pending.skill_level || userSettings?.skill_level || 'normal')
    } catch (error: unknown) {
      setError(`학습 기록 확정 재시도 실패: ${getErrorMessage(error)}`)
    } finally {
      setFinalizeRetrying(false)
      setCreatingProgress(null)
    }
  }

  function cancelMissionCreation() {
    if (!creatingProgress) return
    const status = describeMissionGeneration(
      creatingProgress.stage,
      creatingProgress.message,
      userSettings?.skill_level || 'normal',
    )
    if (!status.cancellable) return
    const abort = missionCreationRef.current
    if (!abort) return
    abort.abort()
    missionCreationRef.current = null
    setCreatingProgress(null)
    addToast('미션 생성을 취소했습니다. AI가 이미 완료한 단계는 되돌릴 수 없습니다.', 'info')
  }

  function prevMonth() {
    setCurrentMonth(prev =>
      prev.month === 0 ? { year: prev.year - 1, month: 11 } : { year: prev.year, month: prev.month - 1 }
    )
  }

  function nextMonth() {
    setCurrentMonth(prev =>
      prev.month === 11 ? { year: prev.year + 1, month: 0 } : { year: prev.year, month: prev.month + 1 }
    )
  }

  const monthNames = ['1월', '2월', '3월', '4월', '5월', '6월', '7월', '8월', '9월', '10월', '11월', '12월']
  const weekDays = ['SUN', 'MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT']
  const generationStatus = creatingProgress
    ? describeMissionGeneration(
        creatingProgress.stage,
        creatingProgress.message,
        userSettings?.skill_level || 'normal',
      )
    : null
  const editorWidthReady = isEditorWidthReady(viewportWidth)

  function renderDays() {
    const { year, month } = currentMonth
    const daysInMonth = getDaysInMonth(year, month)
    const firstDay = getFirstDayOfWeek(year, month)
    const cells: React.ReactElement[] = []

    for (let i = 0; i < firstDay; i++) {
      cells.push(<div key={`empty-${i}`} className="calendar-day empty" />)
    }

    for (let day = 1; day <= daysInMonth; day++) {
      const dateStr = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`
      const missions = missionsByDate.get(dateStr) || []
      const count = missions.length
      const hasCompleted = missions.some(m => m.status === 'completed')
      const hasActive = missions.some(m => m.status !== 'completed')
      const activityLevel = count >= 3 ? 3 : count === 2 ? 2 : count === 1 ? 1 : 0
      const isToday = dateStr === todayStr
      const isSelected = dateStr === selectedDate

      cells.push(
        <button
          type="button"
          key={dateStr}
          className={[
            'calendar-day',
            hasCompleted ? 'has-completed' : (hasActive ? `activity-${activityLevel}` : ''),
            isToday ? 'today' : '',
            isSelected && !isToday ? 'selected' : '',
          ].filter(Boolean).join(' ')}
          onClick={() => handleDateClick(dateStr)}
          title={`${dateStr} (${count}개 미션)`}
          aria-pressed={isSelected}
        >
          <span className="day-number">{day}</span>
          {count > 0 && <span className="day-dot" />}
        </button>
      )
    }

    return cells
  }

  if (loading) {
    return (
      <div className="dashboard-overlay">
        <div className="dashboard-container" style={{ alignItems: 'center', justifyContent: 'center' }}>
          <div className="rehab-loading">
            <div className="loading-pill">💊</div>
            <p>재활 프로그램 준비중...</p>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="dashboard-overlay">
      <div className="dashboard-container">

        {/* ── Sidebar ── */}
        <div className="dashboard-sidebar">
          <div className="dashboard-logo-area">
            <span className="dashboard-logo">💊</span>
            <div>
              <div className="dashboard-title">재활센터</div>
              <div className="dashboard-subtitle">코딩 치료 클리닉</div>
            </div>
          </div>

          <nav className="dashboard-nav">
            <div className="nav-item active">
              <span aria-hidden="true">🏥</span><span className="nav-label">재활 대시보드</span>
            </div>
          </nav>

          {/* 선택된 날짜 훈련 기록 */}
          <div className="sidebar-date-panel">
            <div className="sidebar-date-title">
              <span>
                {selectedDate === todayStr ? '오늘 · ' : ''}
                {selectedDate.slice(5).replace('-', '월 ')}일
              </span>
              <span className="sidebar-date-count">{selectedMissions.length}개</span>
            </div>

            {selectedMissions.length === 0 ? (
              <div className="sidebar-empty-state">
                <p className="sidebar-no-missions">훈련 기록 없음</p>
                <button
                  type="button"
                  className="sidebar-new-mission-btn"
                  onClick={showMissionMenu}
                  aria-haspopup="dialog"
                >
                  + 새 미션
                </button>
              </div>
            ) : (
              <div className="sidebar-mission-list">
                {selectedMissions.map((m) => (
                  <div
                    key={m.id}
                    className="sidebar-mission-item"
                  >
                    <button
                      type="button"
                      className="sidebar-mission-open"
                      onClick={() => void handleLoadMission(m)}
                      disabled={loadingMissionId !== null}
                    >
                      <span className="sidebar-mission-topic">{m.topic}</span>
                      <span className="sidebar-mission-meta">
                        {loadingMissionId === m.id ? '준비중…' : m.project_dir.split('/').slice(-1)[0]}
                      </span>
                      <span className={`project-status ${m.status === 'completed' ? 'completed' : ''}`}>
                        {m.status === 'completed' ? '완료' : '진행 중'}
                      </span>
                    </button>
                    <button
                      type="button"
                      className="mission-delete-btn"
                      onClick={(e) => {
                        e.stopPropagation()
                        if (window.confirm(`"${m.topic}" 미션을 삭제할까요?`)) {
                          handleDeleteMission(m)
                        }
                      }}
                      aria-label={`${m.topic} 미션 삭제`}
                    >
                      🗑
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="sidebar-settings-btn">
            <button type="button" className="nav-item" onClick={onOpenSettings}>
              <span aria-hidden="true">⚙</span><span className="nav-label">설정</span>
            </button>
            <button
              type="button"
              className={`nav-item ${testModeActive ? 'active' : ''}`}
              onClick={() => {
                const newTestMode = !testModeActive
                setTestModeActive(newTestMode)
                if (newTestMode) {
                  // 테스트 모드 활성화
                  setVnFading(false)
                  setVnScenario(testScenarios[0])
                  setVnStep(0)
                  setVnVisible(true)
                  setTestScenarioIndex(0)
                }
              }}
              title="시나리오 테스트 모드"
            >
              <span aria-hidden="true">🧪</span><span className="nav-label">테스트</span>
            </button>
          </div>
        </div>

        {/* ── 프로젝트 생성 진행 오버레이 ── */}
        {creatingProgress && generationStatus && (
          <div
            className="creation-overlay"
            role="dialog"
            aria-modal="true"
            aria-labelledby="mission-creation-title"
            aria-describedby="mission-creation-detail"
            onKeyDown={(event) => {
              if (event.key === 'Escape' && generationStatus.cancellable) cancelMissionCreation()
            }}
          >
            <div className="creation-modal" aria-live="polite">
              <div className="creation-spinner" />
              <div className="creation-stage" aria-hidden="true">{generationStatus.icon}</div>
              <h2 className="creation-title" id="mission-creation-title">{generationStatus.title}</h2>
              <p className="creation-message" id="mission-creation-detail">{generationStatus.detail}</p>
              {generationStatus.cancellable ? (
                <button
                  type="button"
                  className="creation-cancel-btn"
                  onClick={cancelMissionCreation}
                  autoFocus
                >
                  생성 취소
                </button>
              ) : (
                <p className="creation-lock-note">
                  {creatingProgress.stage === 'watcher'
                    ? '로컬 파일 적용은 중단하지 않고 안전하게 완료한 뒤 이동합니다.'
                    : '기록 확정 단계는 안전하게 완료한 뒤 이동합니다.'}
                </p>
              )}
            </div>
          </div>
        )}

        {pendingNarrowTopic && (
          <div className="creation-overlay" role="dialog" aria-modal="true" aria-labelledby="narrow-warning-title">
            <div className="viewport-warning-modal">
              <span className="viewport-warning-icon" aria-hidden="true">🖥️</span>
              <h2 id="narrow-warning-title">미션 시작에는 넓은 화면이 필요합니다</h2>
              <p>
                에디터는 최소 {MIN_EDITOR_WIDTH}px 너비에서 열립니다. 현재 창은 {viewportWidth}px입니다.
                창을 넓힌 뒤 <strong>{pendingNarrowTopic.name}</strong> 미션을 생성해 주세요.
              </p>
              <div className="viewport-warning-actions">
                <button type="button" className="viewport-warning-secondary" onClick={() => setPendingNarrowTopic(null)} autoFocus>
                  추천으로 돌아가기
                </button>
                <button
                  type="button"
                  className="viewport-warning-primary"
                  disabled={!editorWidthReady}
                  onClick={() => confirmNurseTopic(pendingNarrowTopic)}
                >
                  {editorWidthReady ? '미션 생성 시작' : `창을 ${MIN_EDITOR_WIDTH}px 이상으로 넓혀주세요`}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* ── VN 인트로 오버레이 ── */}
        {vnVisible && (
          <div
            className={`vn-intro ${vnFading ? 'vn-fade-out' : 'vn-fade-in'}`}
            onClick={advanceVn}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                advanceVn()
              }
              if (event.key === 'Escape') finishVnIntro()
            }}
            role="dialog"
            aria-modal="true"
            aria-label="오늘의 학습 안내"
            tabIndex={0}
          >
            <button
              type="button"
              className="vn-skip-btn"
              onClick={(event) => {
                event.stopPropagation()
                finishVnIntro()
              }}
            >
              건너뛰기
            </button>
            {/* 우측 캐릭터 — 이미지 전환 시 key로 재마운트해 애니메이션 재실행 */}
            <div className="vn-character" key={`${vnScenario}-${vnStep}`}>
              <img
                src={currentSlide.img}
                alt="간호사"
                onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
              />
              <div className="vn-character-placeholder">👩‍⚕️</div>
            </div>

            {/* 하단 대사창 */}
            <div className="vn-dialogue">
              <div className="vn-name-tag">담당 간호사</div>
              <p className="vn-dialogue-text" key={vnStep}>{currentSlide.text}</p>
              <div className="vn-continue-hint">
                <span className="vn-arrow">▼</span>
                {vnStep < currentSlides.length - 1 ? '클릭하여 계속' : '클릭하여 시작'}
              </div>
            </div>
          </div>
        )}

        {/* ── Nurse Chat 오버레이 ── */}
        {nurseChatVisible && (
          <div
            ref={nurseDialogRef}
            className={`vn-intro nurse-chat-overlay ${nurseChatFading ? 'vn-fade-out' : 'vn-fade-in'}`}
            role="dialog"
            aria-modal="true"
            aria-labelledby="nurse-chat-title"
            onKeyDown={(event) => {
              if (event.key === 'Escape') closeNurseChat()
            }}
          >
            <div className="vn-character nurse-chat-char">
              <img
                src="/greeting.webp"
                alt="간호사"
                onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
              />
              <div className="vn-character-placeholder">👩‍⚕️</div>
            </div>

            <div className="nurse-chat-panel">
              <div className="nurse-chat-header">
                <h2 className="nurse-name-tag" id="nurse-chat-title">담당 간호사</h2>
                <div className="nurse-chat-header-actions">
                  {nurseChatLoading && (
                    <button type="button" className="nurse-chat-cancel" onClick={cancelNurseRequest}>
                      AI 요청 취소
                    </button>
                  )}
                  <button type="button" className="nurse-chat-skip" onClick={closeNurseChat}>닫기 ✕</button>
                </div>
              </div>

              {/* 메시지 영역 */}
              <div className="nurse-chat-messages" role="log" aria-live="polite" aria-busy={nurseChatLoading}>
                {nurseChatHistory.map((msg, i) => (
                  <div key={i} className={`nurse-chat-msg nurse-chat-msg--${msg.role}`}>
                    {msg.role === 'nurse' && <span className="nurse-chat-avatar">👩‍⚕️</span>}
                    <div className="nurse-chat-bubble">
                      {msg.content.replace(/\[TOPICS\][\s\S]*?\[\/TOPICS\]/g, '').trim()}
                    </div>
                  </div>
                ))}
                {nurseChatLoading && nurseChatHistory.length === 0 && (
                  <div className="nurse-chat-msg nurse-chat-msg--nurse">
                    <span className="nurse-chat-avatar">👩‍⚕️</span>
                    <div className="nurse-chat-bubble nurse-chat-typing">
                      <span /><span /><span />
                    </div>
                  </div>
                )}
                <div ref={nurseChatBottomRef} />
              </div>
              {nurseRequestNotice && (
                <p className="nurse-request-notice" role="status">{nurseRequestNotice}</p>
              )}

              {/* choice 모드: 기존 vs 새로운 */}
              {nurseChatMode === 'choice' && (
                <div className="nurse-chat-topics">
                  <div className="nurse-chat-topics-label">
                    {activeMissions.length > 0 ? '어떻게 할까요? (1 / 2)' : '새 미션 준비'}
                  </div>
                  {activeMissions.length > 0 && (
                    <button
                      type="button"
                      className="nurse-chat-topic-btn"
                      onClick={() => { setNurseChatMode('mission-select'); setNurseMissionSelIdx(0) }}
                    >
                      <span className="nurse-chat-diff diff-low">1</span>
                      <span className="nurse-chat-topic-name">기존 훈련 계속하기</span>
                      <span className="nurse-chat-topic-arrow">↑↓ Enter</span>
                    </button>
                  )}
                  <button
                    type="button"
                    className="nurse-chat-topic-btn"
                    onClick={requestNurseRecommendations}
                  >
                    <span className="nurse-chat-diff diff-high">{activeMissions.length > 0 ? '2' : '1'}</span>
                    <span className="nurse-chat-topic-name">AI에게 새 미션 추천받기</span>
                    <span className="nurse-chat-topic-arrow">AI 추천 1회 →</span>
                  </button>
                </div>
              )}

              {/* mission-select 모드: 진행 중 미션 키보드 선택 */}
              {nurseChatMode === 'mission-select' && (
                <div className="nurse-chat-topics">
                  <div className="nurse-chat-topics-label">진행 중인 미션 — ↑↓ 이동, Enter 시작</div>
                  {activeMissions.map((m, i) => (
                    <button
                      type="button"
                      key={m.id}
                      data-mission-index={i}
                      className={`nurse-chat-topic-btn${nurseMissionSelIdx === i ? ' selected' : ''}`}
                      onClick={() => { closeNurseChat(); schedule(() => void handleLoadMission(m), 350) }}
                      onMouseEnter={() => setNurseMissionSelIdx(i)}
                      onFocus={() => setNurseMissionSelIdx(i)}
                    >
                      <span className={`nurse-chat-diff ${m.status === 'completed' ? 'diff-low' : 'diff-mid'}`}>
                        {m.status === 'completed' ? '완료' : '진행'}
                      </span>
                      <span className="nurse-chat-topic-name">{m.topic}</span>
                      {loadingMissionId === m.id
                        ? <span className="nurse-chat-topic-arrow">준비중...</span>
                        : <span className="nurse-chat-topic-arrow">→</span>
                      }
                    </button>
                  ))}
                </div>
              )}

              {/* chat 모드: 주제 추천 + 입력창 */}
              {nurseChatMode === 'chat' && (
                <>
                  {nurseChatSuggestedTopics.length > 0 && (
                    <div className="nurse-chat-topics">
                      <div className="nurse-chat-topics-label">추천 훈련 주제</div>
                      {nurseChatSuggestedTopics.map((t) => (
                        <button
                          type="button"
                          key={t.slug}
                          className="nurse-chat-topic-btn"
                          onClick={() => confirmNurseTopic(t)}
                        >
                          <span className={`nurse-chat-diff diff-${t.difficulty === '상' ? 'high' : t.difficulty === '중' ? 'mid' : 'low'}`}>
                            {t.difficulty}
                          </span>
                          <span className="nurse-chat-topic-name">{t.name}</span>
                          <span className="nurse-chat-topic-arrow">→</span>
                        </button>
                      ))}
                    </div>
                  )}
                  <div className="nurse-chat-input-row">
                    <input
                      ref={nurseChatInputRef}
                      className="nurse-chat-input"
                      placeholder="간호사에게 말하기... (예: 알고리즘 연습하고 싶어)"
                      value={nurseChatInput}
                      disabled={nurseChatLoading}
                      aria-label="간호사에게 보낼 메시지"
                      onChange={(e) => setNurseChatInput(e.target.value)}
                      onKeyDown={(e) => { if (e.key === 'Enter') handleNurseChatSubmit() }}
                    />
                    <button
                      type="button"
                      className="nurse-chat-send"
                      onClick={handleNurseChatSubmit}
                      disabled={nurseChatLoading || !nurseChatInput.trim()}
                    >
                      {nurseChatLoading ? '응답 중…' : '전송 · AI 1회'}
                    </button>
                  </div>
                </>
              )}
            </div>
          </div>
        )}

        {/* ── Main: Full-height Calendar ── */}
        <div className="dashboard-main" ref={mainRef}>
         <div
           className="calendar-content"
           ref={calendarRef}
           style={vnVisible ? { filter: 'blur(5px)', pointerEvents: 'none' } : undefined}
         >
          {latestMission && (
            <section className="resume-mission" aria-labelledby="resume-heading">
              <div className="resume-mission-copy">
                <p className="resume-mission-label" id="resume-heading">이어서 학습</p>
                <h2>{latestMission.topic}</h2>
                <p>{latestMission.date} 시작 · 진행 중</p>
              </div>
              <button
                className="resume-mission-btn"
                type="button"
                disabled={loadingMissionId !== null || !editorWidthReady}
                onClick={() => void handleLoadMission(latestMission)}
              >
                {loadingMissionId === latestMission.id ? '미션 여는 중…' : '이어서 학습 →'}
              </button>
              {activeMissions.length > 1 && (
                <details className="resume-other-missions">
                  <summary>다른 진행 중 미션 {activeMissions.length - 1}개</summary>
                  {activeMissions.slice(1).map((mission) => (
                    <button key={mission.id} type="button"
                      disabled={loadingMissionId !== null || !editorWidthReady}
                      onClick={() => void handleLoadMission(mission)}>
                      <span>{mission.topic}</span><small>{mission.date} 시작</small>
                    </button>
                  ))}
                </details>
              )}
              {!editorWidthReady && <p className="resume-width-note">이어서 학습하려면 창 너비를 {MIN_EDITOR_WIDTH}px 이상으로 늘려주세요.</p>}
            </section>
          )}
          {/* Month navigation */}
          <div className="calendar-header-row">
            <div className="calendar-month-label">
              {monthNames[currentMonth.month]}
              <span>{currentMonth.year}</span>
            </div>
            <div className="calendar-header-actions">
              <button
                type="button"
                className="new-mission-btn"
                onClick={showMissionMenu}
                aria-haspopup="dialog"
              >
                <span aria-hidden="true">＋</span> 새 미션
              </button>
              <div className="cal-nav-group" aria-label="달력 월 이동">
                <button type="button" className="cal-nav-btn" onClick={prevMonth} aria-label="이전 달">‹</button>
                <button type="button" className="cal-nav-btn" onClick={nextMonth} aria-label="다음 달">›</button>
              </div>
            </div>
          </div>

          {!editorWidthReady && (
            <div className="dashboard-width-notice" role="note">
              <span aria-hidden="true">🖥️</span>
              미션 상담은 가능하지만, 생성한 코드를 편집하려면 창 너비가 {MIN_EDITOR_WIDTH}px 이상이어야 합니다.
            </div>
          )}

          {/* Weekday headers */}
          <div className="calendar-weekdays">
            {weekDays.map(d => (
              <div key={d} className="calendar-weekday">{d}</div>
            ))}
          </div>

          {/* Day cells — fills remaining height */}
          <div className="calendar-wrapper">
            <div className="calendar-days-grid">
              {renderDays()}
            </div>
          </div>

          {(error || pendingMissionFinalize) && (
            <div className="error-banner" style={{ marginTop: '1rem' }}>
              <p className="error-message">
                {friendlyError(error || '프로젝트 파일 생성은 끝났지만 학습 기록 확정이 필요합니다.')}
              </p>
              {isSettingsError(error) && (
                <button className="btn btn-primary" onClick={onOpenSettings}>설정으로 이동</button>
              )}
              {pendingMissionFinalize ? (
                <button
                  className="btn btn-primary"
                  onClick={() => { void handleRetryMissionFinalize() }}
                  disabled={finalizeRetrying}
                >
                  {finalizeRetrying ? '생성 결과 복구 중…' : 'AI 재호출 없이 생성 결과 복구'}
                </button>
              ) : !isSettingsError(error) && (
                <button className="btn btn-primary" onClick={() => window.location.reload()}>다시 시도</button>
              )}
            </div>
          )}
         </div>
        </div>

      </div>
    </div>
  )
}
