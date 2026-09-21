import { desktopBridge } from './desktop.ts'

export const DESKTOP_SAVE_EVENT = 'clinic:save-before-close'
export interface DesktopSaveRequest { waitUntil: (save: Promise<boolean>) => void }

export function installDesktopLifecycle(): void {
  const bridge = desktopBridge()
  const runtime = (window as Window & { runtime?: { EventsOn: (event: string, listener: () => void) => void } }).runtime
  if (!bridge || !runtime) return
  runtime.EventsOn('desktop:before-close', () => {
    void (async () => {
      const saves: Promise<boolean>[] = []
      let timer: ReturnType<typeof setTimeout> | undefined
      try {
        window.dispatchEvent(new CustomEvent<DesktopSaveRequest>(DESKTOP_SAVE_EVENT, {
          detail: { waitUntil: save => saves.push(save) },
        }))
        const results = await Promise.race([
          Promise.all(saves),
          new Promise<never>((_, reject) => {
            timer = setTimeout(() => reject(new Error('저장을 기다리는 시간이 초과되었습니다.')), 15_000)
          }),
        ])
        if (results.some(saved => !saved)) throw new Error('일부 파일을 저장하지 못했습니다.')
        await bridge.FinishClose(true)
      } catch {
        await bridge.FinishClose(false)
        window.alert('파일 저장을 완료하지 못해 앱을 열어 두었습니다. 저장 상태를 확인한 뒤 다시 종료해 주세요.')
      } finally {
        clearTimeout(timer)
      }
    })()
  })
  void bridge.EnableCloseHandler()
}
