const REVIEWABLE_SOURCE_EXTENSIONS = new Set([
  '.go',
  '.ts',
  '.tsx',
  '.mts',
  '.cts',
  '.js',
  '.jsx',
  '.mjs',
  '.cjs',
  '.rs',
  '.sol',
  '.py',
])

const IGNORED_SOURCE_DIRECTORIES = new Set([
  '.git',
  '.snapshots',
  'node_modules',
  'vendor',
  'dist',
  'build',
  'target',
  '.venv',
  'venv',
  'coverage',
])

/** Mirrors the clinic watcher contract for files that receive sync_status. */
export function isReviewableSourcePath(path: string, projectDir = ''): boolean {
  const root = projectDir.replace(/\/+$/, '')
  const relativePath = root !== '' && path.startsWith(`${root}/`) ? path.slice(root.length + 1) : path
  const parts = relativePath.split('/')
  const filename = parts.pop()?.toLowerCase() ?? ''
  if (parts.some((part) => part !== '' && (part.startsWith('.') || IGNORED_SOURCE_DIRECTORIES.has(part)))) {
    return false
  }
  const dot = filename.lastIndexOf('.')
  return dot >= 0 && REVIEWABLE_SOURCE_EXTENSIONS.has(filename.slice(dot))
}
