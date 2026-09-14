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
        error={null}
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

  it('shows a friendly inline error with collapsed technical details', async () => {
    render(
      <ConversationViewport
        messages={[]}
        streaming=""
        sending={false}
        phase="error"
        reasoning=""
        tools={[]}
        error={{ code: '429', message: 'raw provider rate limit response' }}
        pendingApproval={null}
        resolvingApproval={false}
        scrollRef={{ current: null }}
        quickPrompts={[]}
        onQuickPrompt={vi.fn()}
        onResolveApproval={vi.fn()}
      />,
    )
    expect(screen.getByText('请求过于频繁')).toBeVisible()
    expect(screen.queryByText('你好，我是 BlankMind')).not.toBeInTheDocument()
    const detail = screen.getByText('raw provider rate limit response')
    expect(detail).not.toBeVisible()
    await userEvent.click(screen.getByText('查看技术详情'))
    expect(detail).toBeVisible()
  })

  it('renders a persisted run error from the conversation snapshot', () => {
    render(
      <ConversationViewport
        messages={[{
          id: 'error-123',
          role: 'error',
          runError: { code: '403', message: 'raw provider access denied response' },
        }]}
        streaming=""
        sending={false}
        phase="idle"
        reasoning=""
        tools={[]}
        error={null}
        pendingApproval={null}
        resolvingApproval={false}
        scrollRef={{ current: null }}
        quickPrompts={[]}
        onQuickPrompt={vi.fn()}
        onResolveApproval={vi.fn()}
      />,
    )

    expect(screen.getByText('无权访问当前模型')).toBeVisible()
    expect(screen.getByText('raw provider access denied response')).not.toBeVisible()
  })
})
