// LSP JSON-RPC 2.0 client over WebSocket

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

  async connect(lang: string, rootPath: string, token: string): Promise<void> {
    this.disconnect()

    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const host = window.location.host
    const url = `${proto}://${host}/ws/lsp?lang=${encodeURIComponent(lang)}&root=${encodeURIComponent(rootPath)}&token=${encodeURIComponent(token)}`

    await new Promise<void>((resolve, reject) => {
      const ws = new WebSocket(url)
      this.ws = ws

      ws.onopen = async () => {
        try {
          await this.initialize(rootPath)
          this.initialized = true
          this.emitStatus(true)
          resolve()
        } catch (e) {
          reject(e)
        }
      }

      ws.onerror = (e) => reject(e)
      ws.onclose = () => {
        this.initialized = false
        this.emitStatus(false)
        for (const [, p] of this.pending) {
          p.reject(new Error('LSP WebSocket closed'))
        }
        this.pending.clear()
      }
      ws.onmessage = (e) => this.onMessage(e.data as string)
    })
  }

  disconnect(): void {
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
    this.initialized = false
    this.openedUris.clear()
    this.fileVersions.clear()
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
      capabilities: {
        textDocument: {
          publishDiagnostics: {
            relatedInformation: false,
          },
          definition: {},
          hover: {},
          formatting: {},
          completion: {
            completionItem: {
              snippetSupport: true,
              documentationFormat: ['plaintext', 'markdown'],
            },
          },
          synchronization: {
            didSave: false,
            willSave: false,
          },
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
    let msg: any
    try {
      msg = JSON.parse(data)
    } catch {
      return
    }

    if ('id' in msg && msg.id !== undefined) {
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
    if (msg.method === 'textDocument/publishDiagnostics' && msg.params) {
      const { uri, diagnostics } = msg.params as { uri: string; diagnostics: LspDiagnostic[] }
      this.diagnosticListeners.forEach(cb => cb(uri, diagnostics))
    }
  }
}

export const lspClient = new LspClient()
export type { Location, LspCompletionItem, LspCompletionList, LspTextEdit, LspDiagnostic, LspHover }
