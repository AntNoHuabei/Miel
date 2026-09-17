import type { ConversationSnapshotLite } from '../types/chat'
import { Services } from './bindings'
import { normalizeList } from './helpers'

export const chatRepository = {
  listConversations: async () => normalizeList(await Services.AgentService.ListConversations()),
  messagesSnapshot: async (id: number) => (await Services.AgentService.MessagesSnapshot(id)) as ConversationSnapshotLite,
  chat: async (request: Parameters<typeof Services.AgentService.Chat>[0]) => Services.AgentService.Chat(request),
  revisePlan: async (request: Parameters<typeof Services.AgentService.RevisePlan>[0]) => Services.AgentService.RevisePlan(request),
  executePlan: async (request: Parameters<typeof Services.AgentService.ExecutePlan>[0]) => Services.AgentService.ExecutePlan(request),
  abandonPlan: (request: Parameters<typeof Services.AgentService.AbandonPlan>[0]) => Services.AgentService.AbandonPlan(request),
  setConversationModel: (request: Parameters<typeof Services.AgentService.SetConversationModel>[0]) => Services.AgentService.SetConversationModel(request),
  setConversationProfile: (request: Parameters<typeof Services.AgentService.SetConversationProfile>[0]) => Services.AgentService.SetConversationProfile(request),
  deleteConversation: (id: number) => Services.AgentService.DeleteConversation(id),
}
