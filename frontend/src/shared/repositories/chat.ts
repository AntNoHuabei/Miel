import type { AGUIMessagesSnapshotLite } from '../types/chat'
import { Services } from './bindings'
import { normalizeList } from './helpers'

export const chatRepository = {
  listConversations: async () => normalizeList(await Services.AgentService.ListConversations()),
  messagesSnapshot: async (id: number) => (await Services.AgentService.MessagesSnapshot(id)) as AGUIMessagesSnapshotLite,
  chat: async (request: Parameters<typeof Services.AgentService.Chat>[0]) => Services.AgentService.Chat(request),
  deleteConversation: (id: number) => Services.AgentService.DeleteConversation(id),
}
