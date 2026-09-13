import type { MemoryInputLite, MemoryItemLite, MemorySettingsLite } from '../types/memory'
import { Services } from './bindings'
import { normalizeList } from './helpers'

export const memoryRepository = {
  getSettings: async () => (await Services.MemoryService.GetSettings()) as MemorySettingsLite,
  list: async () => normalizeList(await Services.MemoryService.ListMemories()) as MemoryItemLite[],
  saveSettings: async (input: Parameters<typeof Services.MemoryService.SaveSettings>[0]) => (await Services.MemoryService.SaveSettings(input)) as MemorySettingsLite,
  add: (input: MemoryInputLite) => Services.MemoryService.AddMemory(input),
  update: (input: Parameters<typeof Services.MemoryService.UpdateMemory>[0]) => Services.MemoryService.UpdateMemory(input),
  delete: (id: string) => Services.MemoryService.DeleteMemory(id),
  clear: () => Services.MemoryService.ClearMemories(),
  export: () => Services.MemoryService.ExportMemories(),
}
