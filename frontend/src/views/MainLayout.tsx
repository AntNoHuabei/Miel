import { useCallback, useEffect, useState } from 'react'
import type { MouseEvent } from 'react'
import { Button, Tooltip } from 'antd'
import {
  BorderOutlined,
  CloseOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  MinusOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import SettingsView from './SettingsView'
import RemindersView from './RemindersView'
import ChatView from './ChatView'
import TodosView from './TodosView'
import MilestonesView from './MilestonesView'
import ScreenshotModal from '../components/ScreenshotModal'
import { useWailsEvent } from '../api'
import { Window as WailsWindow } from '@wailsio/runtime'

type ViewKey = 'chat' | 'todos' | 'milestones' | 'reminders' | 'settings'

export interface ReminderLite {
  type: 'dueSoon' | 'overdue'
  id: number
  title: string
  deadline: number
  ts: number
  text: string
}

const viewLabel: Record<ViewKey, string> = {
  chat: '对话',
  todos: '待办',
  milestones: '里程碑',
  reminders: '提醒中心',
  settings: '设置',
}

// 窗口外壳:标题栏 + 所有功能共享的应用侧栏与内容区。
export default function MainLayout() {
  const [view, setView] = useState<ViewKey>('chat')
  const [reminders, setReminders] = useState<ReminderLite[]>([])
  const [unread, setUnread] = useState(0)
  const [chatSidebarOpen, setChatSidebarOpen] = useState(false)
  const [newChatRequest, setNewChatRequest] = useState(0)

  // 申请系统通知权限(用于到期提醒的系统通知通道)
  useEffect(() => {
    if (typeof Notification !== 'undefined' && Notification.permission === 'default') {
      void Notification.requestPermission()
    }
  }, [])

  // 双通道提醒:Go 每分钟扫描 deadline 后经 reminders.changed 推送;
  // 此处负责“系统通知 + 应用内提醒中心/角标”。
  useWailsEvent<string>(
    'reminders.changed',
    useCallback((raw) => {
      try {
        const ev = JSON.parse(raw) as ReminderLite
        setReminders((prev) => [ev, ...prev].slice(0, 200))
        setUnread((n) => n + 1)
        if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
          new Notification('BlankMind 提醒', { body: ev.text })
        }
      } catch {
        // 忽略无法解析的载荷
      }
    }, []),
  )

  const navigate = useCallback((key: ViewKey) => {
    setView(key)
    if (key === 'reminders') setUnread(0)
  }, [])

  const featureContent =
    view === 'todos' ? (
      <TodosView />
    ) : view === 'milestones' ? (
      <MilestonesView onGoTodos={() => navigate('todos')} />
    ) : view === 'reminders' ? (
      <RemindersView items={reminders} onClear={() => setReminders([])} />
    ) : view === 'settings' ? (
      <SettingsView />
    ) : undefined

  return (
    <div className="bm-window-shell">
      <header className="bm-window-titlebar">
        <div className="bm-window-title-actions">
          <Tooltip title={chatSidebarOpen ? '收起侧栏' : '展开侧栏'}>
            <Button
              type="text"
              aria-label={chatSidebarOpen ? '收起侧栏' : '展开侧栏'}
              icon={chatSidebarOpen ? <MenuFoldOutlined /> : <MenuUnfoldOutlined />}
              onMouseUp={releaseMouseFocus}
              onClick={() => setChatSidebarOpen((open) => !open)}
            />
          </Tooltip>
          {!chatSidebarOpen && (
            <Tooltip title="新建会话">
              <Button
                type="text"
                aria-label="新建会话"
                icon={<PlusOutlined />}
                onMouseUp={releaseMouseFocus}
                onClick={() => setNewChatRequest((request) => request + 1)}
              />
            </Tooltip>
          )}
          <span className="bm-window-title-name">BlankMind</span>
        </div>
        <div className="bm-window-title-drag" />
        <div className="bm-window-controls" aria-label="窗口控制">
          <Button type="text" className="is-minimise" aria-label="最小化" icon={<MinusOutlined />} onMouseUp={releaseMouseFocus} onClick={() => runWindowAction(() => WailsWindow.Minimise())} />
          <Button type="text" className="is-maximise" aria-label="最大化" icon={<BorderOutlined />} onMouseUp={releaseMouseFocus} onClick={() => runWindowAction(() => WailsWindow.ToggleMaximise())} />
          <Button type="text" className="is-close" aria-label="关闭" icon={<CloseOutlined />} onMouseUp={releaseMouseFocus} onClick={() => runWindowAction(() => WailsWindow.Close())} />
        </div>
      </header>

      <div className="bm-window-body">
        <ChatView
          activeView={view}
          featureTitle={viewLabel[view]}
          featureContent={featureContent}
          reminderCount={unread}
          onNavigate={navigate}
          sidebarOpen={chatSidebarOpen}
          newChatRequest={newChatRequest}
        />
      </div>
      <ScreenshotModal />
    </div>
  )
}

function runWindowAction(action: () => Promise<void>) {
  const runtime = (window as typeof window & { _wails?: { environment?: unknown } })._wails
  if (runtime?.environment) void action()
}

function releaseMouseFocus(event: MouseEvent<HTMLButtonElement>) {
  event.currentTarget.blur()
}
