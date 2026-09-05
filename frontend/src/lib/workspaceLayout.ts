export const COMPACT_WORKSPACE_WIDTH = 1000
export const MIN_CODE_WIDTH = 480

// Keep saved panel preferences intact; constrain only the rendered widths.
export function workspaceLayout(viewport: number, leftWidth: number, feedbackWidth: number) {
  if (viewport <= COMPACT_WORKSPACE_WIDTH) return { leftWidth, feedbackWidth: viewport }
  const panelBudget = viewport - MIN_CODE_WIDTH - (leftWidth > 0 ? 16 : 8)
  const left = leftWidth > 0 ? Math.min(leftWidth, Math.max(120, panelBudget - 320)) : 0
  return { leftWidth: left, feedbackWidth: Math.min(feedbackWidth, panelBudget - left) }
}
