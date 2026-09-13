import { describe, expect, it } from 'vitest'
import chatStyles from './chat-md.css?raw'

describe('chat message scrolling', () => {
  it('reserves stable space on both sides of the message scrollbar', () => {
    const messageRule = chatStyles.match(/\.bm-chat-messages\s*\{[^}]*\}/s)?.[0] ?? ''
    expect(messageRule).toMatch(/scrollbar-gutter:\s*stable both-edges\s*;/)
  })

  it('keeps the restored desktop conversation sidebar width', () => {
    const sidebarRule = chatStyles.match(/\.bm-chat-app-sidebar\s*\{[^}]*\}/s)?.[0] ?? ''
    expect(sidebarRule).toMatch(/flex:\s*0 0 252px\s*;/)
    expect(sidebarRule).toMatch(/width:\s*252px\s*;/)
    expect(sidebarRule).toMatch(/min-width:\s*252px\s*;/)
  })
})
