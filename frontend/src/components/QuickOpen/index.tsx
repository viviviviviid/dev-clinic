import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useStore } from '../../store'
import { apiJson } from '../../lib/api'
import { getErrorMessage } from '../../lib/errors'

interface FileMatch {
  relPath: string
  absPath: string
  name: string
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
  const { setShowQuickOpen, openTabs, addTab, addToast, projectStatus } = useStore()
  const [query, setQuery] = useState('')
  const [searchResults, setSearchResults] = useState<FileMatch[]>([])
  const [selectedIdx, setSelectedIdx] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const fetchResults = useCallback(async (q: string) => {
    const dir = projectStatus?.dir
    if (!dir) return
    const url = `/api/fs/search/files?q=${encodeURIComponent(q)}&path=${encodeURIComponent(dir)}`
    try {
      const data = await apiJson<FileMatch[]>(url)
      setSearchResults(data)
      setSelectedIdx(0)
    } catch (error: unknown) {
      addToast(`파일 검색 실패: ${getErrorMessage(error)}`, 'error')
    }
  }, [addToast, projectStatus?.dir])

  const openTabResults = useMemo(() => openTabs.map((tab) => ({
    relPath: tab.path.split('/').pop() || tab.path,
    absPath: tab.path,
    name: tab.path.split('/').pop() || tab.path,
  })), [openTabs])
  const results = query ? searchResults : openTabResults

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current)
    if (!query) return
    debounceRef.current = setTimeout(() => fetchResults(query), 300)
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current) }
  }, [query, fetchResults])

  async function openFile(absPath: string) {
    try {
      const data = await apiJson<{ content?: string }>(`/api/fs/read?path=${encodeURIComponent(absPath)}`)
      addTab(absPath, data.content || '')
      setShowQuickOpen(false)
    } catch (error: unknown) {
      addToast(`파일 열기 실패: ${getErrorMessage(error)}`, 'error')
    }
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
            onChange={(e) => {
              setQuery(e.target.value)
              setSelectedIdx(0)
            }}
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
