import { createStore } from 'zustand/vanilla'
import type { ConversationTimelineItemLite } from '../../../api'
import { agentRunReducer, initialAgentRunState } from './agentRun'
import type { AgentRunAction, AgentRunState } from './agentRun'
import type { ConversationContextUsageLite } from '../../../shared/types/chat'

export interface ConversationState {
  conversationId: number
  timeline: ConversationTimelineItemLite[]
  contextUsage: ConversationContextUsageLite | null
  input: string
  run: AgentRunState
  setConversation: (id: number) => void
  setTimeline: (timeline: ConversationTimelineItemLite[] | ((current: ConversationTimelineItemLite[]) => ConversationTimelineItemLite[])) => void
  setContextUsage: (usage: ConversationContextUsageLite | null) => void
  setInput: (input: string) => void
  dispatchRun: (action: AgentRunAction) => void
  reset: () => void
}

export function createConversationStore(storageKey: string) {
  const persisted = typeof window === 'undefined' ? 0 : Number(window.localStorage.getItem(storageKey))
  const initialConversationId = Number.isSafeInteger(persisted) && persisted > 0 ? persisted : 0

  return createStore<ConversationState>((set) => ({
    conversationId: initialConversationId,
    timeline: [],
    contextUsage: null,
    input: '',
    run: initialAgentRunState,
    setConversation: (conversationId) => {
      if (typeof window !== 'undefined') {
        if (conversationId > 0) window.localStorage.setItem(storageKey, String(conversationId))
        else window.localStorage.removeItem(storageKey)
      }
      set({ conversationId })
    },
    setTimeline: (timeline) => set((state) => ({ timeline: typeof timeline === 'function' ? timeline(state.timeline) : timeline })),
    setContextUsage: (contextUsage) => set({ contextUsage }),
    setInput: (input) => set({ input }),
    dispatchRun: (action) => set((state) => ({ run: agentRunReducer(state.run, action) })),
    reset: () => {
      if (typeof window !== 'undefined') window.localStorage.removeItem(storageKey)
      set({ conversationId: 0, timeline: [], contextUsage: null, input: '', run: initialAgentRunState })
    },
  }))
}

export const mainConversationStore = createConversationStore('blankmind.chat.currentConversation')
export const quickConversationStore = createConversationStore('blankmind.quick.currentConversation')
