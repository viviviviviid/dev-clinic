export type ChatSseEvent =
  | { type: 'message'; text: string }
  | { type: 'done' }

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseBlock(block: string): ChatSseEvent | null {
  let eventType = 'message'
  const dataLines: string[] = []

  for (const line of block.split(/\r?\n/)) {
    if (line.startsWith('event:')) eventType = line.slice(6).trim()
    if (line.startsWith('data:')) dataLines.push(line.slice(5).trimStart())
  }

  if (eventType === 'done') return { type: 'done' }
  if (dataLines.length === 0) return null

  let data: unknown
  try {
    data = JSON.parse(dataLines.join('\n')) as unknown
  } catch (cause) {
    throw new Error('AI 채팅 스트림에 손상된 JSON이 포함되어 있습니다.', { cause })
  }

  if (!isRecord(data) || typeof data.text !== 'string') {
    throw new Error('AI 채팅 스트림의 메시지 형식이 올바르지 않습니다.')
  }
  return { type: 'message', text: data.text }
}

export class ChatSseParser {
  private buffer = ''

  push(chunk: string): ChatSseEvent[] {
    this.buffer += chunk
    const events: ChatSseEvent[] = []

    while (true) {
      const boundary = /\r?\n\r?\n/.exec(this.buffer)
      if (!boundary || boundary.index === undefined) break
      const block = this.buffer.slice(0, boundary.index)
      this.buffer = this.buffer.slice(boundary.index + boundary[0].length)
      const event = parseBlock(block)
      if (event) events.push(event)
    }

    return events
  }

  finish(): ChatSseEvent[] {
    const block = this.buffer
    this.buffer = ''
    if (block.trim() === '') return []
    const event = parseBlock(block)
    return event ? [event] : []
  }
}
