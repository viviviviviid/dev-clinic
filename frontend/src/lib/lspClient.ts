// LSP JSON-RPC 2.0 client over WebSocket
import { WS_BASE } from './api'
import { supabase } from './supabase'

interface Location {
  uri: string
  range: {
    start: { line: number; character: number }
    end: { line: number; character: number }
  }
}

interface LspCompletionItem {
  label: string | { label: string }
  kind?: number
  detail?: string
  documentation?: string | { kind: string; value: string }
  insertText?: string
  insertTextFormat?: number  // 1=PlainText, 2=Snippet
  filterText?: string
  sortText?: string
}

interface LspCompletionList {
  isIncomplete: boolean
  items: LspCompletionItem[]
}

interface LspTextEdit {
  range: {
    start: { line: number; character: number }
    end: { line: number; character: number }
  }
  newText: string
}

type LspDocumentation = string | { kind: string; value: string }

interface LspParameterInformation {
  label: string | [number, number]
  documentation?: LspDocumentation
}

interface LspSignatureInformation {
  label: string
  documentation?: LspDocumentation
  parameters?: LspParameterInformation[]
}

interface LspSignatureHelp {
  signatures: LspSignatureInformation[]
  activeSignature?: number
  activeParameter?: number
}

interface LspCommand {
  title: string
  command: string
  arguments?: unknown[]
}

interface LspCodeAction {
  title: string
  kind?: string
  isPreferred?: boolean
  command?: LspCommand
}

interface LspWorkspaceEdit {
  changes?: Record<string, LspTextEdit[]>
}

interface LspInlayHintLabelPart {
  value: string
}

interface LspInlayHint {
  position: { line: number; character: number }
  label: string | LspInlayHintLabelPart[]
  kind?: number
  paddingLeft?: boolean
  paddingRight?: boolean
}

interface LspCodeLens {
  range: LspTextEdit['range']
  command?: LspCommand
}

interface LspDiagnostic {
  range: {
    start: { line: number; character: number }
    end: { line: number; character: number }
  }
  severity?: number  // 1=Error, 2=Warning, 3=Information, 4=Hint
  message: string
  source?: string
}

interface LspHoverContents {
  kind: string
  value: string
}

interface LspHover {
  contents: string | LspHoverContents | Array<{ language?: string; value: string } | string>
  range?: {
    start: { line: number; character: number }
    end: { line: number; character: number }
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

// Maps file extension → LSP language ID (when it differs from Monaco's lang name)
const LSP_LANG_ID: Record<string, string> = {
  go: 'go',
  typescript: 'typescript',
  typescriptreact: 'typescriptreact',
  javascript: 'javascript',
  javascriptreact: 'javascriptreact',
  python: 'python',
  sol: 'solidity',
  rust: 'rust',
}

class LspClient {
  private ws: WebSocket | null = null
  private pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: unknown) => void }>()
  private openedUris = new Set<string>()
  private fileVersions = new Map<string, number>()
  private initialized = false
  private nextId = 1
  private connectionGeneration = 0
  private statusListeners: Array<(ready: boolean) => void> = []
  private diagnosticListeners: Array<(uri: string, diagnostics: LspDiagnostic[]) => void> = []

  get isReady(): boolean {
    return this.ws?.readyState === WebSocket.OPEN && this.initialized
  }

  onStatusChange(cb: (ready: boolean) => void): () => void {
    this.statusListeners.push(cb)
    return () => { this.statusListeners = this.statusListeners.filter(l => l !== cb) }
  }

  onDiagnostics(cb: (uri: string, diagnostics: LspDiagnostic[]) => void): () => void {
    this.diagnosticListeners.push(cb)
    return () => { this.diagnosticListeners = this.diagnosticListeners.filter(l => l !== cb) }
  }

  private emitStatus(ready: boolean) {
    this.statusListeners.forEach(cb => cb(ready))
  }

  async connect(lang: string, rootPath: string): Promise<void> {
    this.disconnect()
    const generation = this.connectionGeneration

    const { data, error } = await supabase.auth.getSession()
    if (generation !== this.connectionGeneration) return
    const accessToken = data.session?.access_token
    if (error || !accessToken) {
      throw new Error('LSP 연결에 필요한 로그인 정보가 없습니다.')
    }

    const url = `${WS_BASE}/ws/lsp?lang=${encodeURIComponent(lang)}&root=${encodeURIComponent(rootPath)}`

    await new Promise<void>((resolve, reject) => {
      const ws = new WebSocket(url, ['coding-tutor', accessToken])
      this.ws = ws
      let settled = false
      const settle = (error?: unknown) => {
        if (settled) return
        settled = true
        if (error) reject(error)
        else resolve()
      }
      const isCurrent = () => generation === this.connectionGeneration && this.ws === ws

      ws.onopen = async () => {
        if (!isCurrent()) {
          ws.close()
          settle()
          return
        }
        try {
          await this.initialize(rootPath)
          if (!isCurrent()) {
            settle()
            return
          }
          this.initialized = true
          this.emitStatus(true)
          settle()
        } catch (e) {
          settle(isCurrent() ? e : undefined)
        }
      }

      ws.onerror = (e) => settle(isCurrent() ? e : undefined)
      ws.onclose = () => {
        if (!isCurrent()) {
          settle()
          return
        }
        this.ws = null
        this.initialized = false
        this.emitStatus(false)
        for (const [, p] of this.pending) {
          p.reject(new Error('LSP WebSocket closed'))
        }
        this.pending.clear()
        settle(new Error('LSP WebSocket closed before initialization'))
      }
      ws.onmessage = (e) => {
        if (isCurrent()) this.onMessage(e.data as string)
      }
    })
  }

  disconnect(): void {
    this.connectionGeneration += 1
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
    this.initialized = false
    this.openedUris.clear()
    this.fileVersions.clear()
    for (const [, request] of this.pending) {
      request.reject(new Error('LSP disconnected'))
    }
    this.pending.clear()
    this.emitStatus(false)
    this.diagnosticListeners.forEach(cb => cb('*', []))
  }

  async notifyOpen(filePath: string, content: string, langId: string): Promise<void> {
    const uri = `file://${filePath}`
    if (!this.isReady) return
    const lspLangId = LSP_LANG_ID[langId] ?? langId

    if (this.openedUris.has(uri)) {
      // Already opened — send didChange instead
      await this.notifyChange(filePath, content)
      return
    }

    this.openedUris.add(uri)
    this.fileVersions.set(uri, 1)
    this.notify('textDocument/didOpen', {
      textDocument: { uri, languageId: lspLangId, version: 1, text: content },
    })
  }

  notifyChange(filePath: string, content: string): void {
    const uri = `file://${filePath}`
    if (!this.isReady || !this.openedUris.has(uri)) return
    const version = (this.fileVersions.get(uri) ?? 1) + 1
    this.fileVersions.set(uri, version)
    this.notify('textDocument/didChange', {
      textDocument: { uri, version },
      contentChanges: [{ text: content }],
    })
  }

  async definition(
    filePath: string,
    line: number,
    char: number,
  ): Promise<Location | Location[] | null> {
    const uri = `file://${filePath}`
    const result = await Promise.race([
      this.request('textDocument/definition', {
        textDocument: { uri },
        position: { line, character: char },
      }),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 3000)),
    ])
    if (!result) return null
    return result as Location | Location[]
  }

  async completion(
    filePath: string,
    line: number,
    char: number,
    triggerChar?: string,
  ): Promise<LspCompletionList | LspCompletionItem[] | null> {
    const uri = `file://${filePath}`
    const result = await Promise.race([
      this.request('textDocument/completion', {
        textDocument: { uri },
        position: { line, character: char },
        context: triggerChar
          ? { triggerKind: 2, triggerCharacter: triggerChar }
          : { triggerKind: 1 },
      }),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 3000)),
    ])
    if (!result) return null
    return result as LspCompletionList | LspCompletionItem[]
  }

  async formatting(
    filePath: string,
    tabSize = 4,
    insertSpaces = false,
  ): Promise<LspTextEdit[] | null> {
    const uri = `file://${filePath}`
    const result = await Promise.race([
      this.request('textDocument/formatting', {
        textDocument: { uri },
        options: { tabSize, insertSpaces },
      }),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 5000)),
    ])
    if (!result) return null
    return result as LspTextEdit[]
  }

  async references(
    filePath: string,
    line: number,
    char: number,
    includeDeclaration = false,
  ): Promise<Location[] | null> {
    const uri = `file://${filePath}`
    const result = await Promise.race([
      this.request('textDocument/references', {
        textDocument: { uri },
        position: { line, character: char },
        context: { includeDeclaration },
      }),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 5000)),
    ])
    if (!result) return null
    return result as Location[]
  }

  notifySave(filePath: string): void {
    const uri = `file://${filePath}`
    if (!this.isReady || !this.openedUris.has(uri)) return
    this.notify('textDocument/didSave', { textDocument: { uri } })
  }

  notifyClose(filePath: string): void {
    const uri = `file://${filePath}`
    if (!this.isReady || !this.openedUris.has(uri)) return
    this.notify('textDocument/didClose', { textDocument: { uri } })
    this.openedUris.delete(uri)
    this.fileVersions.delete(uri)
  }

  async signatureHelp(filePath: string, line: number, char: number): Promise<LspSignatureHelp | null> {
    const uri = `file://${filePath}`
    const result = await Promise.race([
      this.request('textDocument/signatureHelp', {
        textDocument: { uri },
        position: { line, character: char },
      }),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 2000)),
    ])
    return result as LspSignatureHelp | null
  }

  async codeAction(
    filePath: string,
    range: { startLine: number; startChar: number; endLine: number; endChar: number },
    diagnostics: LspDiagnostic[] = [],
  ): Promise<LspCodeAction[]> {
    const uri = `file://${filePath}`
    const result = await Promise.race([
      this.request('textDocument/codeAction', {
        textDocument: { uri },
        range: {
          start: { line: range.startLine, character: range.startChar },
          end: { line: range.endLine, character: range.endChar },
        },
        context: { diagnostics },
      }),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 3000)),
    ])
    if (!result) return []
    return result as LspCodeAction[]
  }

  async rename(
    filePath: string,
    line: number,
    char: number,
    newName: string,
  ): Promise<LspWorkspaceEdit | null> {
    const uri = `file://${filePath}`
    const result = await Promise.race([
      this.request('textDocument/rename', {
        textDocument: { uri },
        position: { line, character: char },
        newName,
      }),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 3000)),
    ])
    return result as LspWorkspaceEdit | null
  }

  async inlayHints(
    filePath: string,
    startLine: number,
    endLine: number,
  ): Promise<LspInlayHint[] | null> {
    const uri = `file://${filePath}`
    const result = await Promise.race([
      this.request('textDocument/inlayHint', {
        textDocument: { uri },
        range: {
          start: { line: startLine, character: 0 },
          end: { line: endLine, character: 0 },
        },
      }),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 2000)),
    ])
    return (result as LspInlayHint[] | null) ?? null
  }

  async codeLens(filePath: string): Promise<LspCodeLens[] | null> {
    const uri = `file://${filePath}`
    const result = await Promise.race([
      this.request('textDocument/codeLens', {
        textDocument: { uri },
      }),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 3000)),
    ])
    return (result as LspCodeLens[] | null) ?? null
  }

  async hover(
    filePath: string,
    line: number,
    char: number,
  ): Promise<LspHover | null> {
    const uri = `file://${filePath}`
    const result = await Promise.race([
      this.request('textDocument/hover', {
        textDocument: { uri },
        position: { line, character: char },
      }),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 2000)),
    ])
    if (!result) return null
    return result as LspHover
  }

  private async initialize(rootPath: string): Promise<void> {
    await this.request('initialize', {
      processId: null,
      rootUri: `file://${rootPath}`,
      workspaceFolders: [{ uri: `file://${rootPath}`, name: 'root' }],
      capabilities: {
        textDocument: {
          publishDiagnostics: { relatedInformation: true },
          definition: {},
          references: {},
          hover: {},
          formatting: {},
          signatureHelp: {
            signatureInformation: {
              documentationFormat: ['plaintext', 'markdown'],
              parameterInformation: { labelOffsetSupport: true },
            },
          },
          codeAction: {
            codeActionLiteralSupport: {
              codeActionKind: {
                valueSet: ['quickfix', 'refactor', 'source', 'source.organizeImports'],
              },
            },
          },
          rename: { prepareSupport: false },
          inlayHint: { resolveSupport: { properties: [] } },
          codeLens: {},
          completion: {
            completionItem: {
              snippetSupport: true,
              documentationFormat: ['plaintext', 'markdown'],
            },
          },
          synchronization: { didSave: true, willSave: false },
        },
      },
    })
    this.notify('initialized', {})
  }

  private request(method: string, params: unknown): Promise<unknown> {
    return new Promise((resolve, reject) => {
      if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
        reject(new Error('LSP not connected'))
        return
      }
      const id = this.nextId++
      this.pending.set(id, { resolve, reject })
      this.ws.send(JSON.stringify({ jsonrpc: '2.0', id, method, params }))
    })
  }

  private notify(method: string, params: unknown): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return
    this.ws.send(JSON.stringify({ jsonrpc: '2.0', method, params }))
  }

  private onMessage(data: string): void {
    let parsed: unknown
    try {
      parsed = JSON.parse(data) as unknown
    } catch {
      return
    }
    if (!isRecord(parsed)) return
    const msg = parsed

    if (typeof msg.id === 'number') {
      const pending = this.pending.get(msg.id)
      if (pending) {
        this.pending.delete(msg.id)
        if (msg.error) {
          pending.reject(msg.error)
        } else {
          pending.resolve(msg.result)
        }
      }
      return
    }

    // Server-initiated notifications
    if (msg.method === 'textDocument/publishDiagnostics' && isRecord(msg.params)) {
      const { uri, diagnostics } = msg.params
      if (typeof uri !== 'string' || !Array.isArray(diagnostics)) return
      this.diagnosticListeners.forEach(cb => cb(uri, diagnostics))
    }
  }
}

export const lspClient = new LspClient()
export type { Location, LspCompletionItem, LspCompletionList, LspTextEdit, LspDiagnostic, LspHover }
