import { useStore } from '../store'
import type { QuizData, ChatMessage } from '../store'
import { supabase } from '../lib/supabase'
import { REMOTE, LOCAL, AI_PROXY_URL } from '../lib/api'

async function authHeaders(): Promise<Record<string, string>> {
  const { data: { session } } = await supabase.auth.getSession()
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (session?.access_token) {
    headers['Authorization'] = `Bearer ${session.access_token}`
  }
  return headers
}

async function authToken(): Promise<string> {
  const { data: { session } } = await supabase.auth.getSession()
  return session?.access_token ?? ''
}

/** Fetch against the REMOTE home server (AI + Supabase). */
async function fetchRemote(path: string, init?: RequestInit) {
  const headers = await authHeaders()
  return fetch(`${REMOTE}${path}`, {
    ...init,
    headers: { ...headers, ...(init?.headers as Record<string, string> | undefined) },
  })
}

/** Fetch against the LOCAL binary (file I/O, watcher). No auth header needed. */
async function fetchLocal(path: string, init?: RequestInit) {
  return fetch(`${LOCAL}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init?.headers as Record<string, string> | undefined) },
  })
}

export interface TopicSuggestion {
  name: string
  slug: string
  difficulty: '상' | '중' | '하'
}

export function useProject() {
  const { projectStatus, setProjectStatus, setFileTree } = useStore()

  async function refreshStatus() {
    const res = await fetchLocal('/api/project/status')
    const data = await res.json()
    setProjectStatus(data)
    return data
  }

  async function refreshFileTree(dir: string) {
    const res = await fetchLocal(`/api/fs/list?path=${encodeURIComponent(dir)}`)
    const data = await res.json()
    setFileTree(data || [])
  }

  async function readFile(path: string): Promise<string> {
    const res = await fetchLocal(`/api/fs/read?path=${encodeURIComponent(path)}`)
    const data = await res.json()
    return data.content || ''
  }

  async function writeFile(path: string, content: string) {
    await fetchLocal('/api/fs/write', {
      method: 'POST',
      body: JSON.stringify({ path, content }),
    })
  }

  async function loadQuizData(): Promise<QuizData> {
    const res = await fetchLocal('/api/quiz')
    const data = await res.json()
    return data as QuizData
  }

  async function getDailyMission() {
    const res = await fetchRemote('/api/daily')
    return res.json()
  }

  async function getDailyHistory(): Promise<any[]> {
    try {
      const res = await fetchRemote('/api/daily/history')
      if (!res.ok) return []
      const text = await res.text()
      if (!text || text.trim().startsWith('<')) return []
      return JSON.parse(text)
    } catch {
      return []
    }
  }

  async function confirmDailyMission(topic: string, slug: string) {
    const res = await fetchRemote('/api/daily/confirm', {
      method: 'POST',
      body: JSON.stringify({ topic, slug }),
    })
    return res.json()
  }

  /** Load an existing project on the local server. */
  async function loadProject(dir: string) {
    const token = await authToken()
    const res = await fetchLocal('/api/project/load', {
      method: 'POST',
      body: JSON.stringify({ dir, ai_proxy_url: AI_PROXY_URL, token }),
    })
    return res.json()
  }

  async function listSnapshots(): Promise<string[]> {
    const res = await fetchLocal('/api/project/snapshots')
    const data = await res.json()
    return data.snapshots || []
  }

  async function restoreSnapshot(step: string) {
    const res = await fetchLocal('/api/project/snapshot/restore', {
      method: 'POST',
      body: JSON.stringify({ step }),
    })
    return res.json()
  }

  /** Complete a mission: update DB on REMOTE, stop watcher on LOCAL. */
  async function completeMission() {
    const status = useStore.getState().projectStatus
    const projectDir = status?.dir || ''
    const [remoteRes] = await Promise.all([
      fetchRemote('/api/project/complete', {
        method: 'POST',
        body: JSON.stringify({ project_dir: projectDir }),
      }),
      fetchLocal('/api/project/stop-watcher', { method: 'POST', body: '{}' }),
    ])
    return remoteRes.json()
  }

  /**
   * Delete project: remove files on LOCAL, remove DB record on REMOTE.
   */
  async function deleteProject(projectDir: string) {
    await fetchLocal('/api/project/files', {
      method: 'DELETE',
      body: JSON.stringify({ project_dir: projectDir }),
    })
    const res = await fetchRemote('/api/project', {
      method: 'DELETE',
      body: JSON.stringify({ project_dir: projectDir }),
    })
    return res.json()
  }

  async function reloadOpenTabs() {
    const { openTabs } = useStore.getState()
    for (const tab of openTabs) {
      try {
        const content = await readFile(tab.path)
        useStore.getState().updateTabContent(tab.path, content)
      } catch { /* deleted files are ignored */ }
    }
  }

  async function sendChat(message: string, fileContent: string, chatHistory: ChatMessage[]): Promise<ReadableStream<Uint8Array> | null> {
    const res = await fetchLocal('/api/chat', {
      method: 'POST',
      body: JSON.stringify({ message, fileContent, chatHistory }),
    })
    if (!res.ok || !res.body) return null
    return res.body
  }

  /**
   * Composite flow: REMOTE SSE (AI generation) → LOCAL setup (write files + start watcher).
   */
  async function confirmDailyMissionStream(
    topic: string,
    slug: string,
    onProgress: (stage: string, message: string) => void,
  ): Promise<{ project_dir: string; files: string[] } | null> {
    const token = await authToken()

    // Step 1: REMOTE SSE — AI generates curriculum + code files
    const headers = await authHeaders()
    const res = await fetch(`${REMOTE}/api/daily/confirm-stream`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ topic, slug }),
    })
    if (!res.ok || !res.body) return null

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    let doneData: any = null

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const chunks = buf.split('\n\n')
      buf = chunks.pop() ?? ''
      for (const chunk of chunks) {
        const eventMatch = chunk.match(/^event: (\w+)/)
        const dataMatch = chunk.match(/^data: (.+)$/m)
        if (!eventMatch || !dataMatch) continue
        const event = eventMatch[1]
        try {
          const data = JSON.parse(dataMatch[1])
          if (event === 'progress') onProgress(data.stage, data.message)
          else if (event === 'done') doneData = data
          else if (event === 'error') throw new Error(data.error)
        } catch (e) {
          if ((e as Error).message !== 'Unexpected end of JSON input') throw e
        }
      }
    }

    if (!doneData) return null

    // Step 2: LOCAL — write files, start watcher
    onProgress('watcher', '파일 감시자를 시작하고 있습니다...')
    const setupRes = await fetchLocal('/api/project/setup', {
      method: 'POST',
      body: JSON.stringify({
        dir_suffix: doneData.dir_suffix,
        files: doneData.files,
        curriculum: doneData.curriculum,
        skill_level: doneData.skill_level,
        language: doneData.language,
        ai_proxy_url: AI_PROXY_URL,
        token,
      }),
    })
    if (!setupRes.ok) return null
    const setupData = await setupRes.json()

    return {
      project_dir: setupData.project_dir,
      files: Object.keys(doneData.files || {}),
    }
  }

  /**
   * Composite next-step flow:
   * 1. LOCAL read-all → get current files + curriculum
   * 2. REMOTE nextstep → AI generates new curriculum + files
   * 3. LOCAL apply-step → write new files, update state, restart watcher
   */
  async function advanceToNextStep() {
    const token = await authToken()

    // Step 1: LOCAL — read current project state
    const readRes = await fetchLocal('/api/project/read-all')
    if (!readRes.ok) return { error: 'read-all failed' }
    const { files: currentFiles, curriculum } = await readRes.json()

    const skillLevel = useStore.getState().skillLevel || 'normal'

    // Step 2: REMOTE — AI generates next step
    const nextRes = await fetchRemote('/api/project/nextstep', {
      method: 'POST',
      body: JSON.stringify({
        curriculum,
        current_files: currentFiles,
        skill_level: skillLevel,
      }),
    })
    if (!nextRes.ok) return { error: 'nextstep failed' }
    const nextData = await nextRes.json()

    if (nextData.done) {
      return { done: true, message: nextData.message }
    }

    // Step 3: LOCAL — write new files (+ quiz.json if newbie), restart watcher
    const applyRes = await fetchLocal('/api/project/apply-step', {
      method: 'POST',
      body: JSON.stringify({
        new_curriculum: nextData.new_curriculum,
        new_files: nextData.new_files,
        ai_proxy_url: AI_PROXY_URL,
        token,
        quiz_data: nextData.quiz_data ?? null,
      }),
    })
    if (!applyRes.ok) return { error: 'apply-step failed' }
    const applyData = await applyRes.json()

    return applyData
  }

  async function nurseChat(
    message: string,
    history: { role: string; content: string }[],
    pastTopics: string[],
  ): Promise<ReadableStream<Uint8Array> | null> {
    const res = await fetchRemote('/api/daily/nurse-chat', {
      method: 'POST',
      body: JSON.stringify({ message, history, pastTopics }),
    })
    if (!res.ok || !res.body) return null
    return res.body
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
