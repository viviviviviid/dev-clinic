import type { IRange, languages } from 'monaco-editor'

interface LspRange {
  start: { line: number; character: number }
  end: { line: number; character: number }
}

interface LspCompletionEdit {
  range: LspRange
  newText: string
}

export interface LspCompletionItem {
  label: string | { label: string }
  kind?: number
  detail?: string
  documentation?: string | { kind: string; value: string }
  insertText?: string
  insertTextFormat?: number
  filterText?: string
  sortText?: string
  preselect?: boolean
  textEdit?: LspCompletionEdit | { newText: string; insert: LspRange; replace: LspRange }
  additionalTextEdits?: LspCompletionEdit[]
}

export interface LspCompletionList {
  isIncomplete: boolean
  items: LspCompletionItem[]
}

export interface LspCompletionContext {
  triggerKind: 1 | 2 | 3
  triggerCharacter?: string
}

export function toLspCompletionContext(context: languages.CompletionContext): LspCompletionContext {
  // Monaco uses 0/1/2; the wire protocol uses 1/2/3.
  if (context.triggerKind === 1 && context.triggerCharacter) {
    return { triggerKind: 2, triggerCharacter: context.triggerCharacter }
  }
  return { triggerKind: context.triggerKind === 2 ? 3 : 1 }
}

function toRange(range: LspRange): IRange {
  return {
    startLineNumber: range.start.line + 1, startColumn: range.start.character + 1,
    endLineNumber: range.end.line + 1, endColumn: range.end.character + 1,
  }
}

export function toMonacoCompletions(
  raw: LspCompletionList | LspCompletionItem[],
  fallbackRange: IRange,
  mapKind: (kind: number) => languages.CompletionItemKind,
): languages.CompletionList {
  const items = Array.isArray(raw) ? raw : raw.items
  return {
    // gopls expects a fresh query as the prefix changes, including fuzzy and
    // deep completions that were not present in the initial dot response.
    incomplete: !Array.isArray(raw) && raw.isIncomplete,
    suggestions: items.map(item => {
      const label = typeof item.label === 'string' ? item.label : item.label.label
      const edit = item.textEdit
      return {
        label,
        kind: mapKind(item.kind ?? 1),
        detail: item.detail,
        documentation: typeof item.documentation === 'string' ? item.documentation : item.documentation?.value,
        insertText: edit?.newText ?? item.insertText ?? label,
        insertTextRules: item.insertTextFormat === 2 ? 4 : undefined, // InsertAsSnippet
        filterText: item.filterText ?? label,
        sortText: item.sortText ?? label,
        preselect: item.preselect,
        range: !edit ? fallbackRange : 'range' in edit ? toRange(edit.range)
          : { insert: toRange(edit.insert), replace: toRange(edit.replace) },
        additionalTextEdits: item.additionalTextEdits?.map(extra => ({ range: toRange(extra.range), text: extra.newText })),
      }
    }),
  }
}
