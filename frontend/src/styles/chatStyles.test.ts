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

  it('uses the titlebar surface for the sidebar and a rounded main content surface', () => {
    const chatRule = chatStyles.match(/\.bm-chat\s*\{[^}]*\}/s)?.[0] ?? ''
    const shellRule = chatStyles.match(/\.bm-chat-shell\s*\{[^}]*\}/s)?.[0] ?? ''
    const sidebarRule = chatStyles.match(/\.bm-chat-app-sidebar\s*\{[^}]*\}/s)?.[0] ?? ''
    const mainRule = chatStyles.match(/\.bm-chat-main-shell\s*\{[^}]*\}/s)?.[0] ?? ''

    expect(chatRule).toMatch(/--bm-chat-sidebar:\s*var\(--bm-header-bg,[^)]+\)\s*;/)
    expect(shellRule).toMatch(/background:\s*var\(--bm-chat-sidebar\)\s*!important\s*;/)
    expect(sidebarRule).toMatch(/border-right:\s*0\s*;/)
    expect(mainRule).toMatch(/border-top-left-radius:\s*18px\s*;/)
  })
})
