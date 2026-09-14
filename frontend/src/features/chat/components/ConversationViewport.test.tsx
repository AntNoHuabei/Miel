import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ConversationViewport } from './ConversationViewport'

describe('ConversationViewport', () => {
  it('renders separate reasoning blocks in tool execution order and only opens active reasoning', async () => {
    const { container } = render(
      <ConversationViewport
        messages={[{ id: 'user-1', role: 'user', content: 'request' }]}
        streaming=""
        sending
        phase="thinking"
        process={[
          { type: 'reasoning', id: 'a', content: 'first thought', status: 'done' },
          { type: 'tool', id: 't1' },
          { type: 'reasoning', id: 'b', content: 'second thought', status: 'thinking' },
        ]}
        tools={[{ id: 't1', name: 'list_todos', args: '{}', result: 'ok', status: 'done' }]}
        error={null}
        pendingApproval={null}
        resolvingApproval={false}
        scrollRef={{ current: null }}
        quickPrompts={[]}
        onQuickPrompt={vi.fn()}
        onResolveApproval={vi.fn()}
      />,
    )
    const details = container.querySelectorAll<HTMLDetailsElement>('.bm-agent-detail')
    expect(details).toHaveLength(3)
    expect(details[0]).toHaveTextContent('first thought')
    expect(details[1]).toHaveTextContent('读取待办')
    expect(details[2]).toHaveTextContent('second thought')
    expect(details[0].open).toBe(false)
    expect(details[2].open).toBe(true)
    await userEvent.click(details[0].querySelector('summary')!)
    expect(details[0].open).toBe(true)
  })

  it('opens permission approval in a modal above existing messages', async () => {
    const resolve = vi.fn()
    render(
      <ConversationViewport
        messages={[{ id: 'message-1', role: 'assistant', content: '已有回复' }]}
        streaming=""
        sending={false}
        phase="idle"
        process={[]}
        tools={[]}
        error={null}
        pendingApproval={{ id: 'approval-1', sessionId: 'session-1', tool: 'list_directory', operation: 'list', workspacePath: 'D:/code/BlankMind', target: 'C:/Users/lxl/Desktop', riskLevel: 'low', outsideWorkspace: true, scopeRoot: 'D:/code/BlankMind', expiresAt: new Date(Date.now() + 5 * 60 * 1000).toISOString() }}
        resolvingApproval={false}
        scrollRef={{ current: null }}
        quickPrompts={[]}
        onQuickPrompt={vi.fn()}
        onResolveApproval={resolve}
      />,
    )
    const approval = screen.getByLabelText('等待权限批准')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('已有回复')).toBeVisible()
    expect(approval).toBeInTheDocument()
    expect(screen.getByText('操作')).toBeInTheDocument()
    expect(screen.getByText('范围')).toBeInTheDocument()
    expect(screen.getByText('目标')).toBeInTheDocument()
    expect(screen.getByText('工作区外')).toBeInTheDocument()
    expect(screen.getByText('C:/Users/lxl/Desktop')).toBeInTheDocument()
    expect(screen.getByText(/\d{2}:\d{2} 后自动拒绝/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /拒\s*绝/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '本会话允许' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '允许一次' }))
    expect(resolve).toHaveBeenCalledWith('once')
  })

  it('shows a friendly inline error with collapsed technical details', async () => {
    render(
      <ConversationViewport
        messages={[]}
        streaming=""
        sending={false}
        phase="error"
        process={[]}
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
    expect(screen.queryByText('你好，我是 Miel')).not.toBeInTheDocument()
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
        process={[]}
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
