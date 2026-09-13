import { createStore } from 'zustand/vanilla'
import type { AGUIMessageLite } from '../../../api'
import { agentRunReducer, initialAgentRunState } from './agentRun'
import type { AgentRunAction, AgentRunState } from './agentRun'

export interface ConversationState {
  conversationId: number
  messages: AGUIMessageLite[]
  input: string
  run: AgentRunState
  setConversation: (id: number) => void
  setMessages: (messages: AGUIMessageLite[] | ((current: AGUIMessageLite[]) => AGUIMessageLite[])) => void
  setInput: (input: string) => void
  dispatchRun: (action: AgentRunAction) => void
  reset: () => void
}

export function createConversationStore(storageKey: string) {
  const persisted = typeof window === 'undefined' ? 0 : Number(window.localStorage.getItem(storageKey))
  const initialConversationId = Number.isSafeInteger(persisted) && persisted > 0 ? persisted : 0

  return createStore<ConversationState>((set) => ({
    conversationId: initialConversationId,
    messages: [],
    input: '',
    run: initialAgentRunState,
    setConversation: (conversationId) => {
      if (typeof window !== 'undefined') {
        if (conversationId > 0) window.localStorage.setItem(storageKey, String(conversationId))
        else window.localStorage.removeItem(storageKey)
      }
      set({ conversationId })
    },
    setMessages: (messages) => set((state) => ({ messages: typeof messages === 'function' ? messages(state.messages) : messages })),
    setInput: (input) => set({ input }),
    dispatchRun: (action) => set((state) => ({ run: agentRunReducer(state.run, action) })),
    reset: () => {
      if (typeof window !== 'undefined') window.localStorage.removeItem(storageKey)
      set({ conversationId: 0, messages: [], input: '', run: initialAgentRunState })
    },
  }))
}

export const mainConversationStore = createConversationStore('blankmind.chat.currentConversation')
export const quickConversationStore = createConversationStore('blankmind.quick.currentConversation')
