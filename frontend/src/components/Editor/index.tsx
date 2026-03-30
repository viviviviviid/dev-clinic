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
import { LOCAL } from '../../lib/api'

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

function replaceHoleAtIndex(content: string, holeIndex: number, code: string): string {
  const lines = content.split('\n')
  let count = 0
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].includes('[TUTOR:HOLE]')) {
      if (count === holeIndex) {
        // Remove marker line + all following comment-only lines (hint block)
        let end = i
        while (end + 1 < lines.length && lines[end + 1].trimStart().startsWith('//')) end++
        const indent = lines[i].match(/^(\s*)/)?.[1] ?? ''
        const indented = code.split('\n').map(l => indent + l.trimStart()).join('\n')
        lines.splice(i, end - i + 1, indented)
        break
      }
      count++
    }
  }
  return lines.join('\n')
}

function replaceBugAtIndex(content: string, bugIndex: number, code: string): string {
  const lines = content.split('\n')
  let count = 0
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].includes('[TUTOR:BUG]')) {
      if (count === bugIndex) {
        // Remove marker line + all following comment-only lines, then remove the buggy code line
        let end = i
        while (end + 1 < lines.length && lines[end + 1].trimStart().startsWith('//')) end++
        // end+1 is the actual buggy code line — remove it too
        const totalToRemove = (end - i + 1) + (end + 1 < lines.length ? 1 : 0)
        const indent = lines[i].match(/^(\s*)/)?.[1] ?? ''
        const indented = code.split('\n').map(l => indent + l.trimStart()).join('\n')
        lines.splice(i, totalToRemove, indented)
        break
      }
      count++
    }
  }
  return lines.join('\n')
}

// LSP Location → Monaco Location 변환 + 모델 사전 생성 (순수 함수, 이동 없음)
// provideDefinition에서만 사용 — Monaco가 link decoration/peek을 위해 호출할 때 side effect 없어야 함
async function resolveDefinition(
  lspResult: LspLocation | LspLocation[],
  monaco: typeof import('monaco-editor'),
): Promise<import('monaco-editor').languages.Definition | null> {
  const locations = Array.isArray(lspResult) ? lspResult : [lspResult]
  if (!locations.length) return null

  const results: import('monaco-editor').languages.Location[] = []

  for (const loc of locations) {
    const uri = monaco.Uri.parse(loc.uri)
    const { start, end } = loc.range
    const range = {
      startLineNumber: start.line + 1,
      startColumn: start.character + 1,
      endLineNumber: end.line + 1,
      endColumn: end.character + 1,
    }

    // 모델이 없으면 미리 생성 (Monaco가 peek/link decoration에서 내용을 표시할 수 있도록)
    if (!monaco.editor.getModel(uri)) {
      try {
        const absPath = uri.fsPath || uri.path
        const content = await readFileViaApi(absPath)
        monaco.editor.createModel(content, detectLanguage(absPath), uri)
      } catch { continue }
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
  const res = await fetch(`${LOCAL}/api/fs/read?path=${encodeURIComponent(absPath)}`, { headers })
  if (!res.ok) throw new Error(`readFile failed: ${res.status}`)
  const data = await res.json()
  return data.content ?? ''
}

async function fetchGitDiff(filePath: string, projectDir: string): Promise<Array<{lineNum: number; type: string}>> {
  const { data: { session } } = await supabase.auth.getSession()
  const headers: Record<string, string> = {}
  if (session?.access_token) headers['Authorization'] = `Bearer ${session.access_token}`
  try {
    const res = await fetch(
      `/api/fs/git-diff?path=${encodeURIComponent(filePath)}&dir=${encodeURIComponent(projectDir)}`,
      { headers }
    )
    if (!res.ok) return []
    return await res.json()
  } catch { return [] }
}

// Detect test functions in a file by path + content (regex-based, no LSP needed)
function getTestFunctions(filePath: string, content: string): Array<{ name: string; line: number }> {
  const results: Array<{ name: string; line: number }> = []
  const filename = filePath.split('/').pop() || ''
  const lines = content.split('\n')

  if (filename.endsWith('_test.go')) {
    lines.forEach((line, i) => {
      const m = line.match(/^func (Test\w+)\(/)
      if (m) results.push({ name: m[1], line: i + 1 })
    })
  } else if (/^test_.*\.py$/.test(filename) || /.*_test\.py$/.test(filename)) {
    lines.forEach((line, i) => {
      const m = line.match(/^def (test_\w+)\(/)
      if (m) results.push({ name: m[1], line: i + 1 })
    })
  } else if (filename.endsWith('.rs')) {
    for (let i = 0; i < lines.length; i++) {
      if (lines[i].trim() === '#[test]' && i + 1 < lines.length) {
        const m = lines[i + 1].match(/fn (\w+)\(/)
        if (m) results.push({ name: m[1], line: i + 2 })
      }
    }
  } else if (/\.(test|spec)\.(ts|tsx|js|jsx)$/.test(filename)) {
    lines.forEach((line, i) => {
      const m = line.match(/^\s*(?:test|it)\(['"`]([^'"`]+)/)
      if (m) results.push({ name: m[1], line: i + 1 })
    })
  }

  return results
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
    showMinimap,
  } = useStore()
  const { writeFile } = useProject()
  const writeFileRef = useRef(writeFile)
  useEffect(() => { writeFileRef.current = writeFile }, [writeFile])
  const handleTestFuncRef = useRef<(name: string) => void>(() => {})
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null)
  const decorationRef = useRef<DecorCollection | null>(null)
  const gitDiffCollectionRef = useRef<DecorCollection | null>(null)
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [editorInstance, setEditorInstance] = useState<editor.IStandaloneCodeEditor | null>(null)
  // definition 이동 대상 위치 (cross-file): state 대신 ref로 관리해 React 렌더 사이클과 분리
  const pendingNavigateRef = useRef<{ path: string; line: number; column: number } | null>(null)
  // cross-file 이동 함수 ref — Cmd+click/F12에서만 호출 (hover 시 절대 호출 안 함)
  const navigateCrossFileRef = useRef<(absPath: string, line: number, col: number) => Promise<void>>(async () => {})
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
          // 프로젝트 외부 파일(stdlib, 패키지 캐시 등)에서는 정의 이동 비활성화 (연쇄 이동 방지)
          const currentPath = openFileRef.current || model.uri.fsPath || model.uri.path
          const dir = projectStatus?.dir ?? ''
          if (dir && !currentPath.startsWith(dir)) return null

          if (lspClient.isReady) {
            const filePath = currentPath
            const lspResult = await lspClient.definition(
              filePath,
              position.lineNumber - 1,
              position.column - 1,
            ).catch(() => null)

            if (lspResult) {
              return resolveDefinition(lspResult, monacoInstance)
            }
          }

          if (lang === 'go') return textSearchFallback(model, position)
          return null
        },
      }),

      // References provider (Shift+F12 → 사용처 목록 peek 패널)
      monacoInstance.languages.registerReferenceProvider(lang, {
        async provideReferences(model, position, context) {
          if (!lspClient.isReady) return []
          const filePath = openFileRef.current || model.uri.fsPath || model.uri.path
          const lspResult = await lspClient.references(
            filePath,
            position.lineNumber - 1,
            position.column - 1,
            context.includeDeclaration,
          ).catch(() => null)
          if (!lspResult || !lspResult.length) return []

          // 각 참조 파일의 모델을 미리 생성 (Monaco peek 패널이 내용을 표시할 수 있도록)
          const results: import('monaco-editor').languages.Location[] = []
          for (const loc of lspResult) {
            const uri = monacoInstance.Uri.parse(loc.uri)
            if (!monacoInstance.editor.getModel(uri)) {
              try {
                const absPath = uri.fsPath || uri.path
                const content = await readFileViaApi(absPath)
                monacoInstance.editor.createModel(content, detectLanguage(absPath), uri)
              } catch { continue }
            }
            results.push({
              uri,
              range: {
                startLineNumber: loc.range.start.line + 1,
                startColumn: loc.range.start.character + 1,
                endLineNumber: loc.range.end.line + 1,
                endColumn: loc.range.end.character + 1,
              },
            })
          }
          return results
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

      // Signature Help provider
      monacoInstance.languages.registerSignatureHelpProvider(lang, {
        signatureHelpTriggerCharacters: ['(', ','],
        signatureHelpRetriggerCharacters: [','],
        async provideSignatureHelp(_model, position) {
          if (!lspClient.isReady) return null
          const filePath = openFileRef.current
          if (!filePath) return null
          const result = await lspClient.signatureHelp(
            filePath,
            position.lineNumber - 1,
            position.column - 1,
          ).catch(() => null)
          if (!result?.signatures?.length) return null
          return {
            value: {
              signatures: result.signatures.map((sig: any) => ({
                label: sig.label,
                documentation: typeof sig.documentation === 'string'
                  ? sig.documentation
                  : sig.documentation?.value ?? '',
                parameters: (sig.parameters ?? []).map((p: any) => ({
                  label: p.label,
                  documentation: typeof p.documentation === 'string'
                    ? p.documentation
                    : p.documentation?.value ?? '',
                })),
              })),
              activeSignature: result.activeSignature ?? 0,
              activeParameter: result.activeParameter ?? 0,
            },
            dispose() {},
          }
        },
      }),

      // Code Action provider
      monacoInstance.languages.registerCodeActionProvider(lang, {
        async provideCodeActions(model, range, context) {
          if (!lspClient.isReady) return { actions: [], dispose() {} }
          const filePath = openFileRef.current || model.uri.fsPath || model.uri.path
          const diags = context.markers.map(m => ({
            range: {
              start: { line: m.startLineNumber - 1, character: m.startColumn - 1 },
              end: { line: m.endLineNumber - 1, character: m.endColumn - 1 },
            },
            message: m.message,
            severity: m.severity,
          }))
          const actions = await lspClient.codeAction(
            filePath,
            {
              startLine: range.startLineNumber - 1,
              startChar: range.startColumn - 1,
              endLine: range.endLineNumber - 1,
              endChar: range.endColumn - 1,
            },
            diags,
          ).catch(() => [])
          return {
            actions: actions.map((a: any) => ({
              title: a.title,
              kind: a.kind ?? '',
              isPreferred: a.isPreferred ?? false,
              command: a.command
                ? { id: a.command.command, title: a.command.title, arguments: a.command.arguments }
                : undefined,
            })),
            dispose() {},
          }
        },
      }),

      // Rename provider
      monacoInstance.languages.registerRenameProvider(lang, {
        async provideRenameEdits(model, position, newName) {
          if (!lspClient.isReady) return null
          const filePath = openFileRef.current || model.uri.fsPath || model.uri.path
          const result = await lspClient.rename(
            filePath,
            position.lineNumber - 1,
            position.column - 1,
            newName,
          ).catch(() => null)
          if (!result) return null
          const edits: import('monaco-editor').languages.IWorkspaceTextEdit[] = []
          const changes = result.changes ?? {}
          for (const [uri, textEdits] of Object.entries(changes) as [string, any[]][]) {
            for (const te of textEdits) {
              edits.push({
                resource: monacoInstance.Uri.parse(uri),
                textEdit: {
                  range: {
                    startLineNumber: te.range.start.line + 1,
                    startColumn: te.range.start.character + 1,
                    endLineNumber: te.range.end.line + 1,
                    endColumn: te.range.end.character + 1,
                  },
                  text: te.newText,
                },
                versionId: undefined,
              })
            }
          }
          return { edits }
        },
      }),

      // Inlay Hints provider
      monacoInstance.languages.registerInlayHintsProvider(lang, {
        async provideInlayHints(_model, range) {
          if (!lspClient.isReady) return { hints: [], dispose() {} }
          const filePath = openFileRef.current
          if (!filePath) return { hints: [], dispose() {} }
          const hints = await lspClient.inlayHints(
            filePath,
            range.startLineNumber - 1,
            range.endLineNumber - 1,
          ).catch(() => null)
          if (!hints?.length) return { hints: [], dispose() {} }
          return {
            hints: hints.map((h: any) => ({
              position: { lineNumber: h.position.line + 1, column: h.position.character + 1 },
              label: typeof h.label === 'string'
                ? h.label
                : h.label.map((p: any) => p.value).join(''),
              kind: h.kind === 1
                ? monacoInstance.languages.InlayHintKind.Type
                : monacoInstance.languages.InlayHintKind.Parameter,
              paddingLeft: h.paddingLeft ?? false,
              paddingRight: h.paddingRight ?? false,
            })),
            dispose() {},
          }
        },
      }),

      // Code Lens provider
      monacoInstance.languages.registerCodeLensProvider(lang, {
        async provideCodeLenses(model) {
          if (!lspClient.isReady) return { lenses: [], dispose() {} }
          const filePath = openFileRef.current || model.uri.fsPath || model.uri.path
          const lenses = await lspClient.codeLens(filePath).catch(() => null)
          if (!lenses?.length) return { lenses: [], dispose() {} }
          return {
            lenses: lenses.map((lens: any) => ({
              range: {
                startLineNumber: lens.range.start.line + 1,
                startColumn: lens.range.start.character + 1,
                endLineNumber: lens.range.end.line + 1,
                endColumn: lens.range.end.character + 1,
              },
              command: lens.command
                ? { id: lens.command.command ?? '', title: lens.command.title ?? '' }
                : { id: '', title: '' },
            })),
            dispose() {},
          }
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

    // Register global command for running individual test functions via CodeLens
    const cmdDisposable = monacoInstance.editor.registerCommand(
      'tutor.runTest',
      (_accessor: unknown, funcName: string) => handleTestFuncRef.current(funcName),
    )

    // Custom test CodeLens provider (regex-based, works without LSP)
    const testLensDisposable = monacoInstance.languages.registerCodeLensProvider(
      { scheme: 'file' },
      {
        provideCodeLenses(model) {
          const filePath = model.uri.fsPath || model.uri.path
          const content = model.getValue()
          const tests = getTestFunctions(filePath, content)
          if (!tests.length) return { lenses: [], dispose() {} }
          return {
            lenses: tests.map((t) => ({
              range: { startLineNumber: t.line, startColumn: 1, endLineNumber: t.line, endColumn: 1 },
              command: { id: 'tutor.runTest', title: '▶ Run Test', arguments: [t.name] },
            })),
            dispose() {},
          }
        },
      },
    )

    return () => {
      disposables.forEach((d) => d.dispose())
      cmdDisposable.dispose()
      testLensDisposable.dispose()
    }
  }, [monacoInstance])

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

  // ProblemsPanel pendingNavigate → 에디터 위치 이동
  // 에디터가 remount되지 않으므로 editorRef.current가 항상 유효
  useEffect(() => {
    if (!pendingNavigate || !editorRef.current) return
    if (openFile !== pendingNavigate.path) return
    const { line, column } = pendingNavigate
    editorRef.current.revealLineInCenter(line)
    editorRef.current.setPosition({ lineNumber: line, column })
    editorRef.current.focus()
    setPendingNavigate(null)
  }, [pendingNavigate, openFile, setPendingNavigate])

  // openFile 변경 시 모델 교체 (Monaco 네이티브 방식: 에디터 remount 없이 setModel())
  useEffect(() => {
    const ed = editorRef.current
    if (!ed || !monacoInstance || !openFile) return

    const uri = monacoInstance.Uri.parse(`file://${openFile}`)
    let model = monacoInstance.editor.getModel(uri)
    if (!model) {
      model = monacoInstance.editor.createModel(openFileContent, detectLanguage(openFile), uri)
    } else if (model.getValue() !== openFileContent) {
      // 서버 측 파일 갱신 (nextstep 등)에 의한 외부 변경 동기화
      model.setValue(openFileContent)
    }

    ed.setModel(model)
    const isGo = openFile.endsWith('.go')
    model.updateOptions({ tabSize: isGo ? 4 : 2, insertSpaces: !isGo })
    applyDecorations(ed, openFileContent, filename, skillLevel, solvedHoles, decorationRef)

    // 캐시된 LSP 진단 적용
    const cached = diagnosticsCacheRef.current.get(`file://${openFile}`)
    if (cached) monacoInstance.editor.setModelMarkers(model, 'lsp', cached)

    // definition 이동 대상이 있으면 setModel() 직후 바로 이동
    const nav = pendingNavigateRef.current
    if (nav && nav.path === openFile) {
      ed.revealLineInCenter(nav.line)
      ed.setPosition({ lineNumber: nav.line, column: nav.column })
      ed.focus()
      pendingNavigateRef.current = null
    }
  }, [openFile, monacoInstance])

  // readOnly 변경 시 에디터 옵션 업데이트
  useEffect(() => {
    editorRef.current?.updateOptions({ readOnly: openFileReadOnly })
  }, [openFileReadOnly])

  // minimap 토글
  useEffect(() => {
    editorRef.current?.updateOptions({ minimap: { enabled: showMinimap } })
  }, [showMinimap])

  // Git diff gutter decorations
  useEffect(() => {
    const ed = editorRef.current
    if (!ed || !monacoInstance || !openFile || !projectStatus?.dir) return
    const dir = projectStatus.dir
    fetchGitDiff(openFile, dir).then((changes) => {
      if (gitDiffCollectionRef.current) gitDiffCollectionRef.current.clear()
      if (!changes.length) return
      const decorations = changes
        .filter(c => c.type === 'added')
        .map(c => ({
          range: {
            startLineNumber: c.lineNum,
            startColumn: 1,
            endLineNumber: c.lineNum,
            endColumn: 1,
          },
          options: {
            isWholeLine: false,
            linesDecorationsClassName: 'git-added-gutter',
          } as import('monaco-editor').editor.IModelDecorationOptions,
        }))
      gitDiffCollectionRef.current = ed.createDecorationsCollection(decorations)
    })
  }, [openFile, monacoInstance, projectStatus?.dir])

  // navigateCrossFileRef: fresh 값 유지 (addTab, setOpenFileReadOnly, projectStatus?.dir)
  useEffect(() => {
    navigateCrossFileRef.current = async (absPath: string, line: number, col: number) => {
      if (!monacoInstance) return
      const uri = monacoInstance.Uri.parse(`file://${absPath}`)
      let content = monacoInstance.editor.getModel(uri)?.getValue() ?? ''
      if (!content) {
        try {
          content = await readFileViaApi(absPath)
          monacoInstance.editor.createModel(content, detectLanguage(absPath), uri)
        } catch { return }
      }
      const isOutsideProject = projectStatus?.dir && !absPath.startsWith(projectStatus.dir)
      addTab(absPath, content)
      setOpenFileReadOnly(!!isOutsideProject)
      pendingNavigateRef.current = { path: absPath, line, column: col }
    }
  }, [monacoInstance, addTab, setOpenFileReadOnly, projectStatus?.dir])

  // Cmd+click → cross-file 이동 (provideDefinition은 hover/link decoration도 호출하므로 여기서만 이동)
  useEffect(() => {
    const ed = editorRef.current
    if (!ed || !monacoInstance) return
    const disposable = ed.onMouseDown(async (e) => {
      if (!(e.event.metaKey || e.event.ctrlKey)) return
      if (e.target.type !== monacoInstance.editor.MouseTargetType.CONTENT_TEXT) return
      const pos = e.target.position
      if (!pos || !lspClient.isReady) return
      const filePath = openFileRef.current
      if (!filePath) return
      const result = await lspClient.definition(filePath, pos.lineNumber - 1, pos.column - 1).catch(() => null)
      if (!result) return
      const locs = Array.isArray(result) ? result : [result]
      if (!locs.length) return
      const loc = locs[0]
      const uri = monacoInstance.Uri.parse(loc.uri)
      const absPath = uri.fsPath || uri.path
      if (absPath === filePath) return // same-file: Monaco가 provideDefinition 결과로 처리
      await navigateCrossFileRef.current(absPath, loc.range.start.line + 1, loc.range.start.character + 1)
    })
    return () => disposable.dispose()
  }, [monacoInstance])

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

    // 초기 모델 생성 및 에디터에 설정
    if (openFileRef.current) {
      const uri = monaco.Uri.parse(`file://${openFileRef.current}`)
      let model = monaco.editor.getModel(uri)
      if (!model) {
        model = monaco.editor.createModel(
          openFileContentRef.current,
          detectLanguage(openFileRef.current),
          uri,
        )
      }
      ed.setModel(model)
      applyDecorations(ed, openFileContentRef.current, openFileRef.current.split('/').pop() || '', 'normal', new Set(), decorationRef)

      // 캐시된 진단 적용
      const cached = diagnosticsCacheRef.current.get(`file://${openFileRef.current}`)
      if (cached) monaco.editor.setModelMarkers(model, 'lsp', cached)
    }

    // 사용자 입력 → handleChange (onChange prop 대신 직접 등록)
    ed.onDidChangeModelContent(() => {
      const content = ed.getValue()
      setOpenFileContent(content)
      const filePath = openFileRef.current
      if (!filePath) return
      markFileChanged(filePath)
      lspClient.notifyChange(filePath, content)
      if (saveTimer.current) clearTimeout(saveTimer.current)
      saveTimer.current = setTimeout(async () => {
        const fp = openFileRef.current
        if (fp) {
          await writeFileRef.current(fp, content)
          markFileSaved(fp)
        }
      }, 500)
    })

    // Cmd+S / Ctrl+S: format then save
    ed.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, async () => {
      await ed.getAction('editor.action.formatDocument')?.run()
      const content = ed.getValue()
      const filePath = openFileRef.current
      if (filePath) {
        await writeFileRef.current(filePath, content)
        markFileSaved(filePath)
        lspClient.notifySave(filePath)
      }
    })

    // F12 → go to definition (cross-file 이동 처리)
    ed.addCommand(monaco.KeyCode.F12, async () => {
      const pos = ed.getPosition()
      if (!pos || !lspClient.isReady) return
      const filePath = openFileRef.current
      if (!filePath) return
      const result = await lspClient.definition(filePath, pos.lineNumber - 1, pos.column - 1).catch(() => null)
      if (!result) return
      const locs = Array.isArray(result) ? result : [result]
      if (!locs.length) return
      const loc = locs[0]
      const uri = monaco.Uri.parse(loc.uri)
      const absPath = uri.fsPath || uri.path
      if (absPath === filePath) {
        // same-file: 직접 이동
        ed.revealLineInCenter(loc.range.start.line + 1)
        ed.setPosition({ lineNumber: loc.range.start.line + 1, column: loc.range.start.character + 1 })
      } else {
        await navigateCrossFileRef.current(absPath, loc.range.start.line + 1, loc.range.start.character + 1)
      }
    })
  }, [])

  // 콘텐츠/스킬/solvedHoles 변경 시 데코레이션 재적용 (외부 갱신 반영)
  useEffect(() => {
    const ed = editorRef.current
    if (!ed || !monacoInstance || !openFile) return
    const uri = monacoInstance.Uri.parse(`file://${openFile}`)
    const model = monacoInstance.editor.getModel(uri)
    // 모델 콘텐츠가 다르면 동기화 (nextstep/quiz solve 등 외부 변경)
    if (model && model.getValue() !== openFileContent) {
      model.setValue(openFileContent)
    }
    applyDecorations(ed, openFileContent, filename, skillLevel, solvedHoles, decorationRef)
  }, [openFileContent, skillLevel, solvedHoles])

  // 출력 추가될 때마다 하단 스크롤
  useEffect(() => {
    outputEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [runOutput])

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
        let msg = `[오류] 요청 실패 (${response.status})`
        try {
          const body = await response.json()
          if (body.error) msg += ': ' + body.error
        } catch { /* ignore */ }
        setRunOutput([msg])
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
    streamOutput(`${LOCAL}/api/run`, '실행 결과', setIsRunning)
  }

  useEffect(() => {
    handleTestFuncRef.current = (funcName: string) => {
      if (isRunning || isTesting) return
      const url = funcName ? `${LOCAL}/api/test?func=${encodeURIComponent(funcName)}` : `${LOCAL}/api/test`
      const title = funcName ? `테스트: ${funcName}` : '테스트 결과'
      streamOutput(url, title, setIsTesting)
    }
  }, [isRunning, isTesting])

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
                  onClick={(e) => { e.stopPropagation(); lspClient.notifyClose(tab.path); closeTab(tab.path) }}
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
          {isTesting && (
            <span className="run-btn test-btn running" style={{ pointerEvents: 'none' }}>
              <span className="run-spinner" /> 테스트 중...
            </span>
          )}
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
          theme="vs-dark"
          height="100%"
          width="100%"
          onMount={handleMount}
          options={{
            fontSize: 14,
            fontFamily: "'Consolas', 'Courier New', monospace",
            lineNumbers: 'on',
            minimap: { enabled: showMinimap },
            scrollBeyondLastLine: true,
            wordWrap: 'on',
            glyphMargin: true,
            folding: true,
            renderLineHighlight: 'line',
            tabSize: 2,
            insertSpaces: true,
            padding: { top: 8 },
            quickSuggestions: { other: true, comments: false, strings: true },
            suggestOnTriggerCharacters: true,
            acceptSuggestionOnCommitCharacter: true,
            wordBasedSuggestions: 'off',
            codeLens: true,
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
