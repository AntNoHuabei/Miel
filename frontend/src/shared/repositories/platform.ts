import { Clipboard as WailsClipboard } from '@wailsio/runtime'
import { Services } from './bindings'

export const appRepository = { version: () => Services.ApplicationService.Version() }

export const directoryRepository = {
  paths: () => Services.DirectoryService.Paths(),
  openDataDir: () => Services.DirectoryService.OpenDataDir(),
}

export const permissionRepository = {
  getState: () => Services.PermissionService.GetPermissionState(),
  cancelPending: (sessionId: string) => Services.PermissionService.CancelPendingApprovals(sessionId),
  setMode: (mode: string) => Services.PermissionService.SetPermissionMode(mode as never),
  resolve: (id: string, decision: string) => Services.PermissionService.ResolveApproval(id, decision as never),
}

export const themeRepository = { setTheme: (id: string) => Services.WindowThemeService.SetTheme(id) }
export const systemClipboardRepository = { setText: (value: string) => WailsClipboard.SetText(value) }
