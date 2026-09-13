import type { ChatAttachmentDraftLite } from '../types/chat'
import type { ClipboardTodoDraftLite } from '../types/capture'
import { Services } from './bindings'
import { normalizeList } from './helpers'

export const attachmentRepository = {
  pickImages: async () => normalizeList(await Services.ChatAttachmentService.PickImages()) as ChatAttachmentDraftLite[],
  pasteImage: async () => (await Services.ChatAttachmentService.PasteImage()) as ChatAttachmentDraftLite | null,
  discardDrafts: (ids: string[]) => Services.ChatAttachmentService.DiscardDrafts(ids),
  imageDataUri: (id: string) => Services.ChatAttachmentService.GetImageDataURI(id),
}

export const clipboardRepository = {
  extractTodos: async () => (await Services.ClipboardService.ExtractTodos()) as ClipboardTodoDraftLite,
  confirmTodos: (input: Parameters<typeof Services.ClipboardService.ConfirmTodos>[0]) => Services.ClipboardService.ConfirmTodos(input),
  discardDraft: (id: string) => Services.ClipboardService.DiscardDraft(id),
}

export const screenshotRepository = {
  capture: () => Services.ScreenshotService.Capture(),
  extractTodos: (id: number) => Services.ScreenshotService.ExtractTodos(id),
  confirmExtracted: (input: Parameters<typeof Services.ScreenshotService.ConfirmExtracted>[0]) => Services.ScreenshotService.ConfirmExtracted(input),
  ask: (input: Parameters<typeof Services.ScreenshotService.AskAboutShot>[0]) => Services.ScreenshotService.AskAboutShot(input),
  save: (input: Parameters<typeof Services.ScreenshotService.SaveShot>[0]) => Services.ScreenshotService.SaveShot(input),
}
