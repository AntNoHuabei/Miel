import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

vi.mock('../../../theme/ThemeContext', () => ({
  BM_THEMES: [],
  useBMTheme: () => ({ themeId: 'system', setTheme: vi.fn() }),
}))

import { ConversationSidebar } from './ConversationSidebar'

describe('ConversationSidebar', () => {
  function renderSidebar(overrides: Partial<React.ComponentProps<typeof ConversationSidebar>> = {}) {
    const props: React.ComponentProps<typeof ConversationSidebar> = {
      activeView: 'chat',
      agentProfile: 'work',
      profileReady: true,
      sending: false,
      conversations: [{ id: 7, title: '已有会话', workspacePath: 'D:/code/BlankMind' }],
      workspaces: [{ name: 'BlankMind', path: 'D:/code/BlankMind', isCurrent: true }],
      currentConversationId: 0,
      reminderCount: 2,
      onDeleteCurrent: vi.fn(),
      onNavigate: vi.fn(),
      onChangeAgentProfile: vi.fn(),
      onNewChat: vi.fn(),
      onOpenConversation: vi.fn(),
      ...overrides,
    }
    render(<ConversationSidebar {...props} />)
    return props
  }

  it('preserves workspace expansion and conversation navigation', async () => {
    const openConversation = vi.fn()
    renderSidebar({ onOpenConversation: openConversation })

    await userEvent.click(screen.getByRole('button', { name: /已有会话/ }))
    expect(openConversation).toHaveBeenCalledWith(7)

    const workspace = screen.getByRole('button', { name: /BlankMind/ })
    await userEvent.click(workspace)
    expect(workspace).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('已有会话')).not.toBeInTheDocument()
  })

  it('groups conversations by their own workspace and collapses groups independently', async () => {
    renderSidebar({
      workspaces: [
        { name: 'Alpha', path: 'D:/work/alpha', isCurrent: true },
        { name: 'Beta', path: 'D:/work/beta', isCurrent: false },
      ],
      conversations: [
        { id: 1, title: 'Alpha 会话', workspacePath: 'D:/work/alpha' },
        { id: 2, title: 'Beta 会话', workspacePath: 'D:/work/beta' },
        { id: 3, title: '旧会话', workspacePath: '' },
      ],
    })

    expect(screen.getByText('Alpha 会话')).toBeInTheDocument()
    expect(screen.getByText('Beta 会话')).toBeInTheDocument()
    expect(screen.getByText('旧会话')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /不在工作区/ })).toBeInTheDocument()

    const alphaWorkspace = screen.getByText('Alpha').closest('button')
    expect(alphaWorkspace).not.toBeNull()
    await userEvent.click(alphaWorkspace!)
    expect(screen.queryByText('Alpha 会话')).not.toBeInTheDocument()
    expect(screen.getByText('Beta 会话')).toBeInTheDocument()
    expect(screen.getByText('旧会话')).toBeInTheDocument()
  })

  it('switches from Work to Coding through the upper-left profile menu', async () => {
    const changeProfile = vi.fn()
    renderSidebar({ onChangeAgentProfile: changeProfile })

    await userEvent.click(screen.getByRole('button', { name: /Work/ }))
    await userEvent.click(await screen.findByText('Coding'))

    expect(changeProfile).toHaveBeenCalledWith('coding')
  })
})
