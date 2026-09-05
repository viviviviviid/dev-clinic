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
  focusedPath: string
  onFocusPath: (path: string) => void
  onContextMenu: (e: React.MouseEvent | React.KeyboardEvent, fullPath: string, name: string, isDir: boolean) => void
}

function TreeNode({ entry, rootDir, depth, focusedPath, onFocusPath, onContextMenu }: TreeNodeProps) {
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

  function handleKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.target !== event.currentTarget || event.nativeEvent.isComposing) return
    const node = event.currentTarget
    const items = Array.from(node.closest('[role="tree"]')?.querySelectorAll<HTMLElement>('[role="treeitem"]') ?? [])
    const index = items.indexOf(node)
    let target: HTMLElement | undefined | null
    if (event.key === 'ArrowDown') target = items[Math.min(index + 1, items.length - 1)]
    else if (event.key === 'ArrowUp') target = items[Math.max(index - 1, 0)]
    else if (event.key === 'Home') target = items[0]
    else if (event.key === 'End') target = items.at(-1)
    else if (event.key === 'ArrowRight') {
      if (entry.isDir && !open) setOpen(true)
      else target = node.querySelector<HTMLElement>('[role="group"] > [role="treeitem"]')
    } else if (event.key === 'ArrowLeft') {
      if (entry.isDir && open) setOpen(false)
      else target = node.parentElement?.closest<HTMLElement>('[role="treeitem"]')
    } else if (event.key === 'Enter' || event.key === ' ') void handleClick()
    else if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
      onContextMenu(event, fullPath, entry.name, entry.isDir)
    } else return
    event.preventDefault()
    event.stopPropagation()
    target?.focus()
  }

  return (
    <div className="tree-node" role="treeitem" aria-label={entry.name}
      aria-expanded={entry.isDir ? open : undefined} aria-selected={isActive}
      aria-level={depth + 1} tabIndex={focusedPath === entry.path ? 0 : -1}
      onFocus={(event) => { if (event.target === event.currentTarget) onFocusPath(entry.path) }}
      onKeyDown={handleKeyDown}>
      <div
        className={`tree-item ${isActive ? 'active' : ''} ${entry.isDir ? 'dir' : ''}`}
        style={{ paddingLeft: `${depth * 12 + 8}px` }}
        onClick={(event) => {
          event.currentTarget.parentElement?.focus()
          void handleClick()
        }}
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
        <div className="tree-children" role="group">
          {entry.children.map((child) => (
            <TreeNode
              key={child.path}
              entry={child}
              rootDir={rootDir}
              depth={depth + 1}
              focusedPath={focusedPath}
              onFocusPath={onFocusPath}
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
  const menuRef = useRef<HTMLDivElement>(null)
  const menuTriggerRef = useRef<HTMLElement | null>(null)
  const [focusedPath, setFocusedPath] = useState('')
  const hasPath = (entries: FileEntry[]): boolean => entries.some((entry) => entry.path === focusedPath || hasPath(entry.children ?? []))
  const focusPath = hasPath(fileTree) ? focusedPath : fileTree[0]?.path ?? ''

  // Close context menu on outside click
  useEffect(() => {
    if (!ctxMenu) return
    const handler = () => setCtxMenu(null)
    window.addEventListener('click', handler)
    return () => window.removeEventListener('click', handler)
  }, [ctxMenu])

  useEffect(() => {
    if (renaming) {
      renameInputRef.current?.focus()
      renameInputRef.current?.select()
    }
  }, [renaming])

  useEffect(() => {
    if (ctxMenu) menuRef.current?.querySelector('button')?.focus()
  }, [ctxMenu])

  function handleContextMenu(e: React.MouseEvent | React.KeyboardEvent, fullPath: string, name: string, isDir: boolean) {
    e.preventDefault()
    e.stopPropagation()
    menuTriggerRef.current = e.currentTarget.closest<HTMLElement>('[role="treeitem"]')
    menuTriggerRef.current?.focus()
    const rect = e.currentTarget.getBoundingClientRect()
    const x = 'clientX' in e ? e.clientX : rect.left + 24
    const y = 'clientY' in e ? e.clientY : rect.top + 28
    setCtxMenu({ x: Math.min(x, window.innerWidth - 160), y: Math.min(y, window.innerHeight - 100), fullPath, name, isDir })
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
      <div className="filetree-content" role="tree" aria-label="프로젝트 파일" aria-describedby="filetree-keyboard-help">
        {fileTree.map((entry) => (
          <TreeNode
            key={entry.path}
            entry={entry}
            rootDir={dir}
            depth={0}
            focusedPath={focusPath}
            onFocusPath={setFocusedPath}
            onContextMenu={handleContextMenu}
          />
        ))}
      </div>
      <p className="filetree-keyboard-help" id="filetree-keyboard-help">↑↓ 이동 · Enter 열기 · Shift+F10 메뉴</p>

      {/* Context menu */}
      {ctxMenu && (
        <div
          ref={menuRef}
          className="filetree-ctx-menu"
          role="menu" aria-label={`${ctxMenu.name} 작업`}
          style={{ top: ctxMenu.y, left: ctxMenu.x }}
          onClick={(e) => e.stopPropagation()}
          onKeyDown={(event) => {
            const buttons = Array.from(menuRef.current?.querySelectorAll('button') ?? [])
            if (event.key === 'Escape' || event.key === 'Tab') {
              event.preventDefault()
              setCtxMenu(null)
              menuTriggerRef.current?.focus()
            } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
              event.preventDefault()
              const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
              buttons[(index + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length]?.focus()
            }
          }}
        >
          <button className="ctx-menu-item" role="menuitem" onClick={startRename}>
            ✏️ 이름 변경
          </button>
          <button className="ctx-menu-item ctx-menu-danger" role="menuitem" onClick={handleDelete}>
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
