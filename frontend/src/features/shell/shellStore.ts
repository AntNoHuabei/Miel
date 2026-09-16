import { create } from 'zustand'

export type ViewKey = 'chat' | 'todos' | 'milestones' | 'reminders' | 'artifacts' | 'vocabulary' | 'settings'

export interface ReminderItem {
  type: 'dueSoon' | 'overdue'
  id: number
  title: string
  deadline: number
  ts: number
  text: string
}

interface ShellState {
  view: ViewKey
  sidebarOpen: boolean
  reminders: ReminderItem[]
  unread: number
  newChatVersion: number
  openConversationRequest: { id: number; version: number }
  navigate: (view: ViewKey) => void
  toggleSidebar: () => void
  requestNewChat: () => void
  openConversation: (id: number) => void
  addReminder: (reminder: ReminderItem) => void
  clearReminders: () => void
}

export const useShellStore = create<ShellState>((set) => ({
  view: 'chat',
  sidebarOpen: false,
  reminders: [],
  unread: 0,
  newChatVersion: 0,
  openConversationRequest: { id: 0, version: 0 },
  navigate: (view) => set((state) => ({ view, unread: view === 'reminders' ? 0 : state.unread })),
  toggleSidebar: () => set((state) => ({ sidebarOpen: !state.sidebarOpen })),
  requestNewChat: () => set((state) => ({ view: 'chat', newChatVersion: state.newChatVersion + 1 })),
  openConversation: (id) => set((state) => ({ view: 'chat', openConversationRequest: { id, version: state.openConversationRequest.version + 1 } })),
  addReminder: (reminder) => set((state) => ({ reminders: [reminder, ...state.reminders].slice(0, 200), unread: state.unread + 1 })),
  clearReminders: () => set({ reminders: [] }),
}))
