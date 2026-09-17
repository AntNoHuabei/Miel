import { act, renderHook, waitFor } from '@testing-library/react'
import type { UIEvent } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createConversationStore } from '../model/conversationStore'

const mocks = vi.hoisted(() => ({
  chat: vi.fn(),
  executePlan: vi.fn(),
  revisePlan: vi.fn(),
  abandonPlan: vi.fn(),
  messagesSnapshot: vi.fn(),
  error: vi.fn(),
}))

vi.mock('antd', () => ({ App: { useApp: () => ({ message: { error: mocks.error } }) } }))
vi.mock('../../../shared/repositories', () => ({
  chatRepository: {
    chat: mocks.chat,
    executePlan: mocks.executePlan,
    revisePlan: mocks.revisePlan,
    abandonPlan: mocks.abandonPlan,
    messagesSnapshot: mocks.messagesSnapshot,
  },
}))
vi.mock('../../../shared/wails/events', () => ({ useWailsEvent: vi.fn() }))

import { useConversationRuntime } from './useConversationRuntime'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

function runtimeOptions(store: ReturnType<typeof createConversationStore>) {
  return {
    store,
    attachments: [],
    supportsImages: true,
    requestPrefix: 'test',
    unsupportedImagesMessage: 'unsupported',
    consumeAttachments: vi.fn(),
    discardAttachments: vi.fn(),
    getRequestContext: vi.fn().mockResolvedValue({ reasoning: '', workspacePath: '', permissionSessionId: '' }),
  }
}

describe('useConversationRuntime', () => {
  beforeEach(() => {
    window.localStorage.clear()
    mocks.chat.mockReset()
    mocks.executePlan.mockReset()
    mocks.revisePlan.mockReset()
    mocks.abandonPlan.mockReset()
    mocks.messagesSnapshot.mockReset()
    mocks.error.mockReset()
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { callback(0); return 1 })
  })

  it('ignores a slower message load after another conversation is opened', async () => {
    const first = deferred<{ type: string; timeline: Array<{ kind: 'message'; sequence: number; message: { id: string; role: string; content: string } }> }>()
    mocks.messagesSnapshot.mockImplementation((id: number) => id === 1
      ? first.promise
      : Promise.resolve({ type: 'CONVERSATION_SNAPSHOT', timeline: [{ kind: 'message', sequence: 1, message: { id: 'second', role: 'assistant', content: 'second' } }] }))
    const store = createConversationStore('runtime-load-race')
    const { result } = renderHook(() => useConversationRuntime(runtimeOptions(store)))

    act(() => result.current.openConversation(1))
    act(() => result.current.openConversation(2))
    await waitFor(() => expect(result.current.timeline[0]?.kind === 'message' && result.current.timeline[0].message.id).toBe('second'))

    first.resolve({ type: 'CONVERSATION_SNAPSHOT', timeline: [{ kind: 'message', sequence: 1, message: { id: 'first', role: 'assistant', content: 'first' } }] })
    await act(async () => { await first.promise })
    expect(result.current.conversationId).toBe(2)
    expect(result.current.timeline[0]?.kind === 'message' && result.current.timeline[0].message.id).toBe('second')
  })

  it('does not let a completed send reopen a conversation after navigation', async () => {
    const response = deferred<{ conversationId: number }>()
    mocks.chat.mockReturnValue(response.promise)
    mocks.messagesSnapshot.mockResolvedValue({
      type: 'CONVERSATION_SNAPSHOT',
      timeline: [{ kind: 'message', sequence: 1, message: { id: 'selected', role: 'assistant', content: 'selected' } }],
    })
    const store = createConversationStore('runtime-send-race')
    store.getState().setInput('hello')
    const options = runtimeOptions(store)
    const { result } = renderHook(() => useConversationRuntime(options))

    let sending!: Promise<void>
    act(() => { sending = result.current.send() })
    await waitFor(() => expect(mocks.chat).toHaveBeenCalledTimes(1))
    act(() => result.current.openConversation(9))
    await waitFor(() => expect(result.current.timeline[0]?.kind === 'message' && result.current.timeline[0].message.id).toBe('selected'))

    response.resolve({ conversationId: 3 })
    await act(async () => { await sending })
    expect(result.current.conversationId).toBe(9)
    expect(result.current.sending).toBe(false)
    expect(result.current.timeline[0]?.kind === 'message' && result.current.timeline[0].message.id).toBe('selected')
  })

  it('keeps chat failures inline without showing a toast', async () => {
    mocks.chat.mockRejectedValue(new Error('429 Too Many Requests'))
    const store = createConversationStore('runtime-inline-error')
    store.getState().setInput('hello')
    const { result } = renderHook(() => useConversationRuntime(runtimeOptions(store)))

    await act(async () => { await result.current.send() })

    expect(mocks.error).not.toHaveBeenCalled()
    expect(result.current.sending).toBe(false)
    expect(result.current.run).toMatchObject({
      phase: 'error',
      error: { code: '429', message: 'Error: 429 Too Many Requests' },
    })
  })

  it('sends the selected Plan mode and executes a plan through the same runtime', async () => {
    mocks.chat.mockResolvedValue({ conversationId: 4 })
    mocks.executePlan.mockResolvedValue({ conversationId: 4 })
    mocks.messagesSnapshot.mockResolvedValue({ type: 'CONVERSATION_SNAPSHOT', timeline: [] })
    const store = createConversationStore('runtime-plan')
    store.getState().setInput('design it')
    const options = runtimeOptions(store)
    options.getRequestContext.mockResolvedValue({ reasoning: 'high', workspacePath: 'D:/repo', permissionSessionId: 'permission', mode: 'plan' })
    const { result } = renderHook(() => useConversationRuntime(options))

    await act(async () => { await result.current.send() })
    expect(mocks.chat).toHaveBeenCalledWith(expect.objectContaining({ mode: 'plan', planId: 0, planRevision: 0 }))

    await act(async () => {
      await result.current.runPlanAction('execute', { id: 9, messageId: 'plan-9', revision: 1, currentRevision: 1, status: 'pending', content: '# Plan', generatedModel: 'model-a', createdAt: 1 })
    })
    expect(mocks.executePlan).toHaveBeenCalledWith(expect.objectContaining({ planId: 9, revision: 1, workspacePath: 'D:/repo' }))
  })

  it('only follows output while the viewport remains near the bottom', async () => {
    const store = createConversationStore('runtime-scroll-follow')
    const { result } = renderHook(() => useConversationRuntime(runtimeOptions(store)))
    const viewport = document.createElement('div')
    Object.defineProperties(viewport, {
      scrollHeight: { configurable: true, value: 1000 },
      clientHeight: { configurable: true, value: 200 },
      scrollTop: { configurable: true, writable: true, value: 400 },
    })
    result.current.scrollRef.current = viewport
    act(() => store.getState().dispatchRun({ type: 'start', requestId: 'scroll-test', conversationId: 0 }))
    await waitFor(() => expect(viewport.scrollTop).toBe(1000))

    viewport.scrollTop = 400
    act(() => result.current.onMessagesScroll({ currentTarget: viewport } as UIEvent<HTMLDivElement>))
    act(() => store.getState().dispatchRun({ type: 'chunk', payload: { conversationId: 0, delta: 'new output' } }))
    await waitFor(() => expect(viewport.scrollTop).toBe(400))

    viewport.scrollTop = 800
    act(() => result.current.onMessagesScroll({ currentTarget: viewport } as UIEvent<HTMLDivElement>))
    act(() => store.getState().dispatchRun({ type: 'chunk', payload: { conversationId: 0, delta: 'more output' } }))
    await waitFor(() => expect(viewport.scrollTop).toBe(1000))
  })
})
