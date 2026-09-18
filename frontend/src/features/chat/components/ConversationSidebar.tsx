import { useMemo, useState } from 'react'
import { Button, Dropdown, Flex, Popover, Tooltip, Typography } from 'antd'
import {
  BellOutlined,
  BgColorsOutlined,
  CheckOutlined,
  CheckSquareOutlined,
  DeleteOutlined,
  DownOutlined,
  EditOutlined,
  FlagOutlined,
  FileDoneOutlined,
  FolderOpenOutlined,
  HistoryOutlined,
  ReadOutlined,
  RightOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import type { WorkspaceLite } from '../../../api'
import type { AgentProfile } from '../../../shared/types/chat'
import type { ViewKey } from '../../shell/shellStore'
import { BM_THEMES, useBMTheme } from '../../../theme/ThemeContext'

const { Text } = Typography

interface ConversationSummary {
  id: number
  title: string
  workspacePath: string
}

interface WorkspaceGroup {
  key: string
  name: string
  conversations: ConversationSummary[]
}

interface ConversationSidebarProps {
  activeView: ViewKey
  agentProfile: AgentProfile
  profileReady: boolean
  sending: boolean
  conversations: ConversationSummary[]
  workspaces: WorkspaceLite[]
  currentConversationId: number
  reminderCount: number
  onDeleteCurrent: () => void | Promise<void>
  onNavigate: (view: ViewKey) => void
  onChangeAgentProfile: (profile: AgentProfile) => void | Promise<unknown>
  onNewChat: () => void
  onOpenConversation: (id: number) => void
}

export function ConversationSidebar({
  activeView,
  agentProfile,
  profileReady,
  sending,
  conversations,
  workspaces,
  currentConversationId,
  reminderCount,
  onDeleteCurrent,
  onNavigate,
  onChangeAgentProfile,
  onNewChat,
  onOpenConversation,
}: ConversationSidebarProps) {
  const [workspaceOpen, setWorkspaceOpen] = useState<Record<string, boolean>>({})
  const [scrolled, setScrolled] = useState(false)
  const workspaceGroups = useMemo(() => {
    const groups = workspaces.map<WorkspaceGroup>((workspace) => ({ key: `workspace:${workspace.path}`, name: workspace.name, conversations: [] }))
    const byPath = new Map(workspaces.map((workspace, index) => [workspace.path, groups[index]]))
    const unassigned: ConversationSummary[] = []

    for (const conversation of conversations) {
      const path = conversation.workspacePath.trim()
      if (!path) {
        unassigned.push(conversation)
        continue
      }
      let group = byPath.get(path)
      if (!group) {
        const name = path.replace(/\\/g, '/').split('/').filter(Boolean).pop() || path
        group = { key: `workspace:${path}`, name, conversations: [] }
        byPath.set(path, group)
        groups.push(group)
      }
      group.conversations.push(conversation)
    }
    if (unassigned.length > 0) groups.push({ key: 'unassigned', name: '不在工作区', conversations: unassigned })
    return groups
  }, [conversations, workspaces])
  const navItems = [
    { key: 'todos' as const, label: '待办', icon: <CheckSquareOutlined />, count: 0 },
    { key: 'milestones' as const, label: '里程碑', icon: <FlagOutlined />, count: 0 },
    { key: 'reminders' as const, label: '提醒中心', icon: <BellOutlined />, count: reminderCount },
    { key: 'artifacts' as const, label: '产物', icon: <FileDoneOutlined />, count: 0 },
    { key: 'vocabulary' as const, label: '生词本', icon: <ReadOutlined />, count: 0 },
  ]

  return (
    <aside className="bm-chat-app-sidebar" aria-label="应用导航与会话">
      <Dropdown
        trigger={['click']}
        placement="bottomLeft"
        menu={{
          selectedKeys: [agentProfile],
          onClick: ({ key }) => void onChangeAgentProfile(key as AgentProfile),
          items: [
            { key: 'work', label: <span className="bm-chat-profile-option"><span><strong>Work</strong><small>待办、文档与日常工作</small></span>{agentProfile === 'work' && <CheckOutlined />}</span> },
            { key: 'coding', label: <span className="bm-chat-profile-option"><span><strong>Coding</strong><small>构建、调试与代码修改</small></span>{agentProfile === 'coding' && <CheckOutlined />}</span> },
          ],
        }}
      >
        <Button type="text" className="bm-chat-profile-trigger" disabled={sending || !profileReady}><strong>{agentProfile === 'coding' ? 'Coding' : 'Work'}</strong><DownOutlined /></Button>
      </Dropdown>
      <Button type="text" className="bm-chat-new-session" icon={<EditOutlined />} onClick={() => { onNewChat(); onNavigate('chat') }}>
        <span>新建对话</span>
      </Button>
      <div className={`bm-chat-sidebar-scroll ${scrolled ? 'is-scrolled' : ''}`} onScroll={(event) => setScrolled(event.currentTarget.scrollTop > 0)}>
        <nav className="bm-chat-app-nav" aria-label="功能菜单">
          {navItems.map((item) => (
            <Button key={item.key} type="text" className={item.key === activeView ? 'is-active' : undefined} icon={item.icon} onClick={() => onNavigate(item.key)}>
              <span>{item.label}</span>
              {!!item.count && <span className="bm-chat-nav-count">{item.count}</span>}
            </Button>
          ))}
        </nav>
        <div className="bm-chat-workspace-list">
          <div className="bm-chat-sidebar-section-label">工作空间</div>
          {workspaceGroups.map((workspace, index) => {
            const open = workspaceOpen[workspace.key] ?? true
            const conversationsID = `bm-chat-workspace-conversations-${index}`
            return <div key={workspace.key} className="bm-chat-workspace-group">
              <Button type="text" className="bm-chat-workspace-item" aria-expanded={open} aria-controls={conversationsID} onClick={() => setWorkspaceOpen((state) => ({ ...state, [workspace.key]: !open }))}>
                <RightOutlined className="bm-chat-workspace-chevron" />
                <FolderOpenOutlined />
                <span>{workspace.name}</span>
              </Button>
              {open && (
                <div className="bm-chat-history-list" id={conversationsID}>
                  {workspace.conversations.length === 0 ? <Text type="secondary" className="bm-chat-empty-history">暂无历史会话</Text> : workspace.conversations.map((conversation) => {
                    const active = conversation.id === currentConversationId
                    return (
                      <div key={conversation.id} className={`bm-chat-history-item ${active ? 'is-active' : ''}`}>
                        <Button type="text" className="bm-chat-history-open" aria-current={active ? 'page' : undefined} onClick={() => onOpenConversation(conversation.id)}>
                          <span className="bm-chat-history-item-label"><HistoryOutlined /><span>{conversation.title}</span></span>
                        </Button>
                        {active && <Tooltip title="删除当前会话"><Button type="text" className="bm-chat-history-delete" aria-label="删除当前会话" icon={<DeleteOutlined />} onClick={() => void onDeleteCurrent()} /></Tooltip>}
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          })}
        </div>
      </div>
      <div className="bm-chat-sidebar-footer">
        <ThemeSwitcher />
        <Button type="text" className={activeView === 'settings' ? 'is-active' : undefined} icon={<SettingOutlined />} onClick={() => onNavigate('settings')}><span>设置</span></Button>
      </div>
    </aside>
  )
}

function ThemeSwitcher() {
  const { themeId, setTheme } = useBMTheme()
  const panel = (
    <Flex vertical gap={4} className="bm-theme-panel">
      <Typography.Text type="secondary" className="bm-theme-panel-title">选择主题</Typography.Text>
      {BM_THEMES.map((theme) => (
        <Button key={theme.id} size="small" type="text" className={`bm-theme-option ${theme.id === themeId ? 'is-active' : ''}`} aria-pressed={theme.id === themeId} onClick={() => void setTheme(theme.id)}>
          <span className="bm-theme-swatch" style={{ backgroundColor: theme.vars['sidebar-bg'], borderColor: theme.vars.divider }}><span style={{ backgroundColor: theme.vars.signal }} /></span>
          <span className="bm-theme-option-label">{theme.name}</span>
          {theme.id === themeId && <CheckOutlined className="bm-theme-option-check" />}
        </Button>
      ))}
    </Flex>
  )
  return <Popover content={panel} placement="rightBottom" trigger="click"><Button type="text" icon={<BgColorsOutlined />}><span>主题</span></Button></Popover>
}
