export function shouldApplyWebSocketMessage(
  messageProjectDir: string | undefined,
  connectionProjectDir: string,
  currentProjectDir: string,
): boolean {
  if (connectionProjectDir !== currentProjectDir) return false
  return messageProjectDir === undefined || messageProjectDir === currentProjectDir
}
