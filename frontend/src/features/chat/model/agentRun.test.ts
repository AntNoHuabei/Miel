import { describe, expect, it } from 'vitest'
import { agentRunReducer, initialAgentRunState } from './agentRun'

const started = () => agentRunReducer(initialAgentRunState, { type: 'start', requestId: 'r1', conversationId: 0 })

describe('agentRunReducer', () => {
  it('filters unrelated requests and binds a new conversation from the first event', () => {
    const state = started()
    expect(agentRunReducer(state, { type: 'chunk', payload: { requestId: 'other', conversationId: 8, delta: 'x' } })).toBe(state)
    const next = agentRunReducer(state, { type: 'chunk', payload: { requestId: 'r1', conversationId: 8, delta: 'a' } })
    expect(next.targetConversationId).toBe(8)
    expect(next.streaming).toBe('a')
  })

  it('accumulates reasoning and tool execution state', () => {
    let state = started()
    state = agentRunReducer(state, { type: 'event', payload: { requestId: 'r1', conversationId: 3, event: { type: 'REASONING_MESSAGE_CONTENT', delta: 'why' } } })
    state = agentRunReducer(state, { type: 'event', payload: { requestId: 'r1', conversationId: 3, event: { type: 'TOOL_CALL_START', toolCallId: 't1', toolCallName: 'list_todos' } } })
    state = agentRunReducer(state, { type: 'event', payload: { requestId: 'r1', conversationId: 3, event: { type: 'TOOL_CALL_ARGS', toolCallId: 't1', delta: '{}' } } })
    state = agentRunReducer(state, { type: 'event', payload: { requestId: 'r1', conversationId: 3, event: { type: 'TOOL_CALL_RESULT', toolCallId: 't1', content: 'ok' } } })
    expect(state.reasoning).toBe('why')
    expect(state.tools[0]).toMatchObject({ name: 'list_todos', args: '{}', result: 'ok', status: 'done' })
  })

  it('tracks persisted input and terminal states', () => {
    let state = started()
    state = agentRunReducer(state, { type: 'input-saved', payload: { requestId: 'r1', conversationId: 9 } })
    expect(state).toMatchObject({ inputSaved: true, inputSavedConversationId: 9, conversationId: 9 })
    state = agentRunReducer(state, { type: 'complete', conversationId: 9 })
    expect(state.phase).toBe('done')
    state = agentRunReducer(state, { type: 'fail', error: 'network' })
    expect(state).toMatchObject({ phase: 'error', error: 'network' })
  })
})
