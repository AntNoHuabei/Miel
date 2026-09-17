import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ConversationViewport } from './ConversationViewport'

describe('ConversationViewport', () => {
  it('renders a persisted plan card and exposes confirm and revise actions', async () => {
    const execute = vi.fn()
    const revise = vi.fn()
    render(
      <ConversationViewport
        conversationId={7}
        timeline={[{ kind: 'plan', sequence: 1, plan: { id: 3, messageId: 'plan-1', revision: 2, currentRevision: 2, status: 'pending', content: '# Delivery plan', generatedModel: 'model-a', createdAt: 1 } }]}
        streaming=""
        sending={false}
        phase="idle"
        process={[]}
        tools={[]}
        artifacts={[]}
        error={null}
        pendingApproval={null}
        resolvingApproval={false}
        scrollRef={{ current: null }}
        quickPrompts={[]}
        onQuickPrompt={vi.fn()}
        onResolveApproval={vi.fn()}
        onExecutePlan={execute}
        onRevisePlan={revise}
        onAbandonPlan={vi.fn()}
      />,
    )
    expect(screen.getByText('Plan v2')).toBeVisible()
    expect(screen.getByText('待确认')).toBeVisible()
    expect(screen.getByText('model-a')).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: /确认执行/ }))
    expect(execute).toHaveBeenCalledWith(expect.objectContaining({ id: 3, revision: 2 }))
    await userEvent.click(screen.getByRole('button', { name: /Revise/ }))
    await userEvent.type(screen.getByPlaceholderText('说明要调整的目标、范围或约束'), 'add migration coverage')
    await userEvent.click(screen.getByRole('button', { name: '生成新版本' }))
    expect(revise).toHaveBeenCalledWith(expect.objectContaining({ id: 3, revision: 2 }), 'add migration coverage')
  })

  it('renders assistant text and tools in event order', () => {
    const { container } = render(
      <ConversationViewport
        conversationId={7}
        timeline={[{ kind: 'message', sequence: 1, message: { id: 'user-1', role: 'user', content: 'request' } }]}
        streaming="beforeafter"
        sending
        phase="responding"
        process={[
          { type: 'text', id: 'm1', content: 'before', status: 'done' },
          { type: 'tool', id: 't1' },
          { type: 'text', id: 'm2', content: 'after', status: 'streaming' },
        ]}
        tools={[{ id: 't1', name: 'skill_run', args: '{}', result: 'ok', status: 'done' }]}
        artifacts={[]}
        error={null}
        pendingApproval={null}
        resolvingApproval={false}
        scrollRef={{ current: null }}
        quickPrompts={[]}
        onQuickPrompt={vi.fn()}
        onResolveApproval={vi.fn()}
      />,
    )
    const timeline = container.querySelector('.bm-agent-process')
    expect(timeline).toHaveTextContent(/before.*skill_run.*after/)
    expect(screen.queryAllByText('beforeafter')).toHaveLength(0)
  })

  it('keeps the plan in place and appends execution progress below it', () => {
    const { container } = render(
      <ConversationViewport
        conversationId={7}
        timeline={[
          { kind: 'plan', sequence: 1, plan: { id: 3, messageId: 'plan-1', revision: 1, currentRevision: 1, status: 'executing', content: '# Delivery plan\n\nImplement it.', generatedModel: 'model-a', executionModel: 'model-b', createdAt: 1 } },
          { kind: 'plan_execution', sequence: 2, execution: { id: 'execute-1', messageId: 'execute-1', content: '执行已批准计划 v1', planId: 3, revision: 1, createdAt: 2 } },
        ]}
        streaming=""
        sending
        phase="tool"
        process={[{ type: 'tool', id: 't1' }]}
        tools={[{ id: 't1', name: 'list_todos', args: '{}', result: '', status: 'running' }]}
        artifacts={[]}
        error={null}
        pendingApproval={null}
        resolvingApproval={false}
        scrollRef={{ current: null }}
        quickPrompts={[]}
        onQuickPrompt={vi.fn()}
        onResolveApproval={vi.fn()}
      />,
    )
    expect(container.querySelector('.bm-chat-message-track')).toHaveTextContent(/Plan v1.*执行已批准计划 v1.*正在使用工具.*读取待办/)
    expect(screen.queryByRole('button', { name: '重新执行' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Revise/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '废弃' })).not.toBeInTheDocument()
  })

  it('does not expose actions for an interrupted plan', () => {
    render(
      <ConversationViewport
        conversationId={7}
        timeline={[{ kind: 'plan', sequence: 1, plan: { id: 3, messageId: 'plan-1', revision: 1, currentRevision: 1, status: 'interrupted', content: '# Interrupted plan', generatedModel: 'model-a', executionModel: 'model-b', createdAt: 1 } }]}
        streaming=""
        sending={false}
        phase="idle"
        process={[]}
        tools={[]}
        artifacts={[]}
        error={null}
        pendingApproval={null}
        resolvingApproval={false}
        scrollRef={{ current: null }}
        quickPrompts={[]}
        onQuickPrompt={vi.fn()}
        onResolveApproval={vi.fn()}
        onExecutePlan={vi.fn()}
        onRevisePlan={vi.fn()}
        onAbandonPlan={vi.fn()}
      />,
    )
    expect(screen.getByText('已中断')).toBeVisible()
    expect(screen.queryByRole('button', { name: /执行/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Revise/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '废弃' })).not.toBeInTheDocument()
  })

  it('renders separate reasoning blocks in tool execution order and only opens active reasoning', async () => {
    const { container } = render(
      <ConversationViewport
        conversationId={7}
        timeline={[{ kind: 'message', sequence: 1, message: { id: 'user-1', role: 'user', content: 'request' } }]}
        streaming=""
        sending
        phase="thinking"
        process={[
          { type: 'reasoning', id: 'a', content: 'first thought', status: 'done' },
          { type: 'tool', id: 't1' },
          { type: 'reasoning', id: 'b', content: 'second thought', status: 'thinking' },
        ]}
        tools={[{ id: 't1', name: 'list_todos', args: '{}', result: 'ok', status: 'done' }]}
        artifacts={[]}
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
        conversationId={7}
        timeline={[{ kind: 'message', sequence: 1, message: { id: 'message-1', role: 'assistant', content: '已有回复' } }]}
        streaming=""
        sending={false}
        phase="idle"
        process={[]}
        tools={[]}
        artifacts={[]}
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
        conversationId={0}
        timeline={[]}
        streaming=""
        sending={false}
        phase="error"
        process={[]}
        tools={[]}
        artifacts={[]}
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
        conversationId={7}
        timeline={[{ kind: 'message', sequence: 1, message: {
          id: 'error-123',
          role: 'error',
          runError: { code: '403', message: 'raw provider access denied response' },
        } }]}
        streaming=""
        sending={false}
        phase="idle"
        process={[]}
        tools={[]}
        artifacts={[]}
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
