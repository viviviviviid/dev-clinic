import { useState, useEffect, useRef } from 'react'
import { useStore, type FileEntry } from '../../store'
import { useProject } from '../../hooks/useProject'
import { lspClient } from '../../lib/lspClient'
import { apiFetch } from '../../lib/api'
import { getErrorMessage } from '../../lib/errors'
import './FileTree.css'

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

function isAffectedPath(openPath: string, targetPath: string, isDir: boolean): boolean {
  return openPath === targetPath || (isDir && openPath.startsWith(`${targetPath}/`))
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
  const { openFile, addTab, changedFiles, addToast } = useStore()
  const { readFile } = useProject()

  const fullPath = rootDir + '/' + entry.path

  async function handleClick() {
    if (entry.isDir) {
      setOpen(!open)
      return
    }
    try {
      const content = await readFile(fullPath)
      addTab(fullPath, content)
      lspClient.notifyOpen(fullPath, content, detectLanguageFromPath(fullPath)).catch(() => {})
    } catch (error: unknown) {
      addToast(`파일 열기 실패: ${getErrorMessage(error)}`, 'error')
    }
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
  const { fileTree, projectStatus, closeTab, addToast } = useStore()
  const { refreshFileTree } = useProject()
  const dir = projectStatus?.dir || ''

  const [ctxMenu, setCtxMenu] = useState<ContextMenu | null>(null)
  const [renaming, setRenaming] = useState<{ path: string; name: string; isDir: boolean } | null>(null)
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
    setRenaming({ path: ctxMenu.fullPath, name: ctxMenu.name, isDir: ctxMenu.isDir })
    setCtxMenu(null)
  }

  function closeAffectedTabs(targetPath: string, isDir: boolean) {
    const tabs = useStore.getState().openTabs
    for (const tab of tabs) {
      if (isAffectedPath(tab.path, targetPath, isDir)) closeTab(tab.path)
    }
  }

  async function confirmRename(newName: string) {
    const trimmedName = newName.trim()
    if (!renaming || !trimmedName || trimmedName === renaming.name) {
      setRenaming(null)
      return
    }
    if (trimmedName === '.' || trimmedName === '..' || trimmedName.includes('/') || trimmedName.includes('\\')) {
      addToast('파일 이름에는 경로 구분자를 사용할 수 없습니다.', 'error')
      return
    }
    const state = useStore.getState()
    const hasUnsavedFile = state.openTabs.some((tab) =>
      isAffectedPath(tab.path, renaming.path, renaming.isDir) && state.changedFiles.has(tab.path),
    )
    if (hasUnsavedFile) {
      addToast('저장 중인 파일이 있습니다. 저장이 끝난 뒤 이름을 변경하세요.', 'error')
      return
    }
    const parent = renaming.path.substring(0, renaming.path.lastIndexOf('/'))
    const newPath = parent + '/' + trimmedName
    try {
      await apiFetch('/api/fs/rename', {
        method: 'POST',
        body: JSON.stringify({ from: renaming.path, to: newPath }),
      })
    } catch (error: unknown) {
      addToast(`이름 변경 실패: ${getErrorMessage(error)}`, 'error')
      setRenaming(null)
      return
    }

    closeAffectedTabs(renaming.path, renaming.isDir)
    setRenaming(null)
    if (dir) {
      try {
        await refreshFileTree(dir)
      } catch (error: unknown) {
        addToast(`이름은 변경됐지만 파일 목록을 갱신하지 못했습니다: ${getErrorMessage(error)}`, 'error')
      }
    }
  }

  async function handleDelete() {
    if (!ctxMenu) return
    const target = ctxMenu
    setCtxMenu(null)
    if (!window.confirm(`"${target.name}"을(를) 삭제하시겠습니까?`)) return
    try {
      await apiFetch('/api/fs/delete', {
        method: 'DELETE',
        body: JSON.stringify({ path: target.fullPath }),
      })
    } catch (error: unknown) {
      addToast(`삭제 실패: ${getErrorMessage(error)}`, 'error')
      return
    }

    closeAffectedTabs(target.fullPath, target.isDir)
    if (dir) {
      try {
        await refreshFileTree(dir)
      } catch (error: unknown) {
        addToast(`삭제는 완료됐지만 파일 목록을 갱신하지 못했습니다: ${getErrorMessage(error)}`, 'error')
      }
    }
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
