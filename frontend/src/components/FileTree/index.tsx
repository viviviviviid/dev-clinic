import { useState } from 'react'
import { useStore, type FileEntry } from '../../store'
import { useProject } from '../../hooks/useProject'
import { lspClient } from '../../lib/lspClient'
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

interface TreeNodeProps {
  entry: FileEntry
  rootDir: string
  depth: number
}

function TreeNode({ entry, rootDir, depth }: TreeNodeProps) {
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
    // Notify LSP server that the file is open
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
  const { fileTree, projectStatus } = useStore()
  const dir = projectStatus?.dir || ''

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
          <TreeNode key={entry.path} entry={entry} rootDir={dir} depth={0} />
        ))}
      </div>
    </div>
  )
}
