import { useState, useEffect, useRef } from 'react'
import { useStore, type FileEntry } from '../../store'
import { useProject } from '../../hooks/useProject'
import { lspClient } from '../../lib/lspClient'
import { supabase } from '../../lib/supabase'
import './FileTree.css'
import { LOCAL } from '../../lib/api'

const EXT_TO_LANG: Record<string, string> = {
  go: 'go',
  ts: 'typescript',
  tsx: 'typescriptreact',
  js: 'javascript',
  jsx: 'javascriptreact',
  rs: 'rust',
  sol: 'sol',
  py: 'python',
}

function detectLanguageFromPath(filePath: string): string {
  const ext = filePath.split('.').pop()?.toLowerCase() || ''
  return EXT_TO_LANG[ext] || 'plaintext'
}

async function authHeaders(): Promise<Record<string, string>> {
  const { data: { session } } = await supabase.auth.getSession()
  const h: Record<string, string> = { 'Content-Type': 'application/json' }
  if (session?.access_token) h['Authorization'] = `Bearer ${session.access_token}`
  return h
}

interface ContextMenu {
  x: number
  y: number
  fullPath: string
  name: string
  isDir: boolean
}

interface TreeNodeProps {
  entry: FileEntry
  rootDir: string
  depth: number
  onContextMenu: (e: React.MouseEvent, fullPath: string, name: string, isDir: boolean) => void
}

function TreeNode({ entry, rootDir, depth, onContextMenu }: TreeNodeProps) {
  const [open, setOpen] = useState(depth === 0)
  const { openFile, addTab, changedFiles } = useStore()
  const { readFile } = useProject()

  const fullPath = rootDir + '/' + entry.path

  async function handleClick() {
    if (entry.isDir) {
      setOpen(!open)
      return
    }
    const content = await readFile(fullPath)
    addTab(fullPath, content)
    lspClient.notifyOpen(fullPath, content, detectLanguageFromPath(fullPath)).catch(() => {})
  }

  const isActive = openFile === fullPath
  const isChanged = changedFiles.has(fullPath)

  return (
    <div className="tree-node">
      <div
        className={`tree-item ${isActive ? 'active' : ''} ${entry.isDir ? 'dir' : ''}`}
        style={{ paddingLeft: `${depth * 12 + 8}px` }}
        onClick={handleClick}
        onContextMenu={(e) => onContextMenu(e, fullPath, entry.name, entry.isDir)}
      >
        <span className="tree-icon">
          {entry.isDir ? (open ? '▾' : '▸') : ''}
        </span>
        <span className="tree-file-icon">
          {entry.isDir ? '📁' : getFileIcon(entry.name)}
        </span>
        <span className="tree-name">{entry.name}</span>
        {isChanged && <span className="changed-dot" />}
      </div>
      {entry.isDir && open && entry.children && (
        <div className="tree-children">
          {entry.children.map((child) => (
            <TreeNode
              key={child.path}
              entry={child}
              rootDir={rootDir}
              depth={depth + 1}
              onContextMenu={onContextMenu}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function getFileIcon(name: string): string {
  const ext = name.split('.').pop()?.toLowerCase()
  const icons: Record<string, string> = {
    go: '🔵',
    ts: '🔷',
    tsx: '🔷',
    js: '🟡',
    jsx: '🟡',
    rs: '🟠',
    sol: '💎',
    py: '🐍',
    md: '📝',
    json: '⚙️',
    toml: '⚙️',
    yaml: '⚙️',
    yml: '⚙️',
  }
  return icons[ext || ''] || '📄'
}

export default function FileTree() {
  const { fileTree, projectStatus, closeTab } = useStore()
  const { refreshFileTree } = useProject()
  const dir = projectStatus?.dir || ''

  const [ctxMenu, setCtxMenu] = useState<ContextMenu | null>(null)
  const [renaming, setRenaming] = useState<{ path: string; name: string } | null>(null)
  const renameInputRef = useRef<HTMLInputElement>(null)

  // Close context menu on outside click
  useEffect(() => {
    if (!ctxMenu) return
    const handler = () => setCtxMenu(null)
    window.addEventListener('click', handler)
    return () => window.removeEventListener('click', handler)
  }, [ctxMenu])

  useEffect(() => {
    if (renaming) renameInputRef.current?.select()
  }, [renaming])

  function handleContextMenu(e: React.MouseEvent, fullPath: string, name: string, isDir: boolean) {
    e.preventDefault()
    e.stopPropagation()
    setCtxMenu({ x: e.clientX, y: e.clientY, fullPath, name, isDir })
  }

  function startRename() {
    if (!ctxMenu) return
    setRenaming({ path: ctxMenu.fullPath, name: ctxMenu.name })
    setCtxMenu(null)
  }

  async function confirmRename(newName: string) {
    if (!renaming || !newName.trim() || newName === renaming.name) {
      setRenaming(null)
      return
    }
    const parent = renaming.path.substring(0, renaming.path.lastIndexOf('/'))
    const newPath = parent + '/' + newName.trim()
    try {
      await fetch(`${LOCAL}/api/fs/rename`, {
        method: 'POST',
        headers: await authHeaders(),
        body: JSON.stringify({ from: renaming.path, to: newPath }),
      })
      closeTab(renaming.path)
      if (dir) await refreshFileTree(dir)
    } catch { /* ignore */ }
    setRenaming(null)
  }

  async function handleDelete() {
    if (!ctxMenu) return
    const target = ctxMenu
    setCtxMenu(null)
    if (!window.confirm(`"${target.name}"을(를) 삭제하시겠습니까?`)) return
    try {
      await fetch(`${LOCAL}/api/fs/delete`, {
        method: 'DELETE',
        headers: await authHeaders(),
        body: JSON.stringify({ path: target.fullPath }),
      })
      closeTab(target.fullPath)
      if (dir) await refreshFileTree(dir)
    } catch { /* ignore */ }
  }

  if (!fileTree.length) {
    return (
      <div className="filetree-empty">
        <p>파일 없음</p>
      </div>
    )
  }

  return (
    <div className="filetree">
      <div className="filetree-header">
        <span>탐색기</span>
      </div>
      <div className="filetree-content">
        {fileTree.map((entry) => (
          <TreeNode
            key={entry.path}
            entry={entry}
            rootDir={dir}
            depth={0}
            onContextMenu={handleContextMenu}
          />
        ))}
      </div>

      {/* Context menu */}
      {ctxMenu && (
        <div
          className="filetree-ctx-menu"
          style={{ top: ctxMenu.y, left: ctxMenu.x }}
          onClick={(e) => e.stopPropagation()}
        >
          <button className="ctx-menu-item" onClick={startRename}>
            ✏️ 이름 변경
          </button>
          <button className="ctx-menu-item ctx-menu-danger" onClick={handleDelete}>
            🗑 삭제
          </button>
        </div>
      )}

      {/* Inline rename input */}
      {renaming && (
        <div className="filetree-rename-overlay" onClick={() => setRenaming(null)}>
          <div className="filetree-rename-box" onClick={(e) => e.stopPropagation()}>
            <span className="filetree-rename-label">이름 변경</span>
            <input
              ref={renameInputRef}
              className="filetree-rename-input"
              defaultValue={renaming.name}
              onKeyDown={(e) => {
                if (e.key === 'Enter') confirmRename((e.target as HTMLInputElement).value)
                if (e.key === 'Escape') setRenaming(null)
              }}
            />
            <div className="filetree-rename-actions">
              <button
                className="filetree-rename-ok"
                onClick={() => confirmRename(renameInputRef.current?.value || '')}
              >
                확인
              </button>
              <button className="filetree-rename-cancel" onClick={() => setRenaming(null)}>
                취소
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
