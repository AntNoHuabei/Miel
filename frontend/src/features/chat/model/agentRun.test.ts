import { describe, expect, it } from 'vitest'
import { agentRunReducer, initialAgentRunState } from './agentRun'
import type { AgentProtocolEvent } from './agentRun'

const started = () => agentRunReducer(initialAgentRunState, { type: 'start', requestId: 'r1', conversationId: 0 })

describe('agentRunReducer', () => {
  const replay = (events: AgentProtocolEvent[]) => events.reduce((state, event) => agentRunReducer(state, {
    type: 'event', payload: { requestId: 'r1', conversationId: 3, event },
  }), started())

  it('keeps multiple reasoning messages separate and ordered around tools', () => {
    const state = replay([
      { type: 'REASONING_START', messageId: 'a' },
      { type: 'REASONING_MESSAGE_START', messageId: 'a' },
      { type: 'REASONING_MESSAGE_CONTENT', messageId: 'a', delta: 'first ' },
      { type: 'REASONING_MESSAGE_CONTENT', messageId: 'a', delta: 'thought' },
      { type: 'REASONING_MESSAGE_END', messageId: 'a' },
      { type: 'REASONING_END', messageId: 'a' },
      { type: 'TOOL_CALL_START', toolCallId: 't1' },
      { type: 'TOOL_CALL_START', toolCallId: 't1' },
      { type: 'REASONING_START', messageId: 'b' },
      { type: 'REASONING_MESSAGE_START', messageId: 'b' },
      { type: 'REASONING_MESSAGE_CONTENT', messageId: 'b', delta: 'second thought' },
    ])
    expect(state.process).toEqual([
      { type: 'reasoning', id: 'a', content: 'first thought', status: 'done' },
      { type: 'tool', id: 't1' },
      { type: 'reasoning', id: 'b', content: 'second thought', status: 'thinking' },
    ])
  })

  it('routes interleaved content and end events by message ID', () => {
    const state = replay([
      { type: 'REASONING_MESSAGE_CONTENT', messageId: 'a', delta: 'A' },
      { type: 'REASONING_MESSAGE_CONTENT', messageId: 'b', delta: 'B' },
      { type: 'REASONING_MESSAGE_CONTENT', messageId: 'a', delta: '2' },
      { type: 'REASONING_MESSAGE_END', messageId: 'a' },
    ])
    expect(state.phase).toBe('thinking')
    expect(state.process).toEqual([
      { type: 'reasoning', id: 'a', content: 'A2', status: 'done' },
      { type: 'reasoning', id: 'b', content: 'B', status: 'thinking' },
    ])
  })

  it.each(['REASONING', 'THINKING'])('uses lifecycle and tool boundaries when %s events omit IDs', (protocol) => {
    const message = protocol === 'REASONING' ? 'REASONING_MESSAGE' : 'THINKING_TEXT_MESSAGE'
    const state = replay([
      { type: `${protocol}_START` },
      { type: `${message}_START` },
      { type: `${message}_CONTENT`, delta: 'first' },
      { type: `${message}_END` },
      { type: `${protocol}_END` },
      { type: `${message}_CONTENT`, delta: 'second' },
      { type: 'TOOL_CALL_START', toolCallId: 't1' },
      { type: `${message}_CONTENT`, delta: 'third' },
      { type: 'RUN_FINISHED' },
    ])
    expect(state.process.filter((step) => step.type === 'reasoning')).toMatchObject([
      { content: 'first', status: 'done' },
      { content: 'second', status: 'done' },
      { content: 'third', status: 'done' },
    ])
  })

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
    expect(state.process).toEqual([
      { type: 'reasoning', id: 'reasoning-1', content: 'why', status: 'done' },
      { type: 'tool', id: 't1' },
    ])
    expect(state.tools[0]).toMatchObject({ name: 'list_todos', args: '{}', result: 'ok', status: 'done' })
  })

  it('tracks persisted input and terminal states', () => {
    let state = started()
    state = agentRunReducer(state, { type: 'input-saved', payload: { requestId: 'r1', conversationId: 9 } })
    expect(state).toMatchObject({ inputSaved: true, inputSavedConversationId: 9, conversationId: 9 })
    state = agentRunReducer(state, { type: 'complete', conversationId: 9 })
    expect(state.phase).toBe('done')
    state = agentRunReducer(state, { type: 'fail', error: 'network' })
    expect(state).toMatchObject({ phase: 'error', error: { code: 'unknown_error', message: 'network' } })
  })

  it('preserves an upstream RUN_ERROR when the request promise also rejects', () => {
    let state = started()
    state = agentRunReducer(state, {
      type: 'event',
      payload: {
        requestId: 'r1',
        conversationId: 9,
        event: { type: 'RUN_ERROR', message: '403 model is only available on agentic harnesses' },
      },
    })
    state = agentRunReducer(state, { type: 'fail', error: '模型未返回有效内容' })
    expect(state).toMatchObject({
      phase: 'error',
      error: { code: '403', message: '403 model is only available on agentic harnesses' },
    })
  })
})
