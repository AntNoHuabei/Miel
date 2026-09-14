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

export type AgentProcessStep =
  | { type: 'reasoning'; id: string; content: string; status: 'thinking' | 'done' }
  | { type: 'tool'; id: string }

export interface AgentProtocolEvent {
  type: string
  messageId?: string
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
  process: AgentProcessStep[]
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
  process: [],
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

function closeReasoning(process: AgentProcessStep[], messageId?: string): AgentProcessStep[] {
  return process.map((step) => step.type === 'reasoning' && (!messageId || step.id === messageId)
    ? { ...step, status: 'done' }
    : step)
}

function updateReasoning(state: AgentRunState, event: AgentProtocolEvent): AgentProcessStep[] {
  const active = state.phase === 'thinking'
    ? [...state.process].reverse().find((step) => step.type === 'reasoning' && step.status === 'thinking')
    : undefined
  const id = event.messageId ?? active?.id ?? `reasoning-${state.process.length + 1}`
  const existing = state.process.some((step) => step.type === 'reasoning' && step.id === id)
  if (!existing) return [...state.process, { type: 'reasoning', id, content: event.delta ?? '', status: 'thinking' }]
  return state.process.map((step) => step.type === 'reasoning' && step.id === id
    ? { ...step, content: step.content + (event.delta ?? '') }
    : step)
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
        process: closeReasoning(state.process),
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
        process: closeReasoning(state.process),
        tools: state.tools.map((tool) => ({ ...tool, status: 'done' })),
      }
    case 'fail':
      return { ...state, phase: 'error', process: closeReasoning(state.process), error: state.error ?? normalizeAgentRunError(action.error) }
    case 'event': {
      if (!action.payload.event || !accepts(state, action.payload)) return state
      const targetConversationId = bindConversation(state, action.payload.conversationId)
      const event = action.payload.event
      if (event.type === 'RUN_STARTED') return { ...state, targetConversationId, phase: 'waiting' }
      if (['REASONING_START', 'REASONING_MESSAGE_START', 'THINKING_START', 'THINKING_TEXT_MESSAGE_START'].includes(event.type)) {
        return { ...state, targetConversationId, phase: 'thinking', process: updateReasoning(state, event) }
      }
      if (['REASONING_MESSAGE_CONTENT', 'REASONING_MESSAGE_CHUNK', 'THINKING_TEXT_MESSAGE_CONTENT'].includes(event.type)) {
        return { ...state, targetConversationId, phase: 'thinking', process: updateReasoning(state, event) }
      }
      if (['REASONING_END', 'REASONING_MESSAGE_END', 'THINKING_END', 'THINKING_TEXT_MESSAGE_END'].includes(event.type)) {
        const process = closeReasoning(state.process, event.messageId)
        const thinking = process.some((step) => step.type === 'reasoning' && step.status === 'thinking')
        return { ...state, targetConversationId, process, phase: state.phase === 'thinking' && !thinking ? 'waiting' : state.phase }
      }
      if (event.type === 'TOOL_CALL_START') {
        const id = event.toolCallId ?? `tool-${state.tools.length + 1}`
        if (state.tools.some((tool) => tool.id === id)) return { ...state, targetConversationId, phase: 'tool' }
        return {
          ...state,
          targetConversationId,
          phase: 'tool',
          process: [...closeReasoning(state.process), { type: 'tool', id }],
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
        return { ...state, targetConversationId, phase: 'responding', process: closeReasoning(state.process) }
      }
      if (event.type === 'RUN_FINISHED') {
        return { ...state, targetConversationId, phase: 'done', process: closeReasoning(state.process), tools: state.tools.map((tool) => ({ ...tool, status: 'done' })) }
      }
      if (event.type === 'RUN_ERROR') return {
        ...state,
        targetConversationId,
        phase: 'error',
        process: closeReasoning(state.process),
        error: normalizeAgentRunError({ code: event.code, message: event.message }),
      }
      return state
    }
  }
}
