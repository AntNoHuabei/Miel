import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createConversationStore } from '../model/conversationStore'

const mocks = vi.hoisted(() => ({
  chat: vi.fn(),
  messagesSnapshot: vi.fn(),
  error: vi.fn(),
}))

vi.mock('antd', () => ({ App: { useApp: () => ({ message: { error: mocks.error } }) } }))
vi.mock('../../../shared/repositories', () => ({
  chatRepository: {
    chat: mocks.chat,
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
    mocks.messagesSnapshot.mockReset()
    mocks.error.mockReset()
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { callback(0); return 1 })
  })

  it('ignores a slower message load after another conversation is opened', async () => {
    const first = deferred<{ type: string; messages: Array<{ id: string; role: string; content: string }> }>()
    mocks.messagesSnapshot.mockImplementation((id: number) => id === 1
      ? first.promise
      : Promise.resolve({ type: 'MESSAGES_SNAPSHOT', messages: [{ id: 'second', role: 'assistant', content: 'second' }] }))
    const store = createConversationStore('runtime-load-race')
    const { result } = renderHook(() => useConversationRuntime(runtimeOptions(store)))

    act(() => result.current.openConversation(1))
    act(() => result.current.openConversation(2))
    await waitFor(() => expect(result.current.messages[0]?.id).toBe('second'))

    first.resolve({ type: 'MESSAGES_SNAPSHOT', messages: [{ id: 'first', role: 'assistant', content: 'first' }] })
    await act(async () => { await first.promise })
    expect(result.current.conversationId).toBe(2)
    expect(result.current.messages[0]?.id).toBe('second')
  })

  it('does not let a completed send reopen a conversation after navigation', async () => {
    const response = deferred<{ conversationId: number }>()
    mocks.chat.mockReturnValue(response.promise)
    mocks.messagesSnapshot.mockResolvedValue({
      type: 'MESSAGES_SNAPSHOT',
      messages: [{ id: 'selected', role: 'assistant', content: 'selected' }],
    })
    const store = createConversationStore('runtime-send-race')
    store.getState().setInput('hello')
    const options = runtimeOptions(store)
    const { result } = renderHook(() => useConversationRuntime(options))

    let sending!: Promise<void>
    act(() => { sending = result.current.send() })
    await waitFor(() => expect(mocks.chat).toHaveBeenCalledTimes(1))
    act(() => result.current.openConversation(9))
    await waitFor(() => expect(result.current.messages[0]?.id).toBe('selected'))

    response.resolve({ conversationId: 3 })
    await act(async () => { await sending })
    expect(result.current.conversationId).toBe(9)
    expect(result.current.sending).toBe(false)
    expect(result.current.messages[0]?.id).toBe('selected')
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
})
