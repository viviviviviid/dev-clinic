export interface NurseTopicSuggestion {
  name: string
  slug: string
  difficulty: '상' | '중' | '하'
}

export type NurseSseEvent =
  | { type: 'message'; text: string }
  | { type: 'topics'; topics: NurseTopicSuggestion[] }
  | { type: 'done' }

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isTopicSuggestion(value: unknown): value is NurseTopicSuggestion {
  return isRecord(value) &&
    typeof value.name === 'string' &&
    typeof value.slug === 'string' &&
    (value.difficulty === '상' || value.difficulty === '중' || value.difficulty === '하')
}

function parseEventBlock(block: string): NurseSseEvent | null {
  let eventType = 'message'
  const dataLines: string[] = []

  for (const rawLine of block.split(/\r?\n/)) {
    const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine
    if (line.startsWith('event:')) {
      eventType = line.slice(6).trim()
    } else if (line.startsWith('data:')) {
      dataLines.push(line.slice(5).trimStart())
    }
  }

  if (eventType === 'done') return { type: 'done' }
  if (dataLines.length === 0) return null

  let data: unknown
  try {
    data = JSON.parse(dataLines.join('\n')) as unknown
  } catch {
    return null
  }

  if (eventType === 'message') {
    return isRecord(data) && typeof data.text === 'string'
      ? { type: 'message', text: data.text }
      : null
  }

  if (eventType === 'topics') {
    const candidates = Array.isArray(data)
      ? data
      : isRecord(data) && Array.isArray(data.topics)
        ? data.topics
        : null
    if (!candidates) return null
    return { type: 'topics', topics: candidates.filter(isTopicSuggestion) }
  }

  return null
}

/** Incrementally parses complete SSE records while retaining split records. */
export class NurseSseParser {
  private buffer = ''

  push(chunk: string): NurseSseEvent[] {
    this.buffer += chunk
    const events: NurseSseEvent[] = []

    while (true) {
      const boundary = /\r?\n\r?\n/.exec(this.buffer)
      if (!boundary || boundary.index === undefined) break

      const block = this.buffer.slice(0, boundary.index)
      this.buffer = this.buffer.slice(boundary.index + boundary[0].length)
      const event = parseEventBlock(block)
      if (event) events.push(event)
    }

    return events
  }

  finish(): NurseSseEvent[] {
    const block = this.buffer
    this.buffer = ''
    if (block.trim() === '') return []
    const event = parseEventBlock(block)
    return event ? [event] : []
  }
}
