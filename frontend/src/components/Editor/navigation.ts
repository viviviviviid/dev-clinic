import type { Location } from '../../lib/lspClient'

export function isProjectFile(path: string, projectDir: string): boolean {
  const root = projectDir.replace(/\/+$/, '')
  return Boolean(root) && path.startsWith(`${root}/`) && !path.split('/').some(part => part === '..' || part === '.')
}

// Filter before loading models: definition previews also run on modifier hover.
export async function loadProjectLocations<T>(
  locations: Location | Location[],
  projectDir: string,
  pathFromUri: (uri: string) => string | null,
  load: (location: Location, path: string) => Promise<T>,
): Promise<T[]> {
  const results: T[] = []
  for (const location of Array.isArray(locations) ? locations : [locations]) {
    const path = pathFromUri(location.uri)
    if (!path || !isProjectFile(path, projectDir)) continue
    try {
      results.push(await load(location, path))
    } catch { /* An unavailable preview must not hide the remaining references. */ }
  }
  return results
}
