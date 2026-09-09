import { useCallback, useEffect, useState } from 'react'
import { Badge, Button, Flex, Layout, Popover, Space, Tooltip, Typography } from 'antd'
import {
  BellOutlined,
  BgColorsOutlined,
  CheckSquareOutlined,
  CommentOutlined,
  FlagOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import SettingsView from './SettingsView'
import RemindersView from './RemindersView'
import ChatView from './ChatView'
import TodosView from './TodosView'
import MilestonesView from './MilestonesView'
import ScreenshotModal from '../components/ScreenshotModal'
import { useWailsEvent } from '../api'
import { BM_THEMES, useBMTheme } from '../theme/ThemeContext'

const { Sider, Header, Content } = Layout

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

// 极简窄侧栏:上部为功能菜单(图标),下部为皮肤切换与设置。
export default function MainLayout() {
  const [view, setView] = useState<ViewKey>('chat')
  const [reminders, setReminders] = useState<ReminderLite[]>([])
  const [unread, setUnread] = useState(0)
  const { theme } = useBMTheme()

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

  const nav = (key: ViewKey, icon: React.ReactNode, tip: string, count?: number) => (
    <Tooltip title={tip} placement="right">
      <Button
        type={view === key ? 'primary' : 'text'}
        shape={view === key ? undefined : 'circle'}
        icon={count ? <Badge count={count} size="small" offset={[2, -2]}>{icon}</Badge> : icon}
        onClick={() => {
          setView(key)
          if (key === 'reminders') setUnread(0)
        }}
      />
    </Tooltip>
  )

  return (
    <Layout style={{ height: '100vh', background: 'var(--bm-app-bg)' }}>
      <Sider
        width={64}
        theme={theme.dark ? 'dark' : 'light'}
        style={{ borderRight: '1px solid var(--bm-border)' }}
      >
        <Flex vertical justify="space-between" style={{ height: '100%' }}>
          {/* 上部:标识 + 功能菜单 */}
          <Flex vertical align="center" gap={6} style={{ paddingTop: 14 }}>
            <div style={{ fontSize: 22, lineHeight: 1, marginBottom: 10 }}>🧠</div>
            <Space direction="vertical" size={6}>
              {nav('chat', <CommentOutlined />, '对话')}
              {nav('todos', <CheckSquareOutlined />, '待办')}
              {nav('milestones', <FlagOutlined />, '里程碑')}
              {nav('reminders', <BellOutlined />, '提醒', unread)}
            </Space>
          </Flex>

          {/* 下部:皮肤切换 + 设置 */}
          <Flex vertical align="center" gap={6} style={{ paddingBottom: 14 }}>
            <ThemeSwitcher />
            {nav('settings', <SettingOutlined />, '设置')}
          </Flex>
        </Flex>
      </Sider>

      <Layout style={{ background: 'var(--bm-content-bg)' }}>
        <Header
          style={{
            background: 'var(--bm-header-bg)',
            padding: '0 16px',
            borderBottom: '1px solid var(--bm-border)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            height: 44,
            lineHeight: '44px',
          }}
        >
          <Typography.Text strong style={{ fontSize: 14 }}>
            {viewLabel[view]}
          </Typography.Text>
        </Header>
        <Content style={{ overflow: 'auto', background: 'var(--bm-content-bg)' }}>
          {/* 对话页保持挂载,切换功能时保留输入框、当前会话和流式 UI 状态。 */}
          <div
            style={{
              display: view === 'chat' ? 'block' : 'none',
              height: '100%',
            }}
          >
            <ChatView />
          </div>
          {view === 'todos' && <TodosView />}
          {view === 'milestones' && <MilestonesView onGoTodos={() => setView('todos')} />}
          {view === 'reminders' && (
            <RemindersView items={reminders} onClear={() => setReminders([])} />
          )}
          {view === 'settings' && <SettingsView />}
        </Content>
      </Layout>
      <ScreenshotModal />
    </Layout>
  )
}

// 皮肤快速切换(侧栏底部):弹出面板列出全部内置主题。
function ThemeSwitcher() {
  const { themeId, setTheme } = useBMTheme()
  const panel = (
    <Flex vertical gap={4}>
      <Typography.Text type="secondary" style={{ fontSize: 12, padding: '0 4px 4px' }}>
        选择皮肤
      </Typography.Text>
      {BM_THEMES.map((t) => (
        <Button
          key={t.id}
          size="small"
          type={themeId === t.id ? 'primary' : 'text'}
          style={{ justifyContent: 'flex-start' }}
          onClick={() => void setTheme(t.id)}
        >
          {t.dark ? '🌙' : '☀️'} {t.name}
        </Button>
      ))}
    </Flex>
  )
  return (
    <Tooltip title="皮肤" placement="right">
      <Popover content={panel} placement="rightBottom" trigger="click">
        <Button type="text" icon={<BgColorsOutlined />} style={{ color: 'inherit' }} />
      </Popover>
    </Tooltip>
  )
}
