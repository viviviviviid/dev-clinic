export interface ChatCodeReference {
  path: string
  startLine: number
  startColumn: number
  endLine: number
  endColumn: number
  code: string
}

export interface ChatAnswerRequest {
  markerType: 'hole' | 'bug'
  question: string
  reference: ChatCodeReference
}

export interface PendingChatAnswer {
  answer: ChatAnswerRequest
  fileContent: string
}

interface SelectionRange {
  startLineNumber: number
  startColumn: number
  endLineNumber: number
  endColumn: number
}

export const MAX_CHAT_CODE_REFERENCES = 8
export const MAX_CHAT_CODE_BYTES = 32 << 10

export function createChatCodeReference(path: string, range: SelectionRange, code: string): ChatCodeReference | null {
  if (!code.trim()) return null
  const endsAtNextLine = range.endColumn === 1 && range.endLineNumber > range.startLineNumber
  const selectedLines = endsAtNextLine ? code.replace(/\r?\n$/, '').split(/\r?\n/) : []
  return {
    path,
    startLine: range.startLineNumber,
    startColumn: range.startColumn,
    // Monaco's end position is exclusive: selecting through the next line's
    // first column includes only the previous line in the displayed range.
    endLine: endsAtNextLine ? range.endLineNumber - 1 : range.endLineNumber,
    endColumn: endsAtNextLine
      ? selectedLines[selectedLines.length - 1].length + (selectedLines.length === 1 ? range.startColumn : 1)
      : range.endColumn,
    code,
  }
}

export function chatCodeLabel(reference: ChatCodeReference): string {
  const lines = reference.startLine === reference.endLine
    ? `${reference.startLine}` : `${reference.startLine}–${reference.endLine}`
  return `${reference.path}:${lines}`
}

export function chatCodeKey(reference: ChatCodeReference): string {
  return JSON.stringify([reference.path, reference.startLine, reference.startColumn, reference.endLine, reference.endColumn])
}

export function mergeChatCodeReferences(current: ChatCodeReference[], incoming: ChatCodeReference[]): ChatCodeReference[] | null {
  const references = new Map(current.map(reference => [chatCodeKey(reference), reference]))
  for (const reference of incoming) references.set(chatCodeKey(reference), reference)
  const merged = [...references.values()]
  // Bound the serialized context, including paths and escaping, below the
  // existing server file-context budget. Reject rather than silently truncate.
  if (merged.length > MAX_CHAT_CODE_REFERENCES || new TextEncoder().encode(chatCodeContext(merged)).length > MAX_CHAT_CODE_BYTES) return null
  return merged
}

export function chatCodeContext(references: ChatCodeReference[]): string {
  return JSON.stringify({ selectedCode: references.map(reference => ({ ...reference, location: chatCodeLabel(reference) })) })
}

interface MessageWithCode {
  role: 'user' | 'ai'
  content: string
  codeReferences?: ChatCodeReference[]
}

export function createChatPayload(message: string, fileContent: string, history: MessageWithCode[], references: ChatCodeReference[] = [], answerRequest?: ChatAnswerRequest) {
  // Keep code in the existing reference-data fields, which clinic wraps in
  // UNTRUSTED_DATA_JSON. History retains the snapshot used for each question.
  return {
    message,
    fileContent: answerRequest ? fileContent : references.length ? chatCodeContext(references) : fileContent,
    ...(answerRequest ? { answerRequest } : {}),
    chatHistory: history.map(item => ({
      role: item.role,
      content: item.codeReferences?.length ? `${item.content}\n\n${chatCodeContext(item.codeReferences)}` : item.content,
    })),
  }
}
