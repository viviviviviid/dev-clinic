import type { QuizData, QuizItem } from '../../store'

export interface QuizMarker {
  key: string
  lineNumber: number
  item: QuizItem
}

export function quizMarkers(content: string, filename: string, quizData: QuizData, solved: Set<string>): QuizMarker[] {
  const counters = { hole: 0, bug: 0 }
  const markers: QuizMarker[] = []
  content.split('\n').forEach((line, index) => {
    const match = line.match(/\[TUTOR:(HOLE|BUG)\]\s*(.*)/)
    if (!match) return
    const markerType = match[1] === 'BUG' ? 'bug' : 'hole'
    const markerIndex = counters[markerType]++
    const key = `${filename}:${markerType}:${markerIndex}`
    const basename = filename.split('/').pop() ?? filename
    // Current quizzes use relative paths; older quizzes used basenames or HOLE-only keys.
    const candidates = [key, `${basename}:${markerType}:${markerIndex}`]
    if (markerType === 'hole') candidates.push(`${filename}:${markerIndex}`, `${basename}:${markerIndex}`)
    if (candidates.some(candidate => solved.has(candidate))) return
    const stored = candidates.map(candidate => quizData[candidate]).find(Boolean)
    markers.push({ key, lineNumber: index + 1, item: {
      key, filename, markerType, markerIndex,
      question: stored?.question || match[2].trim() || (markerType === 'bug' ? '이 부분의 버그를 수정하세요.' : '이 부분을 구현하세요.'),
      hints: stored?.hints ?? [],
    } })
  })
  return markers
}
