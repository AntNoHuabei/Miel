import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ConversationViewport } from './ConversationViewport'

describe('ConversationViewport', () => {
  it('keeps permission approval actionable in the message stream', async () => {
    const resolve = vi.fn()
    render(
      <ConversationViewport
        messages={[]}
        streaming=""
        sending={false}
        phase="idle"
        reasoning=""
        tools={[]}
        pendingApproval={{ id: 'approval-1', sessionId: 'session-1', tool: 'create_document', operation: 'write', workspacePath: 'D:/code/BlankMind', target: 'report.md', riskLevel: 'medium', outsideWorkspace: false, scopeRoot: 'D:/code/BlankMind', expiresAt: '' }}
        resolvingApproval={false}
        scrollRef={{ current: null }}
        quickPrompts={[]}
        onQuickPrompt={vi.fn()}
        onResolveApproval={resolve}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: '仅这一次' }))
    expect(resolve).toHaveBeenCalledWith('once')
  })
})
