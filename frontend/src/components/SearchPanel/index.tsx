import { useEffect, useRef, useState, useCallback } from 'react'
import { useStore } from '../../store'
import { apiJson } from '../../lib/api'
import { getErrorMessage } from '../../lib/errors'

interface ContentMatch {
  relPath: string
  absPath: string
  lineNum: number
  lineContent: string
  colStart: number
}

interface GroupedResult {
  relPath: string
  absPath: string
  matches: ContentMatch[]
}

function groupResults(matches: ContentMatch[]): GroupedResult[] {
  const map = new Map<string, GroupedResult>()
  for (const m of matches) {
    if (!map.has(m.absPath)) {
      map.set(m.absPath, { relPath: m.relPath, absPath: m.absPath, matches: [] })
    }
    map.get(m.absPath)!.matches.push(m)
  }
  return Array.from(map.values())
}

function highlightLine(line: string, query: string, colStart: number): React.ReactNode {
  if (!query || colStart < 0) return line
  return (
    <>
      {line.slice(0, colStart)}
      <mark>{line.slice(colStart, colStart + query.length)}</mark>
      {line.slice(colStart + query.length)}
    </>
  )
}

export default function SearchPanel() {
  const { setShowSearchPanel, addTab, addToast, setPendingNavigate, projectStatus } = useStore()
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<GroupedResult[]>([])
  const [loading, setLoading] = useState(false)
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const fetchResults = useCallback(async (q: string) => {
    const dir = projectStatus?.dir
    if (!dir || !q) {
      setResults([])
      return
    }
    setLoading(true)
    try {
      const path = `/api/fs/search/content?q=${encodeURIComponent(q)}&path=${encodeURIComponent(dir)}`
      const data = await apiJson<ContentMatch[]>(path)
      setResults(groupResults(data))
    } catch (error: unknown) {
      addToast(`프로젝트 검색 실패: ${getErrorMessage(error)}`, 'error')
    } finally {
      setLoading(false)
    }
  }, [addToast, projectStatus?.dir])

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current)
    if (!query.trim()) {
      setResults([])
      return
    }
    debounceRef.current = setTimeout(() => fetchResults(query), 500)
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current) }
  }, [query, fetchResults])

  async function openResult(absPath: string, lineNum: number) {
    try {
      const data = await apiJson<{ content?: string }>(`/api/fs/read?path=${encodeURIComponent(absPath)}`)
      addTab(absPath, data.content || '')
      setPendingNavigate({ path: absPath, line: lineNum, column: 1 })
    } catch (error: unknown) {
      addToast(`파일 열기 실패: ${getErrorMessage(error)}`, 'error')
    }
  }

  function toggleCollapse(absPath: string) {
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(absPath)) next.delete(absPath)
      else next.add(absPath)
      return next
    })
  }

  const totalMatches = results.reduce((sum, g) => sum + g.matches.length, 0)

  return (
    <div className="search-panel">
      <div className="search-panel-header">
        <div className="search-panel-input-row">
          <span className="search-panel-icon">🔍</span>
          <input
            ref={inputRef}
            className="search-panel-input"
            aria-label="프로젝트에서 검색할 내용"
            placeholder="프로젝트 검색..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') setShowSearchPanel(false)
            }}
          />
          <button aria-label="검색 패널 닫기" className="search-panel-close" onClick={() => setShowSearchPanel(false)}>✕</button>
        </div>
        {totalMatches > 0 && (
          <div className="search-panel-summary">{totalMatches}개 결과</div>
        )}
      </div>
      <div className="search-panel-results">
        {loading && <div className="search-panel-loading">검색 중...</div>}
        {!loading && query && results.length === 0 && (
          <div className="search-panel-empty">결과 없음</div>
        )}
        {results.map((group) => (
          <div key={group.absPath} className="search-group">
            <button
              type="button"
              aria-expanded={!collapsed.has(group.absPath)}
              className="search-group-header"
              onClick={() => toggleCollapse(group.absPath)}
            >
              <span className="search-group-toggle">
                {collapsed.has(group.absPath) ? '▶' : '▼'}
              </span>
              <span className="search-group-filename">
                {group.relPath.split('/').pop()}
              </span>
              <span className="search-group-dir">
                {group.relPath.includes('/')
                  ? group.relPath.substring(0, group.relPath.lastIndexOf('/'))
                  : ''}
              </span>
              <span className="search-group-count">({group.matches.length})</span>
            </button>
            {!collapsed.has(group.absPath) && group.matches.map((m, i) => (
              <button
                type="button"
                key={i}
                aria-label={`${group.relPath} ${m.lineNum}행: ${m.lineContent.trim()}`}
                className="search-result-item"
                onClick={() => openResult(m.absPath, m.lineNum)}
              >
                <span className="search-result-linenum">{m.lineNum}</span>
                <span className="search-result-content">
                  {highlightLine(m.lineContent.trim(), query, m.colStart - (m.lineContent.length - m.lineContent.trimStart().length))}
                </span>
              </button>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}
