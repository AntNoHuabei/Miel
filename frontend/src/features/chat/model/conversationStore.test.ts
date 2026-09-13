import { beforeEach, describe, expect, it } from 'vitest'
import { createConversationStore } from './conversationStore'

describe('conversation store factory', () => {
  beforeEach(() => window.localStorage.clear())

  it('creates isolated main and quick assistant state', () => {
    const main = createConversationStore('main-test')
    const quick = createConversationStore('quick-test')
    main.getState().setConversation(12)
    main.getState().setInput('main')
    expect(quick.getState()).toMatchObject({ conversationId: 0, input: '' })
    expect(window.localStorage.getItem('main-test')).toBe('12')
  })

  it('restores the persisted conversation pointer', () => {
    window.localStorage.setItem('restore-test', '7')
    expect(createConversationStore('restore-test').getState().conversationId).toBe(7)
  })
})
