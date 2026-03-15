import { useEffect, useRef, useState, useCallback } from 'react'
import { useStore } from '../../store'
import { supabase } from '../../lib/supabase'

interface FileMatch {
  relPath: string
  absPath: string
  name: string
}

async function authHeaders(): Promise<Record<string, string>> {
  const { data: { session } } = await supabase.auth.getSession()
  const headers: Record<string, string> = {}
  if (session?.access_token) {
    headers['Authorization'] = `Bearer ${session.access_token}`
  }
  return headers
}

function highlightMatch(text: string, query: string): React.ReactNode {
  if (!query) return text
  const idx = text.toLowerCase().indexOf(query.toLowerCase())
  if (idx < 0) return text
  return (
    <>
      {text.slice(0, idx)}
      <mark>{text.slice(idx, idx + query.length)}</mark>
      {text.slice(idx + query.length)}
    </>
  )
}

export default function QuickOpen() {
  const { setShowQuickOpen, openTabs, addTab, projectStatus } = useStore()
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<FileMatch[]>([])
  const [selectedIdx, setSelectedIdx] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const fetchResults = useCallback(async (q: string) => {
    const dir = projectStatus?.dir
    if (!dir) return
    const headers = await authHeaders()
    const url = `/api/fs/search/files?q=${encodeURIComponent(q)}&path=${encodeURIComponent(dir)}`
    try {
      const res = await fetch(url, { headers })
      const data: FileMatch[] = await res.json()
      setResults(data)
      setSelectedIdx(0)
    } catch { /* ignore */ }
  }, [projectStatus?.dir])

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current)
    if (!query) {
      // Show open tabs when query is empty
      setResults(openTabs.map(t => ({
        relPath: t.path.split('/').pop() || t.path,
        absPath: t.path,
        name: t.path.split('/').pop() || t.path,
      })))
      setSelectedIdx(0)
      return
    }
    debounceRef.current = setTimeout(() => fetchResults(query), 300)
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current) }
  }, [query, fetchResults, openTabs])

  async function openFile(absPath: string) {
    const headers = await authHeaders()
    try {
      const res = await fetch(`/api/fs/read?path=${encodeURIComponent(absPath)}`, { headers })
      const data = await res.json()
      addTab(absPath, data.content || '')
    } catch { /* ignore */ }
    setShowQuickOpen(false)
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') {
      setShowQuickOpen(false)
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setSelectedIdx((i) => Math.min(i + 1, results.length - 1))
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      setSelectedIdx((i) => Math.max(i - 1, 0))
      return
    }
    if (e.key === 'Enter' && results[selectedIdx]) {
      openFile(results[selectedIdx].absPath)
    }
  }

  return (
    <div className="quick-open-backdrop" onClick={() => setShowQuickOpen(false)}>
      <div className="quick-open-panel" onClick={(e) => e.stopPropagation()}>
        <div className="quick-open-input-row">
          <span className="quick-open-icon">🔍</span>
          <input
            ref={inputRef}
            className="quick-open-input"
            placeholder="파일명 입력..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
          />
        </div>
        <div className="quick-open-results">
          {results.length === 0 && (
            <div className="quick-open-empty">
              {query ? '결과 없음' : '열린 파일 없음'}
            </div>
          )}
          {results.map((r, i) => (
            <div
              key={r.absPath}
              className={`quick-open-item ${i === selectedIdx ? 'selected' : ''}`}
              onClick={() => openFile(r.absPath)}
              onMouseEnter={() => setSelectedIdx(i)}
            >
              <span className="quick-open-item-icon">📄</span>
              <span className="quick-open-item-name">
                {highlightMatch(r.name, query)}
              </span>
              <span className="quick-open-item-dir">
                {r.relPath.includes('/') ? r.relPath.substring(0, r.relPath.lastIndexOf('/')) : ''}
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
