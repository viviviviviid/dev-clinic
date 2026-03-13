import React, { useEffect, useRef, useCallback, useState } from 'react'
import MonacoEditor, { type OnMount, useMonaco } from '@monaco-editor/react'
import type { editor } from 'monaco-editor'
import { useStore } from '../../store'
import { useProject } from '../../hooks/useProject'
import { supabase } from '../../lib/supabase'
import { lspClient } from '../../lib/lspClient'
import type { Location as LspLocation, LspDiagnostic } from '../../lib/lspClient'
import type { DiagnosticItem } from '../../store'
import QuizOverlay from './QuizOverlay'
import ConceptPanel from './ConceptPanel'
import './Editor.css'

type DecorCollection = editor.IEditorDecorationsCollection

const LANG_MAP: Record<string, string> = {
  go: 'go',
  ts: 'typescript',
  tsx: 'typescriptreact',
  js: 'javascript',
  jsx: 'javascriptreact',
  rs: 'rust',
  sol: 'sol',
  py: 'python',
  md: 'markdown',
  json: 'json',
  toml: 'toml',
  yaml: 'yaml',
  yml: 'yaml',
}

function detectLanguage(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() || ''
  return LANG_MAP[ext] || 'plaintext'
}

const DECORATION_BASE = {
  hole: {
    isWholeLine: true,
    className: 'tutor-hole-line',
    glyphMarginClassName: 'tutor-hole-glyph',
  } as editor.IModelDecorationOptions,
  holeBlurred: {
    isWholeLine: true,
    className: 'tutor-hole-line',
    glyphMarginClassName: 'tutor-hole-glyph',
    inlineClassName: 'tutor-hole-text-blurred',
  } as editor.IModelDecorationOptions,
  holeLocked: {
    isWholeLine: true,
    className: 'tutor-hole-line-locked',
    glyphMarginClassName: 'tutor-hole-glyph-locked',
    inlineClassName: 'tutor-hole-text-blurred',
  } as editor.IModelDecorationOptions,
  bug: {
    isWholeLine: true,
    className: 'tutor-bug-line',
    glyphMarginClassName: 'tutor-bug-glyph',
  } as editor.IModelDecorationOptions,
}

function applyDecorations(
  editorInstance: editor.IStandaloneCodeEditor,
  content: string,
  filename: string,
  skillLevel: string,
  solvedHoles: Set<string>,
  collectionRef: React.MutableRefObject<DecorCollection | null>,
) {
  const model = editorInstance.getModel()
  if (!model) return

  const decorations: editor.IModelDeltaDecoration[] = []
  const lines = content.split('\n')
  let holeIndex = 0
  let firstUnsolvedHoleFound = false

  lines.forEach((line, idx) => {
    const lineNumber = idx + 1
    if (line.includes('[TUTOR:HOLE]')) {
      const newKey = `${filename}:hole:${holeIndex}`
      const legacyKey = `${filename}:${holeIndex}`
      const isSolved = solvedHoles.has(newKey) || solvedHoles.has(legacyKey)

      let opts = DECORATION_BASE.hole
      if (skillLevel === 'newbie' && !isSolved) {
        if (!firstUnsolvedHoleFound) {
          // 첫 번째 미해결 HOLE: 활성 (노란 블러)
          firstUnsolvedHoleFound = true
          opts = DECORATION_BASE.holeBlurred
        } else {
          // 이후 미해결 HOLE: 잠금 (회색 블러)
          opts = DECORATION_BASE.holeLocked
        }
      }

      decorations.push({
        range: { startLineNumber: lineNumber, startColumn: 1, endLineNumber: lineNumber, endColumn: line.length + 1 },
        options: opts,
      })
      holeIndex++
    }
    if (line.includes('[TUTOR:BUG]')) {
      decorations.push({
        range: { startLineNumber: lineNumber, startColumn: 1, endLineNumber: lineNumber, endColumn: line.length + 1 },
        options: DECORATION_BASE.bug,
      })
    }
  })

  if (collectionRef.current) collectionRef.current.clear()
  collectionRef.current = editorInstance.createDecorationsCollection(decorations)
}

function replaceHoleAtIndex(content: string, holeIndex: number, correctCode: string): string {
  const lines = content.split('\n')
  let count = 0
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].includes('[TUTOR:HOLE]')) {
      if (count === holeIndex) { lines[i] = correctCode; break }
      count++
    }
  }
  return lines.join('\n')
}

function replaceBugAtIndex(content: string, bugIndex: number, correctCode: string): string {
  const lines = content.split('\n')
  let count = 0
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].includes('[TUTOR:BUG]')) {
      if (count === bugIndex) {
        lines.splice(i, i + 1 < lines.length ? 2 : 1, correctCode)
        break
      }
      count++
    }
  }
  return lines.join('\n')
}

// Resolve an LSP Location (or array) into a Monaco definition, loading cross-file content as needed
async function resolveDefinition(
  lspResult: LspLocation | LspLocation[],
  monaco: typeof import('monaco-editor'),
  addTab: (path: string, content: string) => void,
  setOpenFileReadOnly: (v: boolean) => void,
  projectDir: string,
): Promise<import('monaco-editor').languages.Definition | null> {
  const locations = Array.isArray(lspResult) ? lspResult : [lspResult]
  if (!locations.length) return null

  const results: import('monaco-editor').languages.Location[] = []

  for (const loc of locations) {
    const uri = monaco.Uri.parse(loc.uri)
    const { start, end } = loc.range
    // LSP 0-based → Monaco 1-based
    const range = {
      startLineNumber: start.line + 1,
      startColumn: start.character + 1,
      endLineNumber: end.line + 1,
      endColumn: end.character + 1,
    }

    // Ensure the model exists
    let model = monaco.editor.getModel(uri)
    if (!model) {
      // Need to load the file content
      const absPath = uri.fsPath || uri.path
      try {
        const content = await readFileViaApi(absPath)
        model = monaco.editor.createModel(content, undefined, uri)
        // Navigate to the file in the editor via tab
        const isOutsideProject = projectDir && !absPath.startsWith(projectDir)
        setOpenFileReadOnly(!!isOutsideProject)
        addTab(absPath, content)
      } catch {
        continue
      }
    }

    results.push({ uri, range })
  }

  return results.length === 1 ? results[0] : results.length > 1 ? results : null
}

// ANSI 색상 코드를 HTML span으로 변환 (최소한: 빨강/초록/리셋)
function ansiToHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/\x1b\[31m/g, '<span class="run-stderr">')
    .replace(/\x1b\[32m/g, '<span class="run-success">')
    .replace(/\x1b\[0m/g, '</span>')
    .replace(/\x1b\[[0-9;]*m/g, '') // 나머지 ANSI 코드 제거
}

// Read a file via API (usable outside React hooks, e.g. in Monaco provider callbacks)
async function readFileViaApi(absPath: string): Promise<string> {
  const { data: { session } } = await supabase.auth.getSession()
  const headers: Record<string, string> = {}
  if (session?.access_token) headers['Authorization'] = `Bearer ${session.access_token}`
  const res = await fetch(`/api/fs/read?path=${encodeURIComponent(absPath)}`, { headers })
  if (!res.ok) throw new Error(`readFile failed: ${res.status}`)
  const data = await res.json()
  return data.content ?? ''
}

// Standalone Go text-search fallback (same-file only)
function textSearchFallback(
  model: import('monaco-editor').editor.ITextModel,
  position: import('monaco-editor').Position,
): import('monaco-editor').languages.Definition | null {
  const word = model.getWordAtPosition(position)
  if (!word) return null
  const name = word.word
  const lines = model.getLinesContent()
  const patterns = [
    new RegExp(`^\\s*func\\s+(?:\\([^)]*\\)\\s+)?${name}\\s*[\\[\\(]`),
    new RegExp(`^\\s*type\\s+${name}\\s`),
    new RegExp(`^\\s*(?:var\\s+)?${name}\\s+[A-Z*\\[\\]]`),
    new RegExp(`^\\s*(?:const\\s+)?${name}\\s*=`),
    new RegExp(`^\\s*${name}\\s`),
  ]
  for (let i = 0; i < lines.length; i++) {
    const lineNum = i + 1
    if (lineNum === position.lineNumber) continue
    for (const pat of patterns) {
      if (pat.test(lines[i])) {
        const col = lines[i].indexOf(name) + 1
        return {
          uri: model.uri,
          range: { startLineNumber: lineNum, startColumn: col, endLineNumber: lineNum, endColumn: col + name.length },
        }
      }
    }
  }
  return null
}

export default function Editor() {
  const {
    openFile,
    openFileContent,
    openFileReadOnly,
    setOpenFileContent,
    setOpenFileReadOnly,
    markFileChanged,
    markFileSaved,
    skillLevel,
    quizData,
    solvedHoles,
    markHoleSolved,
    projectStatus,
    openTabs,
    addTab,
    closeTab,
    changedFiles,
    setDiagnostics,
    pendingNavigate,
    setPendingNavigate,
  } = useStore()
  const { writeFile } = useProject()
  const writeFileRef = useRef(writeFile)
  useEffect(() => { writeFileRef.current = writeFile }, [writeFile])
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null)
  const decorationRef = useRef<DecorCollection | null>(null)
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [editorInstance, setEditorInstance] = useState<editor.IStandaloneCodeEditor | null>(null)
  const monacoInstance = useMonaco()
  const [lspReady, setLspReady] = useState(lspClient.isReady)
  // Track the currently open file path so providers can use it regardless of model URI
  const openFileRef = useRef<string | null>(openFile)
  const openFileContentRef = useRef<string>(openFileContent)
  const diagnosticsCacheRef = useRef<Map<string, import('monaco-editor').editor.IMarkerData[]>>(new Map())

  useEffect(() => { openFileRef.current = openFile }, [openFile])
  useEffect(() => { openFileContentRef.current = openFileContent }, [openFileContent])

  // 파일이 열릴 때 캐시된 진단 적용은 handleMount에서 처리
  useEffect(() => {
    setLspReady(lspClient.isReady)
    return lspClient.onStatusChange(setLspReady)
  }, [])

  // LSP diagnostics → Monaco markers (빨간/노란 밑줄)
  useEffect(() => {
    if (!monacoInstance) return
    return lspClient.onDiagnostics((uri, diagnostics) => {
      // uri === '*' means disconnect — clear all markers
      if (uri === '*') {
        monacoInstance.editor.getModels().forEach(m => {
          monacoInstance.editor.setModelMarkers(m, 'lsp', [])
        })
        diagnosticsCacheRef.current.clear()
        setDiagnostics([])
        return
      }

      const S = monacoInstance.MarkerSeverity
      const markers = diagnostics.map((d: LspDiagnostic) => ({
        startLineNumber: d.range.start.line + 1,
        startColumn: d.range.start.character + 1,
        endLineNumber: d.range.end.line + 1,
        endColumn: d.range.end.character + 1,
        message: d.message,
        severity: d.severity === 1 ? S.Error
          : d.severity === 2 ? S.Warning
          : d.severity === 3 ? S.Info
          : S.Hint,
        source: d.source,
      }))

      // 항상 캐시에 저장 (파일이 아직 열리지 않았을 수 있음)
      diagnosticsCacheRef.current.set(uri, markers)

      // URI로 모델 찾기, 실패 시 현재 열린 파일과 URI가 같으면 editorRef 사용
      const monacoUri = monacoInstance.Uri.parse(uri)
      const model = monacoInstance.editor.getModel(monacoUri)
        ?? (openFileRef.current && uri === `file://${openFileRef.current}` ? editorRef.current?.getModel() ?? null : null)
      if (model) {
        monacoInstance.editor.setModelMarkers(model, 'lsp', markers)
      }
    })
  }, [monacoInstance, setDiagnostics])

  // Block browser Cmd+S / Ctrl+S save dialog
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 's') {
        e.preventDefault()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  // Register LSP-backed definition + completion providers for all supported languages
  useEffect(() => {
    if (!monacoInstance) return

    const LANGS = ['go', 'typescript', 'typescriptreact', 'javascript', 'javascriptreact', 'python', 'sol', 'rust']

    // Trigger characters per language
    const TRIGGER_CHARS: Partial<Record<string, string[]>> = {
      go: ['.', '"', '/'],
      typescript: ['.', '"', '/', '<'],
      typescriptreact: ['.', '"', '/', '<'],
      javascript: ['.', '"', '/'],
      javascriptreact: ['.', '"', '/'],
      python: ['.', '"'],
      rust: ['.', ':'],
    }

    // Map LSP CompletionItemKind → Monaco CompletionItemKind
    function lspKindToMonaco(lspKind: number): number {
      const K = monacoInstance!.languages.CompletionItemKind
      const map: Record<number, number> = {
        1: K.Text, 2: K.Method, 3: K.Function, 4: K.Constructor,
        5: K.Field, 6: K.Variable, 7: K.Class, 8: K.Interface,
        9: K.Module, 10: K.Property, 11: K.Unit, 12: K.Value,
        13: K.Enum, 14: K.Keyword, 15: K.Snippet, 16: K.Color,
        17: K.File, 18: K.Reference, 19: K.Folder, 20: K.EnumMember,
        21: K.Constant, 22: K.Struct, 23: K.Event, 24: K.Operator,
        25: K.TypeParameter,
      }
      return map[lspKind] ?? K.Text
    }

    const disposables = LANGS.flatMap((lang) => [
      // Formatting provider (Cmd+S → LSP textDocument/formatting)
      monacoInstance.languages.registerDocumentFormattingEditProvider(lang, {
        async provideDocumentFormattingEdits(_model) {
          const filePath = openFileRef.current
          if (!filePath || !lspClient.isReady) return []
          const edits = await lspClient.formatting(filePath).catch(() => null)
          if (!edits || edits.length === 0) return []
          return edits.map((e) => ({
            range: {
              startLineNumber: e.range.start.line + 1,
              startColumn: e.range.start.character + 1,
              endLineNumber: e.range.end.line + 1,
              endColumn: e.range.end.character + 1,
            },
            text: e.newText,
          }))
        },
      }),

      // Definition provider
      monacoInstance.languages.registerDefinitionProvider(lang, {
        async provideDefinition(model, position) {
          if (lspClient.isReady) {
            const filePath = openFileRef.current || model.uri.fsPath || model.uri.path
            const lspResult = await lspClient.definition(
              filePath,
              position.lineNumber - 1,
              position.column - 1,
            ).catch(() => null)

            if (lspResult) {
              return resolveDefinition(lspResult, monacoInstance, addTab, setOpenFileReadOnly, projectStatus?.dir ?? '')
            }
          }

          if (lang === 'go') return textSearchFallback(model, position)
          return null
        },
      }),

      // Hover provider
      monacoInstance.languages.registerHoverProvider(lang, {
        async provideHover(_model, position) {
          if (!lspClient.isReady) return null
          const filePath = openFileRef.current
          if (!filePath) return null
          const result = await lspClient.hover(
            filePath,
            position.lineNumber - 1,
            position.column - 1,
          ).catch(() => null)
          if (!result) return null
          const c = result.contents
          let value = ''
          if (typeof c === 'string') {
            value = c
          } else if (Array.isArray(c)) {
            value = c.map(item => typeof item === 'string' ? item : item.value).join('\n\n')
          } else if (typeof c === 'object' && 'value' in c) {
            value = (c as { value: string }).value
          }
          if (!value) return null
          return { contents: [{ value }] }
        },
      }),

      // Completion provider
      monacoInstance.languages.registerCompletionItemProvider(lang, {
        triggerCharacters: TRIGGER_CHARS[lang] ?? ['.'],
        async provideCompletionItems(model, position, context) {
          if (!lspClient.isReady) return null
          const filePath = openFileRef.current || model.uri.fsPath || model.uri.path
          // Ensure gopls has the exact current content before requesting completions.
          // Use model.getValue() rather than openFileContentRef.current because React
          // state updates are async: the ref may still hold stale content (without the
          // just-typed trigger character), which would revert gopls to an old version
          // and cause it to return empty completions.
          if (filePath && openFileRef.current) {
            await lspClient.notifyOpen(filePath, model.getValue(), lang)
          }
          const raw = await lspClient.completion(
            filePath,
            position.lineNumber - 1,
            position.column - 1,
            context.triggerCharacter,
          ).catch(() => null)

          if (!raw) return null
          const items = Array.isArray(raw) ? raw : (raw.items ?? [])
          const word = model.getWordUntilPosition(position)
          // When triggered by a character (e.g. '.'), start the replace range
          // from the current cursor position so Monaco's filter text is empty
          // and all server-returned items are shown. Otherwise use the word
          // start so typed characters narrow the list correctly.
          const startColumn = context.triggerCharacter ? position.column : word.startColumn
          const range = {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn,
            endColumn: position.column,
          }

          return {
            suggestions: items.map((item) => {
              const label = typeof item.label === 'string' ? item.label : item.label.label
              const insertText = item.insertText ?? label
              const doc = typeof item.documentation === 'string'
                ? item.documentation
                : item.documentation?.value ?? ''
              return {
                label,
                kind: lspKindToMonaco(item.kind ?? 1),
                detail: item.detail ?? '',
                documentation: doc,
                insertText,
                insertTextRules: item.insertTextFormat === 2
                  ? monacoInstance.languages.CompletionItemInsertTextRule.InsertAsSnippet
                  : undefined,
                filterText: item.filterText ?? label,
                sortText: item.sortText ?? label,
                range,
              }
            }),
          }
        },
      }),
    ])

    return () => disposables.forEach((d) => d.dispose())
  }, [monacoInstance, addTab, setOpenFileReadOnly, projectStatus?.dir])

  // Monaco 마커 변경 → setDiagnostics
  useEffect(() => {
    if (!monacoInstance) return
    const d = monacoInstance.editor.onDidChangeMarkers(() => {
      const allItems: DiagnosticItem[] = []
      monacoInstance.editor.getModels().forEach(model => {
        const markers = monacoInstance.editor.getModelMarkers({ resource: model.uri })
        markers.forEach(m => {
          const filePath = model.uri.fsPath || model.uri.path
          allItems.push({
            filePath,
            fileName: filePath.split('/').pop() || filePath,
            message: m.message,
            severity: m.severity as 1 | 2 | 3 | 4,
            startLine: m.startLineNumber,
            startColumn: m.startColumn,
          })
        })
      })
      setDiagnostics(allItems)
    })
    return () => d.dispose()
  }, [monacoInstance, setDiagnostics])

  // pendingNavigate → 에디터 위치 이동
  useEffect(() => {
    if (!pendingNavigate || !editorRef.current) return
    const { line, column } = pendingNavigate
    editorRef.current.revealLineInCenter(line)
    editorRef.current.setPosition({ lineNumber: line, column })
    editorRef.current.focus()
    setPendingNavigate(null)
  }, [pendingNavigate, setPendingNavigate])

  // Concept panel
  const [showConcept, setShowConcept] = useState(false)
  const [isRunning, setIsRunning] = useState(false)
  const [isTesting, setIsTesting] = useState(false)
  const [runOutput, setRunOutput] = useState<string[]>([])
  const [outputTitle, setOutputTitle] = useState('실행 결과')
  const [showOutput, setShowOutput] = useState(false)
  const outputEndRef = useRef<HTMLDivElement>(null)

  const filename = openFile ? openFile.split('/').pop() || openFile : ''

  const handleMount: OnMount = useCallback((ed, monaco) => {
    editorRef.current = ed
    setEditorInstance(ed)
    if (openFileContent) {
      applyDecorations(ed, openFileContent, filename, skillLevel, solvedHoles, decorationRef)
    }

    // 캐시된 진단 적용 (gopls가 파일 열기 전에 진단을 보냈을 때)
    const model = ed.getModel()
    if (model && openFileRef.current) {
      const uri = `file://${openFileRef.current}`
      const cached = diagnosticsCacheRef.current.get(uri)
      if (cached) {
        monaco.editor.setModelMarkers(model, 'lsp', cached)
      }
    }

    // Cmd+S / Ctrl+S: format then save
    ed.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, async () => {
      await ed.getAction('editor.action.formatDocument')?.run()
      const content = ed.getValue()
      const filePath = openFileRef.current
      if (filePath) {
        await writeFileRef.current(filePath, content)
        markFileSaved(filePath)
      }
    })
  }, [])

  useEffect(() => {
    if (editorRef.current && openFileContent !== undefined) {
      applyDecorations(editorRef.current, openFileContent, filename, skillLevel, solvedHoles, decorationRef)
    }
  }, [openFile, openFileContent, skillLevel, solvedHoles])

  // 출력 추가될 때마다 하단 스크롤
  useEffect(() => {
    outputEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [runOutput])

  function handleChange(value: string | undefined) {
    const content = value || ''
    setOpenFileContent(content)
    if (openFile) markFileChanged(openFile)
    // Notify LSP of content change for accurate completions
    if (openFile) lspClient.notifyChange(openFile, content)
    if (saveTimer.current) clearTimeout(saveTimer.current)
    saveTimer.current = setTimeout(async () => {
      if (openFile) {
        await writeFile(openFile, content)
        markFileSaved(openFile)
      }
    }, 500)
  }

  async function handleQuizSolve(key: string, correctCode: string, markerType: string, markerIndex: number) {
    markHoleSolved(key)
    const newContent = markerType === 'bug'
      ? replaceBugAtIndex(openFileContent, markerIndex, correctCode)
      : replaceHoleAtIndex(openFileContent, markerIndex, correctCode)
    setOpenFileContent(newContent)
    if (openFile) await writeFile(openFile, newContent)
  }

  async function streamOutput(endpoint: string, title: string, setActive: (v: boolean) => void) {
    setActive(true)
    setRunOutput([])
    setOutputTitle(title)
    setShowOutput(true)

    try {
      const { data: { session } } = await supabase.auth.getSession()
      const runHeaders: Record<string, string> = {}
      if (session?.access_token) runHeaders['Authorization'] = `Bearer ${session.access_token}`
      const response = await fetch(endpoint, { headers: runHeaders })
      console.log(`[streamOutput] ${endpoint} → status=${response.status}, ok=${response.ok}`)
      if (!response.ok || !response.body) {
        setRunOutput([`[오류] 요청 실패 (${response.status})`])
        setActive(false)
        return
      }

      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })

        const chunks = buffer.split('\n\n')
        buffer = chunks.pop() ?? ''

        for (const chunk of chunks) {
          if (!chunk.trim()) continue
          console.log('[sse chunk]', JSON.stringify(chunk))
          if (chunk.startsWith('event: done')) {
            setActive(false)
            continue
          }
          const match = chunk.match(/^data: (.*)$/m)
          if (match) {
            setRunOutput((prev) => [...prev, match[1]])
          }
        }
      }
    } catch (e: any) {
      setRunOutput((prev) => [...prev, '[오류] ' + e.message])
    } finally {
      setActive(false)
    }
  }

  async function handleRun() {
    if (isRunning || isTesting) return
    streamOutput('/api/run', '실행 결과', setIsRunning)
  }

  async function handleTest() {
    if (isRunning || isTesting) return
    console.log('[test] button clicked, starting streamOutput')
    streamOutput('/api/test', '테스트 결과', setIsTesting)
  }

  if (!openFile) {
    return (
      <div className="editor-empty">
        <div className="editor-empty-content">
          <span className="editor-empty-icon">{'</>'}</span>
          <p>파일을 선택하면 에디터가 열립니다</p>
          <p className="editor-hint">
            <span className="hint-yellow">■</span> HOLE: 구현할 부분 &nbsp;
            <span className="hint-red">■</span> BUG: 버그가 있는 부분
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="editor-container">
      {/* 다중 탭 바 */}
      <div className="editor-tabs-bar">
        <div className="editor-tabs-list">
          {openTabs.map(tab => {
            const tabName = tab.path.split('/').pop() || tab.path
            const isActive = tab.path === openFile
            const isUnsaved = changedFiles.has(tab.path)
            return (
              <div
                key={tab.path}
                className={`editor-tab-item${isActive ? ' active' : ''}`}
                onClick={() => addTab(tab.path, tab.content)}
                title={tab.path}
              >
                {isUnsaved && <span className="tab-unsaved">●</span>}
                <span className="editor-tab-name">{tabName}</span>
                <button
                  className="tab-close-btn"
                  onClick={(e) => { e.stopPropagation(); closeTab(tab.path) }}
                  title="탭 닫기"
                >✕</button>
              </div>
            )
          })}
        </div>
        <div className="editor-tab-actions">
          <button
            className={`run-btn ${isRunning ? 'running' : ''}`}
            onClick={handleRun}
            disabled={isRunning || isTesting}
            title="코드 실행 (▶)"
          >
            {isRunning ? (
              <><span className="run-spinner" /> 실행 중...</>
            ) : (
              <>▶ 실행</>
            )}
          </button>
          <button
            className={`run-btn test-btn ${isTesting ? 'running' : ''}`}
            onClick={handleTest}
            disabled={isRunning || isTesting}
            title="테스트 실행"
          >
            {isTesting ? (
              <><span className="run-spinner" /> 테스트 중...</>
            ) : (
              <>✓ 테스트</>
            )}
          </button>
          <span
            className={`lsp-status ${lspReady ? 'lsp-ready' : 'lsp-off'}`}
            title={lspReady ? 'LSP 연결됨 (자동완성 활성)' : 'LSP 미연결 (gopls 설치 필요)'}
          >
            ● LSP
          </span>
          <button
            className={`concept-btn ${showConcept ? 'active' : ''}`}
            onClick={() => setShowConcept((v) => !v)}
            title="개념 설명 보기"
          >
            📖 개념
          </button>
          {showOutput && (
            <button
              className="run-output-toggle"
              onClick={() => setShowOutput((v) => !v)}
              title="출력 패널 토글"
            >
              {showOutput ? '⌄ 출력' : '⌃ 출력'}
            </button>
          )}
        </div>
      </div>

      {/* Monaco + Quiz overlay */}
      <div className="editor-monaco-wrapper">
        <MonacoEditor
          key={openFile}
          path={openFile}
          value={openFileContent}
          language={detectLanguage(openFile)}
          theme="vs-dark"
          height="100%"
          width="100%"
          onChange={handleChange}
          onMount={handleMount}
          options={{
            fontSize: 14,
            fontFamily: "'Consolas', 'Courier New', monospace",
            lineNumbers: 'on',
            minimap: { enabled: false },
            scrollBeyondLastLine: true,
            wordWrap: 'on',
            glyphMargin: true,
            folding: true,
            renderLineHighlight: 'line',
            tabSize: 2,
            insertSpaces: true,
            padding: { top: 8 },
            readOnly: openFileReadOnly,
            quickSuggestions: { other: true, comments: false, strings: true },
            suggestOnTriggerCharacters: true,
            acceptSuggestionOnCommitCharacter: true,
            wordBasedSuggestions: 'off',
          }}
        />
        {skillLevel === 'newbie' && editorInstance && openFile && (
          <QuizOverlay
            editor={editorInstance}
            filename={filename}
            content={openFileContent}
            quizData={quizData}
            solvedHoles={solvedHoles}
            onSolve={handleQuizSolve}
          />
        )}
        {showConcept && (
          <ConceptPanel
            concept={projectStatus?.concept ?? ''}
            tasks={projectStatus?.tasks ?? ''}
            onClose={() => setShowConcept(false)}
          />
        )}
      </div>

      {/* 실행 출력 패널 */}
      {showOutput && (
        <div className="run-output-panel">
          <div className="run-output-header">
            <span>{outputTitle}</span>
            {(isRunning || isTesting) && <span className="run-output-running">● 실행 중</span>}
            <button className="run-output-close" onClick={() => setShowOutput(false)}>✕</button>
          </div>
          <div className="run-output-body">
            {runOutput.length === 0 && (isRunning || isTesting) && (
              <span className="run-output-waiting">출력 대기 중...</span>
            )}
            {runOutput.map((line, i) => (
              <div
                key={i}
                className="run-output-line"
                dangerouslySetInnerHTML={{ __html: ansiToHtml(line) }}
              />
            ))}
            <div ref={outputEndRef} />
          </div>
        </div>
      )}
    </div>
  )
}
