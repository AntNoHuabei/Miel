import { beforeEach, describe, expect, it } from 'vitest'
import { useShellStore } from './shellStore'

describe('shell store', () => {
  beforeEach(() => useShellStore.setState({ view: 'chat', sidebarOpen: false, reminders: [], unread: 0, newChatVersion: 0, openConversationRequest: { id: 0, version: 0 } }))

  it('navigates directly and clears reminder unread state', () => {
    useShellStore.getState().addReminder({ type: 'dueSoon', id: 1, title: 'T', deadline: 1, ts: 1, text: 'T' })
    expect(useShellStore.getState().unread).toBe(1)
    useShellStore.getState().navigate('reminders')
    expect(useShellStore.getState()).toMatchObject({ view: 'reminders', unread: 0 })
  })

  it('opens the same conversation repeatedly without request counters in props', () => {
    useShellStore.getState().openConversation(4)
    const first = useShellStore.getState().openConversationRequest.version
    useShellStore.getState().openConversation(4)
    expect(useShellStore.getState()).toMatchObject({ view: 'chat', openConversationRequest: { id: 4, version: first + 1 } })
  })
})
