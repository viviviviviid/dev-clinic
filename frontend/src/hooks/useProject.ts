import { useStore } from '../store'
import type { QuizData, ChatMessage } from '../store'
import { supabase } from '../lib/supabase'

async function authHeaders(): Promise<Record<string, string>> {
  const { data: { session } } = await supabase.auth.getSession()
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (session?.access_token) {
    headers['Authorization'] = `Bearer ${session.access_token}`
  }
  return headers
}

export interface TopicSuggestion {
  name: string
  slug: string
  difficulty: '상' | '중' | '하'
}

export function useProject() {
  const { projectStatus, setProjectStatus, setFileTree } = useStore()

  async function refreshStatus() {
    const res = await fetch('/api/project/status', { headers: await authHeaders() })
    const data = await res.json()
    setProjectStatus(data)
    return data
  }

  async function refreshFileTree(dir: string) {
    const res = await fetch(`/api/fs/list?path=${encodeURIComponent(dir)}`, {
      headers: await authHeaders(),
    })
    const data = await res.json()
    setFileTree(data || [])
  }

  async function readFile(path: string): Promise<string> {
    const res = await fetch(`/api/fs/read?path=${encodeURIComponent(path)}`, {
      headers: await authHeaders(),
    })
    const data = await res.json()
    return data.content || ''
  }

  async function writeFile(path: string, content: string) {
    await fetch('/api/fs/write', {
      method: 'POST',
      headers: await authHeaders(),
      body: JSON.stringify({ path, content }),
    })
  }

  async function loadQuizData(): Promise<QuizData> {
    const res = await fetch('/api/quiz', { headers: await authHeaders() })
    const data = await res.json()
    return data as QuizData
  }

  async function getDailyMission() {
    const res = await fetch('/api/daily', { headers: await authHeaders() })
    return res.json()
  }

  async function getDailyHistory(): Promise<any[]> {
    try {
      const res = await fetch('/api/daily/history', { headers: await authHeaders() })
      if (!res.ok) return []
      const text = await res.text()
      if (!text || text.trim().startsWith('<')) return []
      return JSON.parse(text)
    } catch {
      return []
    }
  }

  async function confirmDailyMission(topic: string, slug: string) {
    const res = await fetch('/api/daily/confirm', {
      method: 'POST',
      headers: await authHeaders(),
      body: JSON.stringify({ topic, slug }),
    })
    return res.json()
  }

  async function loadProject(dir: string) {
    const res = await fetch('/api/project/load', {
      method: 'POST',
      headers: await authHeaders(),
      body: JSON.stringify({ dir }),
    })
    return res.json()
  }

  async function advanceToNextStep() {
    const res = await fetch('/api/project/nextstep', {
      method: 'POST',
      headers: await authHeaders(),
    })
    return res.json()
  }

  async function listSnapshots(): Promise<string[]> {
    const res = await fetch('/api/project/snapshots', { headers: await authHeaders() })
    const data = await res.json()
    return data.snapshots || []
  }

  async function restoreSnapshot(step: string) {
    const res = await fetch('/api/project/snapshot/restore', {
      method: 'POST',
      headers: await authHeaders(),
      body: JSON.stringify({ step }),
    })
    return res.json()
  }

  async function completeMission() {
    const res = await fetch('/api/project/complete', {
      method: 'POST',
      headers: await authHeaders(),
    })
    return res.json()
  }

  async function sendChat(message: string, fileContent: string, chatHistory: ChatMessage[]): Promise<ReadableStream<Uint8Array> | null> {
    const headers = await authHeaders()
    const res = await fetch('/api/chat', {
      method: 'POST',
      headers,
      body: JSON.stringify({ message, fileContent, chatHistory }),
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
    loadProject,
    advanceToNextStep,
    completeMission,
    listSnapshots,
    restoreSnapshot,
    sendChat,
  }
}
