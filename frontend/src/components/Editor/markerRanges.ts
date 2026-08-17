export type TutorMarkerType = 'hole' | 'bug'

const START_MARKER = /^(\s*)(\/\/|#)\s*\[TUTOR:(HOLE|BUG)\](?:\s.*)?$/
const END_MARKER = /^\s*(\/\/|#)\s*\[TUTOR:END\]\s*$/

interface TutorMarkerRange {
  kind: TutorMarkerType
  start: number
  end: number
  baseIndent: string
}

function formatSnippet(code: string, baseIndent: string): string[] | null {
  const lines = code.replace(/\r\n/g, '\n').split('\n')
  while (lines.length > 0 && lines[0].trim() === '') lines.shift()
  while (lines.length > 0 && lines[lines.length - 1].trim() === '') lines.pop()
  if (lines.length === 0) return null

  const nonEmpty = lines.filter(line => line.trim() !== '')
  const minimumIndent = Math.min(
    ...nonEmpty.map(line => line.match(/^[\t ]*/)?.[0].length ?? 0),
  )

  return lines.map(line => {
    if (line.trim() === '') return ''
    return baseIndent + line.slice(minimumIndent)
  })
}

export function replaceTutorMarkerAtIndex(
  content: string,
  markerType: TutorMarkerType,
  markerIndex: number,
  code: string,
): string {
  if (!Number.isInteger(markerIndex) || markerIndex < 0) return content

  const lines = content.split('\n')
  const ranges: TutorMarkerRange[] = []
  let open: (Omit<TutorMarkerRange, 'end'> & { comment: string }) | null = null

  for (let lineIndex = 0; lineIndex < lines.length; lineIndex++) {
    const startMatch = lines[lineIndex].match(START_MARKER)
    if (startMatch) {
      if (open) return content
      open = {
        kind: startMatch[3].toLowerCase() as TutorMarkerType,
        start: lineIndex,
        baseIndent: startMatch[1],
        comment: startMatch[2],
      }
      continue
    }

    const endMatch = lines[lineIndex].match(END_MARKER)
    if (!endMatch) continue
    if (!open || endMatch[1] !== open.comment) return content
    ranges.push({ kind: open.kind, start: open.start, end: lineIndex, baseIndent: open.baseIndent })
    open = null
  }
  if (open) return content

  const target = ranges.filter(range => range.kind === markerType)[markerIndex]
  if (!target) return content

  const replacement = formatSnippet(code, target.baseIndent)
  if (!replacement) return content
  lines.splice(target.start, target.end - target.start + 1, ...replacement)
  return lines.join('\n')
}
