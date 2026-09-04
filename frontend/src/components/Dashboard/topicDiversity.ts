interface SuggestedTopic {
  name: string
}

export function rememberSuggestedTopics(
  previousTopics: string[],
  suggestions: SuggestedTopic[],
  limit = 50,
): string[] {
  const remembered: string[] = []
  const seen = new Set<string>()

  for (const topic of [...suggestions.map((suggestion) => suggestion.name), ...previousTopics]) {
    const normalized = topic.trim()
    if (!normalized) continue
    const key = normalized.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    remembered.push(normalized)
    if (remembered.length === limit) break
  }

  return remembered
}
