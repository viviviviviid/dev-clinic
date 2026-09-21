import { findTutorMarkerLineRange } from './markerRanges.ts'
import { createChatCodeReference, MAX_CHAT_CODE_BYTES } from '../../lib/chatCodeContext.ts'
import type { PendingChatAnswer } from '../../lib/chatCodeContext.ts'

interface AnswerMarker {
  markerType: string
  markerIndex: number
  question: string
}

export function createQuizAnswerRequest(filename: string, content: string, item: AnswerMarker): PendingChatAnswer {
  if (item.markerType !== 'hole' && item.markerType !== 'bug') throw new Error('답지를 생성할 문제를 찾지 못했습니다.')
  const range = findTutorMarkerLineRange(content, item.markerType, item.markerIndex)
  if (!range) throw new Error('문제 범위가 변경되었습니다. 과제를 다시 열어 주세요.')
  const lines = content.split('\n')
  const reference = createChatCodeReference(filename, {
    ...range, startColumn: 1, endColumn: lines[range.endLineNumber - 1].length + 1,
  }, lines.slice(range.startLineNumber - 1, range.endLineNumber).join('\n'))
  if (!reference) throw new Error('답지를 생성할 코드가 없습니다.')
  const bytes = new TextEncoder()
  if (bytes.encode(reference.code).length > MAX_CHAT_CODE_BYTES || bytes.encode(content).length > (128 << 10)) {
    throw new Error('답지 생성은 문제 코드 32KB, 현재 파일 128KB까지 지원합니다.')
  }
  return { answer: { markerType: item.markerType, question: item.question, reference }, fileContent: content }
}
