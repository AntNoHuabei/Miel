import { act, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useShellStore } from '../features/shell/shellStore'

vi.mock('../shared/wails/events', () => ({ useWailsEvent: vi.fn(), parseEventData: (value: unknown) => value }))
vi.mock('../features/todos/todoController', () => ({ refreshTodosIfLoaded: vi.fn() }))
vi.mock('./ChatView', () => ({ default: () => <div>chat stays mounted</div> }))
vi.mock('./SettingsView', () => ({ default: () => <div>settings page</div> }))
vi.mock('./TodosView', () => ({ default: () => <div>todos page</div> }))
vi.mock('./MilestonesView', () => ({ default: () => <div>milestones page</div> }))
vi.mock('./RemindersView', () => ({ default: () => <div>reminders page</div> }))
vi.mock('../components/ScreenshotModal', () => ({ default: () => null }))

import AppShell from './MainLayout'

describe('AppShell navigation', () => {
  beforeEach(() => useShellStore.setState({ view: 'chat', sidebarOpen: true, reminders: [], unread: 0 }))

  it('keeps chat mounted while feature pages cover the viewport', () => {
    render(<AppShell />)
    expect(screen.getByText('chat stays mounted')).toBeInTheDocument()

    const featurePages = [
      ['todos', 'todos page'],
      ['milestones', 'milestones page'],
      ['reminders', 'reminders page'],
      ['settings', 'settings page'],
    ] as const

    for (const [view, content] of featurePages) {
      act(() => useShellStore.getState().navigate(view))
      expect(screen.getByText('chat stays mounted')).toBeInTheDocument()
      expect(screen.getByText(content)).toBeInTheDocument()
      expect(screen.getByText(content).closest('.bm-shell-feature-layer')).toHaveClass('bm-feature-page')
    }
  })
})
