import { useCallback, useEffect } from 'react'
import type { MouseEvent } from 'react'
import { Button, Tooltip } from 'antd'
import { BorderOutlined, CloseOutlined, MenuFoldOutlined, MenuUnfoldOutlined, MinusOutlined, PlusOutlined } from '@ant-design/icons'
import { Window as WailsWindow } from '@wailsio/runtime'
import SettingsView from './SettingsView'
import RemindersView from './RemindersView'
import ChatView from './ChatView'
import TodosView from './TodosView'
import MilestonesView from './MilestonesView'
import ScreenshotModal from '../components/ScreenshotModal'
import { parseEventData, useWailsEvent } from '../shared/wails/events'
import { refreshTodosIfLoaded } from '../features/todos/todoController'
import { useShellStore } from '../features/shell/shellStore'
import type { ReminderItem, ViewKey } from '../features/shell/shellStore'

const viewLabel: Record<ViewKey, string> = { chat: '对话', todos: '待办', milestones: '里程碑', reminders: '提醒中心', settings: '设置' }
export default function AppShell() {
  const view = useShellStore((state) => state.view)
  const sidebarOpen = useShellStore((state) => state.sidebarOpen)
  const reminders = useShellStore((state) => state.reminders)
  const navigate = useShellStore((state) => state.navigate)
  const toggleSidebar = useShellStore((state) => state.toggleSidebar)
  const requestNewChat = useShellStore((state) => state.requestNewChat)
  const openConversation = useShellStore((state) => state.openConversation)
  const addReminder = useShellStore((state) => state.addReminder)
  const clearReminders = useShellStore((state) => state.clearReminders)

  useEffect(() => {
    if (typeof Notification !== 'undefined' && Notification.permission === 'default') void Notification.requestPermission()
  }, [])

  useWailsEvent<string | ReminderItem>('reminders.changed', useCallback((raw) => {
    const reminder = parseEventData<ReminderItem>(raw)
    if (!reminder) return
    addReminder(reminder)
    if (typeof Notification !== 'undefined' && Notification.permission === 'granted') new Notification('Miel 提醒', { body: reminder.text })
  }, [addReminder]))
  useWailsEvent('todos.changed', refreshTodosIfLoaded, [])

  const feature = view === 'todos'
    ? <TodosView onOpenConversation={openConversation} />
    : view === 'milestones'
      ? <MilestonesView onGoTodos={() => navigate('todos')} />
      : view === 'reminders'
        ? <RemindersView items={reminders} onClear={clearReminders} />
        : view === 'settings' ? <SettingsView /> : null

  return (
    <div className="bm-window-shell">
      <header className="bm-window-titlebar">
        <div className="bm-window-title-actions">
          <Tooltip title={sidebarOpen ? '收起会话侧栏' : '展开会话侧栏'}><Button type="text" aria-label={sidebarOpen ? '收起会话侧栏' : '展开会话侧栏'} icon={sidebarOpen ? <MenuFoldOutlined /> : <MenuUnfoldOutlined />} onMouseUp={releaseMouseFocus} onClick={toggleSidebar} /></Tooltip>
          {!sidebarOpen && <Tooltip title="新建会话"><Button type="text" aria-label="新建会话" icon={<PlusOutlined />} onMouseUp={releaseMouseFocus} onClick={requestNewChat} /></Tooltip>}
          <span className="bm-window-title-name">Miel</span>
        </div>
        <div className="bm-window-title-drag" />
        <div className="bm-window-controls" aria-label="窗口控制">
          <Button type="text" className="is-minimise" aria-label="最小化" icon={<MinusOutlined />} onMouseUp={releaseMouseFocus} onClick={() => runWindowAction(() => WailsWindow.Minimise())} />
          <Button type="text" className="is-maximise" aria-label="最大化" icon={<BorderOutlined />} onMouseUp={releaseMouseFocus} onClick={() => runWindowAction(() => WailsWindow.ToggleMaximise())} />
          <Button type="text" className="is-close" aria-label="关闭" icon={<CloseOutlined />} onMouseUp={releaseMouseFocus} onClick={() => runWindowAction(() => WailsWindow.Close())} />
        </div>
      </header>
      <div className="bm-window-body bm-app-shell">
        <main className="bm-shell-viewport">
          <div className="bm-shell-chat-layer"><ChatView /></div>
          {view !== 'chat' && <section className={`bm-shell-feature-layer ${sidebarOpen ? 'is-sidebar-open' : ''} bm-feature-page`}><header className="bm-feature-header"><span>{viewLabel[view]}</span></header><div className="bm-feature-content">{feature}</div></section>}
        </main>
      </div>
      <ScreenshotModal />
    </div>
  )
}

function runWindowAction(action: () => Promise<void>) {
  const runtime = (window as typeof window & { _wails?: { environment?: unknown } })._wails
  if (runtime?.environment) void action()
}
function releaseMouseFocus(event: MouseEvent<HTMLButtonElement>) { event.currentTarget.blur() }
