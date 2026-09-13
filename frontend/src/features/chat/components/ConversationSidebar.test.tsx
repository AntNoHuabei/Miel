import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

vi.mock('../../../theme/ThemeContext', () => ({
  BM_THEMES: [],
  useBMTheme: () => ({ themeId: 'system', setTheme: vi.fn() }),
}))

import { ConversationSidebar } from './ConversationSidebar'

describe('ConversationSidebar', () => {
  it('preserves workspace expansion and conversation navigation', async () => {
    const openConversation = vi.fn()
    render(
      <ConversationSidebar
        activeView="chat"
        conversations={[{ id: 7, title: '已有会话' }]}
        currentConversationId={0}
        currentWorkspace={{ name: 'BlankMind', path: 'D:/code/BlankMind', isCurrent: true }}
        reminderCount={2}
        onDeleteCurrent={vi.fn()}
        onNavigate={vi.fn()}
        onNewChat={vi.fn()}
        onOpenConversation={openConversation}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: /已有会话/ }))
    expect(openConversation).toHaveBeenCalledWith(7)

    const workspace = screen.getByRole('button', { name: /BlankMind/ })
    await userEvent.click(workspace)
    expect(workspace).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('已有会话')).not.toBeInTheDocument()
  })
})
