import { normalizeAgentRunError } from './chatError'
import type { AgentRunError } from './chatError'

export type AgentPhase = 'idle' | 'waiting' | 'thinking' | 'tool' | 'responding' | 'done' | 'error'

export interface AgentToolCall {
  id: string
  name: string
  args: string
  result: string
  status: 'preparing' | 'running' | 'done'
}

export interface AgentProtocolEvent {
  type: string
  delta?: string
  content?: string
  message?: string
  code?: string
  toolCallId?: string
  toolCallName?: string
}

export interface AgentEnvelope {
  conversationId: number
  requestId?: string
  delta?: string
  event?: AgentProtocolEvent
}

export interface AgentRunState {
  requestId: string
  conversationId: number
  targetConversationId: number
  phase: AgentPhase
  streaming: string
  reasoning: string
  tools: AgentToolCall[]
  inputSaved: boolean
  inputSavedConversationId: number
  error: AgentRunError | null
}

export type AgentRunAction =
  | { type: 'start'; requestId: string; conversationId: number }
  | { type: 'chunk'; payload: AgentEnvelope }
  | { type: 'event'; payload: AgentEnvelope }
  | { type: 'input-saved'; payload: { requestId?: string; conversationId: number } }
  | { type: 'complete'; conversationId: number }
  | { type: 'fail'; error: string }
  | { type: 'reset' }

export const initialAgentRunState: AgentRunState = {
  requestId: '',
  conversationId: 0,
  targetConversationId: 0,
  phase: 'idle',
  streaming: '',
  reasoning: '',
  tools: [],
  inputSaved: false,
  inputSavedConversationId: 0,
  error: null,
}

function accepts(state: AgentRunState, payload: AgentEnvelope) {
  if (!state.requestId) return false
  if (payload.requestId && payload.requestId !== state.requestId) return false
  return state.targetConversationId === 0 || payload.conversationId === state.targetConversationId
}

function bindConversation(state: AgentRunState, id: number) {
  return state.targetConversationId === 0 && id > 0 ? id : state.targetConversationId
}

export function agentRunReducer(state: AgentRunState, action: AgentRunAction): AgentRunState {
  switch (action.type) {
    case 'start':
      return {
        ...initialAgentRunState,
        requestId: action.requestId,
        conversationId: action.conversationId,
        targetConversationId: action.conversationId,
        phase: 'waiting',
      }
    case 'reset':
      return initialAgentRunState
    case 'chunk': {
      if (!accepts(state, action.payload)) return state
      return {
        ...state,
        targetConversationId: bindConversation(state, action.payload.conversationId),
        phase: 'responding',
        streaming: state.streaming + (action.payload.delta ?? ''),
      }
    }
    case 'input-saved':
      if (!state.requestId || (action.payload.requestId && action.payload.requestId !== state.requestId)) return state
      return {
        ...state,
        conversationId: action.payload.conversationId,
        targetConversationId: action.payload.conversationId,
        inputSaved: true,
        inputSavedConversationId: action.payload.conversationId,
      }
    case 'complete':
      return {
        ...state,
        conversationId: action.conversationId || state.conversationId,
        targetConversationId: action.conversationId || state.targetConversationId,
        phase: 'done',
        tools: state.tools.map((tool) => ({ ...tool, status: 'done' })),
      }
    case 'fail':
      return { ...state, phase: 'error', error: state.error ?? normalizeAgentRunError(action.error) }
    case 'event': {
      if (!action.payload.event || !accepts(state, action.payload)) return state
      const targetConversationId = bindConversation(state, action.payload.conversationId)
      const event = action.payload.event
      if (event.type === 'RUN_STARTED') return { ...state, targetConversationId, phase: 'waiting' }
      if (['REASONING_START', 'REASONING_MESSAGE_START', 'THINKING_START', 'THINKING_TEXT_MESSAGE_START'].includes(event.type)) {
        return { ...state, targetConversationId, phase: 'thinking' }
      }
      if (['REASONING_MESSAGE_CONTENT', 'REASONING_MESSAGE_CHUNK', 'THINKING_TEXT_MESSAGE_CONTENT'].includes(event.type)) {
        return { ...state, targetConversationId, phase: 'thinking', reasoning: state.reasoning + (event.delta ?? '') }
      }
      if (event.type === 'TOOL_CALL_START') {
        const id = event.toolCallId ?? `tool-${state.tools.length + 1}`
        if (state.tools.some((tool) => tool.id === id)) return { ...state, targetConversationId, phase: 'tool' }
        return {
          ...state,
          targetConversationId,
          phase: 'tool',
          tools: [...state.tools, { id, name: event.toolCallName ?? 'unknown_tool', args: '', result: '', status: 'preparing' }],
        }
      }
      if (event.type === 'TOOL_CALL_ARGS' && event.toolCallId) {
        return {
          ...state,
          targetConversationId,
          phase: 'tool',
          tools: state.tools.map((tool) => tool.id === event.toolCallId ? { ...tool, args: tool.args + (event.delta ?? '') } : tool),
        }
      }
      if (event.type === 'TOOL_CALL_END' && event.toolCallId) {
        return {
          ...state,
          targetConversationId,
          phase: 'tool',
          tools: state.tools.map((tool) => tool.id === event.toolCallId ? { ...tool, status: 'running' } : tool),
        }
      }
      if (event.type === 'TOOL_CALL_RESULT' && event.toolCallId) {
        return {
          ...state,
          targetConversationId,
          tools: state.tools.map((tool) => tool.id === event.toolCallId ? { ...tool, result: event.content ?? '', status: 'done' } : tool),
        }
      }
      if (event.type === 'TEXT_MESSAGE_START' || event.type === 'TEXT_MESSAGE_CONTENT') {
        return { ...state, targetConversationId, phase: 'responding' }
      }
      if (event.type === 'RUN_FINISHED') {
        return { ...state, targetConversationId, phase: 'done', tools: state.tools.map((tool) => ({ ...tool, status: 'done' })) }
      }
      if (event.type === 'RUN_ERROR') return {
        ...state,
        targetConversationId,
        phase: 'error',
        error: normalizeAgentRunError({ code: event.code, message: event.message }),
      }
      return state
    }
  }
}
