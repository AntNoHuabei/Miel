import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import {
  App as AntApp,
  Button,
  Dropdown,
  Flex,
  Input,
  Popover,
  Select,
  Slider,
  Space,
  Tooltip,
  Typography,
} from 'antd'
import {
  ArrowRightOutlined,
  BulbOutlined,
  CameraOutlined,
  CheckOutlined,
  CheckCircleOutlined,
  CopyOutlined,
  DeleteOutlined,
  EditOutlined,
  FolderOpenOutlined,
  HistoryOutlined,
  LoadingOutlined,
  PlusOutlined,
  SendOutlined,
  ToolOutlined,
  WarningOutlined,
  CheckSquareOutlined,
  FlagOutlined,
  BellOutlined,
  SettingOutlined,
  BgColorsOutlined,
  RightOutlined,
} from '@ant-design/icons'
import { Clipboard as WailsClipboard } from '@wailsio/runtime'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { AgentService, SettingsService, useWailsEvent } from '../api'
import { BM_THEMES, useBMTheme } from '../theme/ThemeContext'
import type {
  AGUIMessageLite,
  AGUIMessagesSnapshotLite,
  CatalogProviderLite,
  ModelOptionLite,
  ProviderLite,
} from '../api'
import '../styles/chat-md.css'

const { Text } = Typography

interface AgentChunk {
  conversationId: number
  requestId?: string
  delta: string
}

type AgentPhase = 'idle' | 'waiting' | 'thinking' | 'tool' | 'responding' | 'done' | 'error'

interface AGUIEvent {
  type: string
  delta?: string
  content?: string
  message?: string
  toolCallId?: string
  toolCallName?: string
}

interface AGUIEnvelope {
  conversationId: number
  requestId?: string
  event: AGUIEvent
}

interface AgentStarted {
  conversationId: number
  requestId?: string
}

interface ToolCallUI {
  id: string
  name: string
  args: string
  result: string
  status: 'preparing' | 'running' | 'done'
}

const QUICK = [
  '帮我生成最近一周的周报',
  '列出我当前所有待办',
  '我的里程碑进度如何',
]

// 思考档位显示名(low/medium/high/max → 中文)
const LEVEL_LABEL: Record<string, string> = {
  low: '低',
  medium: '中',
  high: '高',
  max: '最高',
}

const TOOL_LABEL: Record<string, string> = {
  create_todo: '创建待办',
  list_todos: '读取待办',
  set_todo_status: '更新待办',
  delete_todo: '删除待办',
  todo_stats: '统计待办',
  list_events: '读取操作记录',
  reminder_upcoming: '读取近期提醒',
  reminder_settings: '读取提醒设置',
  generate_weekly_report: '生成周报',
  create_document: '创建文档',
  create_table: '创建表格',
  export_todos: '导出待办',
}

const CURRENT_CONVERSATION_STORAGE_KEY = 'blankmind.chat.currentConversation'
const CHAT_REASONING_STORAGE_KEY = 'chat.reasoning.v1'

function readPersistedConversationId() {
  if (typeof window === 'undefined') return 0
  const value = Number(window.localStorage.getItem(CURRENT_CONVERSATION_STORAGE_KEY))
  return Number.isSafeInteger(value) && value > 0 ? value : 0
}

// Codex 风格 Agent 对话页:
// 左侧历史会话 aside + 右侧消息流(Markdown)与 Composer。
// Composer 内集中:工作区、模型切换、思考(档位随模型能力)、发送。
type ChatViewProps = {
  activeView: 'chat' | 'todos' | 'milestones' | 'reminders' | 'settings'
  featureTitle: string
  featureContent?: ReactNode
  reminderCount: number
  onNavigate?: (view: 'chat' | 'todos' | 'milestones' | 'reminders' | 'settings') => void
  sidebarOpen: boolean
  newChatRequest: number
  openConversationRequest?: { id: number; seq: number }
}

export default function ChatView({
  activeView,
  featureTitle,
  featureContent,
  reminderCount,
  onNavigate,
  sidebarOpen,
  newChatRequest,
  openConversationRequest,
}: ChatViewProps) {
  const { message } = AntApp.useApp()
  const [convs, setConvs] = useState<{ id: number; title: string }[]>([])
  const [workspaceOpen, setWorkspaceOpen] = useState(true)
  const [sidebarScrolled, setSidebarScrolled] = useState(false)
  const [current, setCurrent] = useState(() => readPersistedConversationId()) // 0 = 新对话
  const [msgs, setMsgs] = useState<AGUIMessageLite[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [streaming, setStreaming] = useState('')
  const [reasoning, setReasoning] = useState('')
  const [reasoningPreferences, setReasoningPreferences] = useState<Record<string, string>>({})
  const [agentPhase, setAgentPhase] = useState<AgentPhase>('idle')
  const [reasoningTrace, setReasoningTrace] = useState('')
  const [toolCalls, setToolCalls] = useState<ToolCallUI[]>([])

  // 模型与工作区
  const [providers, setProviders] = useState<ProviderLite[]>([])
  const [modelOpts, setModelOpts] = useState<ModelOptionLite[]>([])
  const [catalog, setCatalog] = useState<CatalogProviderLite[]>([])
  const [dataDir, setDataDir] = useState('')

  const currentRef = useRef(current)
  const sendingRef = useRef(false)
  const targetRef = useRef(0)
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const reasoningPreferencesRef = useRef<Record<string, string>>({})
  const activeRequestIdRef = useRef('')

  const defaultP = providers.find((p) => p.isDefault) ?? providers[0]

  // 模型切换下拉:按服务商分组展示已启用模型
  const modelOptGroups = useMemo(() => {
    const map = new Map<string, { value: string; label: string }[]>()
    for (const o of modelOpts) {
      if (!map.has(o.providerName)) map.set(o.providerName, [])
      map.get(o.providerName)?.push({
        value: `${o.providerId}::${o.model}`,
        label: o.custom ? `${o.model} (自定义)` : o.label || o.model,
      })
    }
    return Array.from(map, ([label, options]) => ({ label, options }))
  }, [modelOpts])

  // 从内置目录解析当前模型的“思考规格”
  const catProv = catalog.find((c) => c.kind === defaultP?.kind)
  const spec = catProv?.models.find((m) => m.id === defaultP?.model)?.reasoning
  const specType = spec?.type ?? 'none'
  const isCustom = defaultP?.kind === 'custom'
  const canDisableReasoning =
    specType === 'toggle' || (specType === 'effort' && defaultP?.kind === 'deepseek')

  // 只有厂商明确支持逐请求开关时才展示“关闭”;其余模型从最低档开始。
  const thinkSteps = useMemo(() => {
    if (specType === 'effort') {
      const levels = (spec?.levels ?? []).filter((l) => LEVEL_LABEL[l])
      return canDisableReasoning ? ['', ...levels] : levels
    }
    if (specType === 'toggle') {
      return ['', 'on']
    }
    if (isCustom) {
      return ['', 'low', 'medium', 'high']
    }
    return []
  }, [spec, specType, isCustom, canDisableReasoning])
  // 仅“关闭”一档 = 不支持调节
  const thinkLocked = thinkSteps.length <= 1
  const effectiveReasoning = thinkSteps.includes(reasoning) ? reasoning : (thinkSteps[0] ?? '')
  const thinkIdx = Math.max(thinkSteps.indexOf(effectiveReasoning), 0)
  const thinkMarks = useMemo(() => {
    const m: Record<number, string> = {}
    thinkSteps.forEach((s, i) => {
      if (s === '') m[i] = isCustom ? '服务商默认' : '关闭'
      else if (s === 'on') m[i] = '开启'
      else m[i] = LEVEL_LABEL[s] ?? s
    })
    return m
  }, [thinkSteps, isCustom])
  const activeModel = modelOpts.find(
    (option) => option.providerId === defaultP?.id && option.model === defaultP?.model,
  )
  const activeModelLabel = activeModel
    ? activeModel.custom
      ? `${activeModel.model} (自定义)`
      : activeModel.label || activeModel.model
    : defaultP?.model || '未配置模型'
  const reasoningStatus = thinkLocked
    ? specType === 'always'
      ? '思考常开'
      : specType === 'none'
        ? '不支持思考'
        : '不可调节'
    : thinkMarks[thinkIdx] ?? '关闭'
  const reasoningPillLabel = thinkLocked
    ? specType === 'always'
      ? '常开'
      : ''
    : thinkMarks[thinkIdx] ?? '关闭'
  const showWorkspaceControl = current === 0 && msgs.length === 0
  const reasoningPreferenceKey = defaultP ? `${defaultP.id}::${defaultP.model}` : ''

  const reloadConvs = useCallback(async () => {
    try {
      const list = (await AgentService.ListConversations()) as unknown as {
        id: number
        title: string
      }[]
      setConvs(list ?? [])
      if (currentRef.current > 0 && !(list ?? []).some((item) => item.id === currentRef.current)) {
        currentRef.current = 0
        setCurrent(0)
        setMsgs([])
      }
    } catch {
      /* 忽略 */
    }
  }, [])

  const reloadProviders = useCallback(async () => {
    try {
      const list = ((await SettingsService.ListProviders()) ?? []) as unknown as ProviderLite[]
      setProviders(list)
    } catch {
      /* 忽略 */
    }
  }, [])

  // 服务商 × 启用模型 扁平列表(对话模型切换)
  const reloadModelOpts = useCallback(async () => {
    try {
      const list = ((await SettingsService.ModelOptions()) ?? []) as unknown as ModelOptionLite[]
      setModelOpts(list)
    } catch {
      /* 忽略 */
    }
  }, [])

  const loadMessages = useCallback(async (convId: number) => {
    if (!convId) {
      setMsgs([])
      return
    }
    try {
      const snapshot = (await AgentService.MessagesSnapshot(
        convId,
      )) as unknown as AGUIMessagesSnapshotLite
      setMsgs(snapshot?.messages ?? [])
    } catch {
      /* 忽略 */
    }
  }, [])

  useEffect(() => {
    void reloadConvs()
    void reloadProviders()
    void reloadModelOpts()
    // 加载内置模型目录(驱动模型/思考规格)
    SettingsService.ModelCatalog()
      .then((list) => setCatalog((list ?? []) as unknown as CatalogProviderLite[]))
      .catch(() => undefined)
  }, [reloadConvs, reloadProviders, reloadModelOpts])

  // 恢复上次打开的会话;消息内容由后端 AG-UI snapshot 提供。
  useEffect(() => {
    if (currentRef.current > 0) void loadMessages(currentRef.current)
  }, [loadMessages])

  // 当前会话是 UI 指针,消息本身仍由后端持久化。
  useEffect(() => {
    if (typeof window === 'undefined') return
    if (current > 0) {
      window.localStorage.setItem(CURRENT_CONVERSATION_STORAGE_KEY, String(current))
    } else {
      window.localStorage.removeItem(CURRENT_CONVERSATION_STORAGE_KEY)
    }
  }, [current])

  useEffect(() => {
    SettingsService.GetSetting(CHAT_REASONING_STORAGE_KEY)
      .then((raw) => {
        if (!raw) return
        const parsed: unknown = JSON.parse(raw)
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return
        const preferences = Object.fromEntries(
          Object.entries(parsed).filter(
            (entry): entry is [string, string] => typeof entry[1] === 'string',
          ),
        )
        const merged = { ...preferences, ...reasoningPreferencesRef.current }
        reasoningPreferencesRef.current = merged
        setReasoningPreferences(merged)
      })
      .catch(() => undefined)
  }, [])

  // 每个模型恢复自己上次的思考档位;已失效的档位回落到模型默认值。
  const modelKey = `${defaultP?.kind ?? ''}:${defaultP?.model ?? ''}`
  useEffect(() => {
    const saved = reasoningPreferenceKey
      ? reasoningPreferences[reasoningPreferenceKey]
      : undefined
    setReasoning(saved !== undefined && thinkSteps.includes(saved) ? saved : (thinkSteps[0] ?? ''))
  }, [modelKey, reasoningPreferenceKey, reasoningPreferences, thinkSteps])

  useEffect(() => {
    SettingsService.DataDir()
      .then((d) => setDataDir(d))
      .catch(() => undefined)
  }, [])

  const openConv = (id: number) => {
    currentRef.current = id
    targetRef.current = id
    setCurrent(id)
    setStreaming('')
    setAgentPhase('idle')
    setReasoningTrace('')
    setToolCalls([])
    void loadMessages(id)
    onNavigate?.('chat')
  }

  const newChat = () => {
    currentRef.current = 0
    targetRef.current = 0
    setCurrent(0)
    setMsgs([])
    setStreaming('')
    setAgentPhase('idle')
    setReasoningTrace('')
    setToolCalls([])
  }

  useEffect(() => {
    if (newChatRequest > 0) {
      newChat()
      onNavigate?.('chat')
    }
    // newChatRequest is an imperative request counter owned by the window title bar.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [newChatRequest, onNavigate])

  useEffect(() => {
    if (openConversationRequest?.id) openConv(openConversationRequest.id)
    // Request sequence intentionally makes reopening the same conversation imperative.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openConversationRequest?.seq])

  const send = async () => {
    const text = input.trim()
    if (!text || sending) return
    setInput('')
    setMsgs((prev) => [
      ...prev,
      { id: `pending-${Date.now()}`, role: 'user', content: text },
    ])
    setSending(true)
    setStreaming('')
    setAgentPhase('waiting')
    setReasoningTrace('')
    setToolCalls([])
    sendingRef.current = true
    targetRef.current = currentRef.current
    const requestId = typeof crypto !== 'undefined' && crypto.randomUUID
      ? crypto.randomUUID()
      : `chat-${Date.now()}-${Math.random().toString(16).slice(2)}`
    activeRequestIdRef.current = requestId
    try {
      const res = (await AgentService.Chat({
        conversationId: currentRef.current,
        message: text,
        reasoning: effectiveReasoning,
        requestId,
      })) as unknown as { conversationId: number; answer: string }
      const id = res.conversationId
      currentRef.current = id
      targetRef.current = id
      setCurrent(id)
      await loadMessages(id)
      setAgentPhase('idle')
      setReasoningTrace('')
      setToolCalls([])
      await reloadConvs()
    } catch (err) {
      setAgentPhase('error')
      message.error(`对话失败:${String(err)}`)
    } finally {
      sendingRef.current = false
      setSending(false)
      setStreaming('')
    }
  }

  // 流式增量:仅收当前发送会话的 chunk(新建会话以首个 chunk 的 id 为准)
  useWailsEvent<AgentChunk>(
    'agent.chunk',
    useCallback((p) => {
      if (!sendingRef.current) return
      if (p.requestId && p.requestId !== activeRequestIdRef.current) return
      if (targetRef.current === 0) {
        targetRef.current = p.conversationId
      } else if (p.conversationId !== targetRef.current) {
        return
      }
      setStreaming((prev) => prev + p.delta)
      // 自动滚到底
      requestAnimationFrame(() => {
        const el = scrollRef.current
        if (el) el.scrollTop = el.scrollHeight
      })
    }, []),
  )

  // 标准 AG-UI 事件:展示等待、推理与工具执行过程。
  useWailsEvent<AGUIEnvelope>(
    'agent.agui',
    useCallback((payload) => {
      if (!sendingRef.current || !payload?.event) return
      if (payload.requestId && payload.requestId !== activeRequestIdRef.current) return
      if (targetRef.current === 0) {
        targetRef.current = payload.conversationId
      } else if (payload.conversationId !== targetRef.current) {
        return
      }

      const event = payload.event
      switch (event.type) {
        case 'RUN_STARTED':
          setAgentPhase('waiting')
          break
        case 'REASONING_START':
        case 'REASONING_MESSAGE_START':
        case 'THINKING_START':
        case 'THINKING_TEXT_MESSAGE_START':
          setAgentPhase('thinking')
          break
        case 'REASONING_MESSAGE_CONTENT':
        case 'REASONING_MESSAGE_CHUNK':
        case 'THINKING_TEXT_MESSAGE_CONTENT':
          setAgentPhase('thinking')
          if (event.delta) setReasoningTrace((prev) => prev + event.delta)
          break
        case 'TOOL_CALL_START': {
          const id = event.toolCallId ?? `tool-${Date.now()}`
          setAgentPhase('tool')
          setToolCalls((prev) => {
            if (prev.some((tool) => tool.id === id)) return prev
            return [
              ...prev,
              {
                id,
                name: event.toolCallName ?? 'unknown_tool',
                args: '',
                result: '',
                status: 'preparing',
              },
            ]
          })
          break
        }
        case 'TOOL_CALL_ARGS': {
          const id = event.toolCallId
          if (!id) break
          setAgentPhase('tool')
          setToolCalls((prev) =>
            prev.map((tool) =>
              tool.id === id
                ? { ...tool, args: tool.args + (event.delta ?? ''), status: 'preparing' }
                : tool,
            ),
          )
          break
        }
        case 'TOOL_CALL_END': {
          const id = event.toolCallId
          if (!id) break
          setAgentPhase('tool')
          setToolCalls((prev) =>
            prev.map((tool) => (tool.id === id ? { ...tool, status: 'running' } : tool)),
          )
          break
        }
        case 'TOOL_CALL_RESULT': {
          const id = event.toolCallId
          if (!id) break
          setToolCalls((prev) =>
            prev.map((tool) =>
              tool.id === id
                ? { ...tool, result: event.content ?? '', status: 'done' }
                : tool,
            ),
          )
          break
        }
        case 'TEXT_MESSAGE_START':
        case 'TEXT_MESSAGE_CONTENT':
          setAgentPhase('responding')
          break
        case 'RUN_FINISHED':
          setAgentPhase('done')
          setToolCalls((prev) => prev.map((tool) => ({ ...tool, status: 'done' })))
          break
        case 'RUN_ERROR':
          setAgentPhase('error')
          break
      }
    }, []),
  )

  // agent.start 比模型的首个协议事件更早到达,也负责绑定新会话 ID。
  useWailsEvent<AgentStarted>(
    'agent.start',
    useCallback((payload) => {
      if (!sendingRef.current) return
      if (payload.requestId && payload.requestId !== activeRequestIdRef.current) return
      if (targetRef.current === 0) targetRef.current = payload.conversationId
      if (payload.conversationId === targetRef.current) setAgentPhase('waiting')
    }, []),
  )

  useWailsEvent<string>(
    'conversations.changed',
    useCallback(() => void reloadConvs(), [reloadConvs]),
  )

  useWailsEvent<string>(
    'models.changed',
    useCallback(() => {
      void reloadProviders()
      void reloadModelOpts()
    }, [reloadProviders, reloadModelOpts]),
  )

  useEffect(() => {
    requestAnimationFrame(() => {
      const el = scrollRef.current
      if (el) el.scrollTop = el.scrollHeight
    })
  }, [agentPhase, reasoningTrace, toolCalls])

  const delCurrent = async () => {
    if (!current) return
    try {
      await AgentService.DeleteConversation(current)
      await reloadConvs()
      newChat()
      message.success('已删除该会话')
    } catch (err) {
      message.error(`删除失败:${String(err)}`)
    }
  }

  const switchModel = async (pid: number, model: string) => {
    try {
      await SettingsService.SetProviderModel(pid, model)
      message.success(`已切换到 ${model}`)
      await reloadProviders()
      await reloadModelOpts()
    } catch (err) {
      message.error(`切换失败:${String(err)}`)
    }
  }

  const changeReasoning = (value: string) => {
    setReasoning(value)
    if (!reasoningPreferenceKey) return
    const preferences = {
      ...reasoningPreferencesRef.current,
      [reasoningPreferenceKey]: value,
    }
    reasoningPreferencesRef.current = preferences
    setReasoningPreferences(preferences)
    void SettingsService.SetSetting(
      CHAT_REASONING_STORAGE_KEY,
      JSON.stringify(preferences),
    ).catch((err) => message.error(`思考级别保存失败:${String(err)}`))
  }

  const showAgentProcess = sending || reasoningTrace.length > 0 || toolCalls.length > 0
  // 回答落库后 msgs 会追加 assistant 消息,过程块应留在该回答之前。
  const processBeforeIndex =
    showAgentProcess && msgs[msgs.length - 1]?.role === 'assistant' ? msgs.length - 1 : -1
  const snapshotToolNames = useMemo(() => {
    const names = new Map<string, string>()
    for (const item of msgs) {
      for (const call of item.toolCalls ?? []) names.set(call.id, call.function.name)
    }
    return names
  }, [msgs])

  const navItems = [
    { key: 'todos' as const, label: '待办', icon: <CheckSquareOutlined />, count: 0 },
    { key: 'milestones' as const, label: '里程碑', icon: <FlagOutlined />, count: 0 },
    { key: 'reminders' as const, label: '提醒中心', icon: <BellOutlined />, count: reminderCount },
  ]

  return (
    <div className={`bm-chat bm-chat-shell ${sidebarOpen ? 'is-sidebar-open' : 'is-sidebar-collapsed'}`}>
      <aside className="bm-chat-app-sidebar" aria-label="应用导航与会话">
        <Button
          type="text"
          className="bm-chat-new-session"
          icon={<EditOutlined />}
          onClick={() => {
            newChat()
            onNavigate?.('chat')
          }}
        >
          <span>新建对话</span>
        </Button>

        <div
          className={`bm-chat-sidebar-scroll ${sidebarScrolled ? 'is-scrolled' : ''}`}
          onScroll={(event) => setSidebarScrolled(event.currentTarget.scrollTop > 0)}
        >
          <nav className="bm-chat-app-nav" aria-label="功能菜单">
            {navItems.map((item) => (
              <Button
                key={item.key}
                type="text"
                className={item.key === activeView ? 'is-active' : undefined}
                icon={item.icon}
                onClick={() => onNavigate?.(item.key)}
              >
                <span>{item.label}</span>
                {!!item.count && <span className="bm-chat-nav-count">{item.count}</span>}
              </Button>
            ))}
          </nav>

          <div className="bm-chat-workspace-list">
            <div className="bm-chat-sidebar-section-label">工作空间</div>
            <Button
              type="text"
              className="bm-chat-workspace-item"
              aria-expanded={workspaceOpen}
              aria-controls="bm-chat-workspace-conversations"
              onClick={() => setWorkspaceOpen((open) => !open)}
            >
              <RightOutlined className="bm-chat-workspace-chevron" />
              <FolderOpenOutlined />
              <span>BlankMind</span>
            </Button>

            {workspaceOpen && (
              <div className="bm-chat-history-list" id="bm-chat-workspace-conversations">
                {convs.length === 0 ? (
                  <Text type="secondary" className="bm-chat-empty-history">暂无历史会话</Text>
                ) : (
                  convs.map((conversation) => {
                    const isActive = conversation.id === current
                    return (
                      <div
                        key={conversation.id}
                        className={`bm-chat-history-item ${isActive ? 'is-active' : ''}`}
                      >
                        <Button
                          type="text"
                          className="bm-chat-history-open"
                          aria-current={isActive ? 'page' : undefined}
                          onClick={() => openConv(conversation.id)}
                        >
                          <span className="bm-chat-history-item-label">
                            <HistoryOutlined />
                            <span>{conversation.title}</span>
                          </span>
                        </Button>
                        {isActive && (
                          <Tooltip title="删除当前会话">
                            <Button
                              type="text"
                              className="bm-chat-history-delete"
                              aria-label="删除当前会话"
                              icon={<DeleteOutlined />}
                              onClick={() => void delCurrent()}
                            />
                          </Tooltip>
                        )}
                      </div>
                    )
                  })
                )}
              </div>
            )}
          </div>
        </div>

        <div className="bm-chat-sidebar-footer">
          <ThemeSwitcher />
          <Button
            type="text"
            className={activeView === 'settings' ? 'is-active' : undefined}
            icon={<SettingOutlined />}
            onClick={() => onNavigate?.('settings')}
          >
            <span>设置</span>
          </Button>
        </div>
      </aside>

      <Flex vertical className="bm-chat-main-shell" style={{ minWidth: 0, minHeight: 0 }}>
        {activeView === 'chat' ? (
        <Flex vertical className="bm-chat-main" style={{ flex: 1, minWidth: 0, minHeight: 0 }}>
          {/* 消息流 */}
          <div
            className={`bm-chat-messages ${msgs.length === 0 && !streaming ? 'is-empty' : ''}`}
            ref={scrollRef}
            style={{ flex: 1, overflowY: 'auto', minWidth: 0, minHeight: 0 }}
          >
          <Flex vertical className="bm-chat-message-track" style={{ padding: '8px 24px 24px' }}>
            {msgs.length === 0 && !streaming && (
              <section className="bm-chat-empty-state" aria-labelledby="bm-chat-empty-title">
                <div className="bm-chat-empty-intro">
                  <Typography.Title level={1} id="bm-chat-empty-title" className="bm-chat-empty-title">
                    你好，我是 BlankMind
                  </Typography.Title>
                  <Text type="secondary" className="bm-chat-empty-copy">
                    可以让我记录待办、安排里程碑、生成周报，或创建文档与表格。
                  </Text>
                </div>
                <div className="bm-chat-quick-list">
                  {QUICK.map((prompt) => (
                    <Button
                      key={prompt}
                      type="text"
                      className="bm-chat-quick-item"
                      onClick={() => {
                        setInput(prompt)
                        void (document.querySelector('#bm-chat-input') as
                          | HTMLTextAreaElement
                          | null)?.focus()
                      }}
                    >
                      <span>{prompt}</span>
                      <ArrowRightOutlined className="bm-chat-quick-arrow" />
                    </Button>
                  ))}
                </div>
              </section>
            )}

            {msgs.map((m, i) => (
              <Fragment key={i}>
                {i === processBeforeIndex && (
                  <AgentProcess
                    phase={agentPhase}
                    reasoning={reasoningTrace}
                    tools={toolCalls}
                  />
                )}
                <SnapshotMessage
                  message={m}
                  toolName={m.toolCallId ? snapshotToolNames.get(m.toolCallId) : undefined}
                />
              </Fragment>
            ))}

            {showAgentProcess && processBeforeIndex < 0 && (
              <AgentProcess
                phase={agentPhase}
                reasoning={reasoningTrace}
                tools={toolCalls}
              />
            )}

            {streaming && (
              <div style={{ margin: '6px 0' }}>
                <Text type="secondary" style={{ fontSize: 11, fontWeight: 600 }}>
                  BlankMind
                </Text>
                <div className="bm-md">
                  {renderMarkdown(streaming)}
                  <span className="bm-cursor" />
                </div>
              </div>
            )}
          </Flex>
          </div>

          {/* Composer:输入 + 工具(新对话显示工作区，右侧切换模型与思考档位) */}
          <div
            className="bm-chat-composer"
            style={{
              padding: '6px 16px 14px',
              background: 'var(--bm-content-bg)',
            }}
          >
        <Flex justify="center" className="bm-chat-composer-track">
          <div
            className="bm-chat-composer-shell"
            style={{
              border: '1px solid var(--bm-border)',
              background: 'var(--bm-header-bg)',
            }}
          >
            <Input.TextArea
              id="bm-chat-input"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onPressEnter={(e) => {
                if (!e.shiftKey) {
                  e.preventDefault()
                  void send()
                }
              }}
              placeholder="给 BlankMind 发消息…(Enter 发送 / Shift+Enter 换行)"
              autoSize={{ minRows: 2, maxRows: 8 }}
              variant="borderless"
              style={{ padding: '12px 14px 4px', fontSize: 14, lineHeight: 1.6 }}
              disabled={sending}
            />
            <Flex
              className="bm-chat-composer-actions"
              justify="space-between"
              align="center"
              style={{ padding: '4px 6px 6px' }}
              gap={6}
            >
              <Space size={2} className="bm-chat-composer-actions-left">
                {/* ➕ 快捷菜单:位于输入区左下角 */}
                <Dropdown
                  menu={{
                    items: [
                      {
                        key: 'capture',
                        icon: <CameraOutlined />,
                        label: '截屏处理',
                        onClick: () => window.dispatchEvent(new Event('blankmind:capture')),
                      },
                    ],
                  }}
                  trigger={['click']}
                  placement="topLeft"
                >
                  <Button type="text" icon={<PlusOutlined />} />
                </Dropdown>

                {showWorkspaceControl && (
                  <Popover
                    trigger="click"
                    content={
                      <Flex vertical gap={6} style={{ maxWidth: 380 }}>
                        <Text type="secondary" style={{ fontSize: 12 }}>
                          工作区(应用数据目录)
                        </Text>
                        <Text style={{ fontSize: 12, wordBreak: 'break-all' }}>
                          {dataDir || '读取中…'}
                        </Text>
                        <Button
                          size="small"
                          icon={<FolderOpenOutlined />}
                          onClick={() => void SettingsService.OpenDataDir()}
                        >
                          打开目录
                        </Button>
                      </Flex>
                    }
                  >
                    <Tooltip title="工作区">
                      <Button type="text" icon={<FolderOpenOutlined />} />
                    </Tooltip>
                  </Popover>
                )}
              </Space>

              <Space size={6} className="bm-chat-composer-actions-right">
                <Popover
                  trigger="click"
                  placement="topRight"
                  rootClassName="bm-chat-model-popover"
                  content={
                    <div className="bm-chat-model-panel">
                      <label className="bm-chat-model-panel-label" htmlFor="bm-chat-model-select">
                        模型
                      </label>
                      <Select
                        id="bm-chat-model-select"
                        className="bm-chat-model-select"
                        value={defaultP ? `${defaultP.id}::${defaultP.model}` : undefined}
                        onChange={(value) => {
                          const [providerID, ...modelParts] = String(value).split('::')
                          void switchModel(Number(providerID), modelParts.join('::'))
                        }}
                        placeholder="未配置模型"
                        popupMatchSelectWidth={false}
                        options={modelOptGroups}
                        getPopupContainer={(trigger) => trigger.parentElement ?? document.body}
                      />

                      <div className="bm-chat-model-panel-divider" />

                      <Flex justify="space-between" align="center" gap={16}>
                        <span className="bm-chat-model-panel-label">思考级别</span>
                        <Text type="secondary" className="bm-chat-model-panel-value">
                          {reasoningStatus}
                        </Text>
                      </Flex>
                      <Slider
                        className="bm-chat-reasoning-slider"
                        min={0}
                        max={Math.max(thinkSteps.length - 1, 0)}
                        step={1}
                        value={thinkIdx}
                        disabled={thinkLocked}
                        onChange={(value) => changeReasoning(thinkSteps[value as number] ?? '')}
                        marks={thinkMarks}
                        tooltip={{ open: false }}
                      />
                      {spec?.note && (
                        <Text type="secondary" className="bm-chat-model-panel-note">
                          {spec.note}
                        </Text>
                      )}
                      {isCustom && !spec && (
                        <Text type="secondary" className="bm-chat-model-panel-note">
                          自定义模型会按 OpenAI 兼容协议透传 reasoning_effort。
                        </Text>
                      )}
                    </div>
                  }
                >
                  <Button type="text" className="bm-chat-model-trigger">
                    <span className="bm-chat-model-trigger-name">{activeModelLabel}</span>
                    {reasoningPillLabel && (
                      <span className="bm-chat-model-trigger-reasoning">
                        {reasoningPillLabel}
                      </span>
                    )}
                    <RightOutlined className="bm-chat-model-trigger-chevron" />
                  </Button>
                </Popover>

                <Tooltip title={sending ? '生成中…' : '发送'}>
                  <Button
                    type="primary"
                    shape="circle"
                    size="large"
                    icon={<SendOutlined />}
                    loading={sending}
                    disabled={!input.trim()}
                    onClick={() => void send()}
                  />
                </Tooltip>
              </Space>
            </Flex>
          </div>
        </Flex>
          </div>
        </Flex>
        ) : (
          <Flex vertical className="bm-feature-page">
            <header className="bm-feature-header">
              <Text strong>{featureTitle}</Text>
            </header>
            <div className="bm-feature-content">{featureContent}</div>
          </Flex>
        )}
      </Flex>
    </div>
  )
}

function ThemeSwitcher() {
  const { themeId, setTheme } = useBMTheme()
  const panel = (
    <Flex vertical gap={4} className="bm-theme-panel">
      <Typography.Text type="secondary" className="bm-theme-panel-title">
        选择主题
      </Typography.Text>
      {BM_THEMES.map((theme) => (
        <Button
          key={theme.id}
          size="small"
          type="text"
          className={`bm-theme-option ${theme.id === themeId ? 'is-active' : ''}`}
          aria-pressed={theme.id === themeId}
          onClick={() => void setTheme(theme.id)}
        >
          <span
            className="bm-theme-swatch"
            style={{
              backgroundColor: theme.vars['sidebar-bg'],
              borderColor: theme.vars.divider,
            }}
          >
            <span style={{ backgroundColor: theme.vars.signal }} />
          </span>
          <span className="bm-theme-option-label">{theme.name}</span>
          {theme.id === themeId && <CheckOutlined className="bm-theme-option-check" />}
        </Button>
      ))}
    </Flex>
  )
  return (
    <Popover content={panel} placement="rightBottom" trigger="click">
      <Button type="text" icon={<BgColorsOutlined />}>
        <span>主题</span>
      </Button>
    </Popover>
  )
}

// Markdown 渲染(Sanitize:react-markdown 默认不执行 HTML)。
function renderMarkdown(content: string) {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{
        a: (props) => <a {...props} target="_blank" rel="noreferrer" />,
      }}
    >
      {content}
    </ReactMarkdown>
  )
}

function snapshotContentText(content: unknown) {
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((part) => {
        if (typeof part === 'string') return part
        if (part && typeof part === 'object' && 'text' in part) {
          return String((part as { text?: unknown }).text ?? '')
        }
        return ''
      })
      .filter(Boolean)
      .join('\n')
  }
  if (content == null) return ''
  try {
    return JSON.stringify(content, null, 2)
  } catch {
    return String(content)
  }
}

export function SnapshotMessage({
  message,
  toolName,
}: {
  message: AGUIMessageLite
  toolName?: string
}) {
  const { message: messageApi } = AntApp.useApp()
  const content = snapshotContentText(message.content)

  const copyContent = async () => {
    try {
      await WailsClipboard.SetText(content)
      messageApi.success('已复制')
    } catch (error) {
      messageApi.error(`复制失败：${String(error)}`)
    }
  }

  if (message.role === 'user') {
    return (
      <Flex justify="flex-end" style={{ margin: '4px 0' }}>
        <div className="bm-chat-user-message">
          {content}
        </div>
      </Flex>
    )
  }

  if (message.role === 'reasoning') {
    return (
      <div className="bm-agent-process">
        <details className="bm-agent-detail">
          <summary>
            <BulbOutlined />
            <span>思考过程</span>
          </summary>
          <div className="bm-agent-reasoning">{content}</div>
        </details>
      </div>
    )
  }

  if (message.role === 'tool') {
    const label = toolName ? (TOOL_LABEL[toolName] ?? toolName) : '工具调用'
    return (
      <div className="bm-agent-process">
        <details className="bm-agent-detail">
          <summary>
            {message.error ? <WarningOutlined /> : <CheckCircleOutlined />}
            <span>{label}</span>
            <Text type="secondary" className="bm-tool-status">
              {message.error ? '执行失败' : '已完成'}
            </Text>
          </summary>
          <div className="bm-tool-detail">
            {message.toolCallId && <code>{message.toolCallId}</code>}
            {content && <pre>{content}</pre>}
            {message.error && <pre>{message.error}</pre>}
          </div>
        </details>
      </div>
    )
  }

  if (message.role === 'activity') {
    return (
      <div className="bm-agent-process">
        <details className="bm-agent-detail">
          <summary>
            <ToolOutlined />
            <span>{message.activityType || '活动'}</span>
          </summary>
          {content && <div className="bm-tool-detail"><pre>{content}</pre></div>}
        </details>
      </div>
    )
  }

  if (message.role !== 'assistant') return null

  return (
    <>
      {(message.toolCalls ?? []).map((call) => {
        const name = call.function.name
        return (
          <div className="bm-agent-process" key={call.id}>
            <details className="bm-agent-detail">
              <summary>
                <ToolOutlined />
                <span>{TOOL_LABEL[name] ?? name}</span>
                <Text type="secondary" className="bm-tool-status">已调用</Text>
              </summary>
              <div className="bm-tool-detail">
                <code>{name}</code>
                {call.function.arguments && <pre>{call.function.arguments}</pre>}
              </div>
            </details>
          </div>
        )
      })}
      {content && (
        <div className="bm-chat-assistant-message">
          <Text type="secondary" style={{ fontSize: 11, fontWeight: 600 }}>
            BlankMind
          </Text>
          <div className="bm-md">{renderMarkdown(content)}</div>
          <AssistantMessageFooter
            content={content}
            metrics={message.metrics}
            onCopy={copyContent}
          />
        </div>
      )}
    </>
  )
}

function formatDuration(milliseconds: number) {
  if (milliseconds < 1000) return `${Math.round(milliseconds)} ms`
  return `${(milliseconds / 1000).toFixed(milliseconds < 10_000 ? 1 : 0)} s`
}

function AssistantMessageFooter({
  content,
  metrics,
  onCopy,
}: {
  content: string
  metrics?: AGUIMessageLite['metrics']
  onCopy: () => Promise<void>
}) {
  const totalTokens = metrics
    ? metrics.totalTokens > 0
      ? metrics.totalTokens
      : metrics.promptTokens + metrics.completionTokens
    : 0
  const hasUsage = totalTokens > 0
  const usageDetails = metrics
    ? [
        metrics.model && `模型：${metrics.model}`,
        hasUsage && `输入：${metrics.promptTokens.toLocaleString()} tokens`,
        hasUsage && `输出：${metrics.completionTokens.toLocaleString()} tokens`,
        metrics.reasoningTokens > 0 && `推理：${metrics.reasoningTokens.toLocaleString()} tokens`,
        metrics.cachedTokens > 0 && `缓存命中：${metrics.cachedTokens.toLocaleString()} tokens`,
      ]
        .filter(Boolean)
        .join('\n')
    : ''

  return (
    <div className="bm-chat-response-meta">
      <Tooltip title="复制回复">
        <Button
          type="text"
          className="bm-chat-copy-button"
          aria-label="复制回复"
          icon={<CopyOutlined />}
          disabled={!content}
          onClick={() => void onCopy()}
        />
      </Tooltip>
      <div className="bm-chat-response-stats" aria-label="回复统计">
        {hasUsage && metrics && (
          <Tooltip title={<span className="bm-chat-response-tooltip">{usageDetails}</span>}>
            <span>{totalTokens.toLocaleString()} tokens</span>
          </Tooltip>
        )}
        {metrics && metrics.durationMs > 0 && (
          <Tooltip title="从发送请求到回复完成的总耗时">
            <span>{formatDuration(metrics.durationMs)}</span>
          </Tooltip>
        )}
        {hasUsage && metrics && metrics.tokensPerSecond > 0 && (
          <Tooltip title="输出 token / 扣除首 token 等待后的剩余总时长">
            <span>{metrics.tokensPerSecond.toFixed(1)} tok/s</span>
          </Tooltip>
        )}
        {metrics && metrics.firstTokenMs > 0 && (
          <Tooltip title="首个有效 token 延迟">
            <span>TTFT {formatDuration(metrics.firstTokenMs)}</span>
          </Tooltip>
        )}
        {metrics?.model && (
          <Tooltip title="本次回复使用的模型">
            <span className="bm-chat-response-model">{metrics.model}</span>
          </Tooltip>
        )}
      </div>
    </div>
  )
}

function AgentProcess({
  phase,
  reasoning,
  tools,
}: {
  phase: AgentPhase
  reasoning: string
  tools: ToolCallUI[]
}) {
  const active = phase !== 'done' && phase !== 'error' && phase !== 'idle'
  const label =
    phase === 'thinking'
      ? '正在思考'
      : phase === 'tool'
        ? '正在使用工具'
        : phase === 'responding'
          ? '正在组织回复'
          : phase === 'done'
            ? '处理完成'
            : phase === 'error'
              ? '处理失败'
              : '正在等待模型响应'

  return (
    <div className="bm-agent-process">
      <Flex align="center" gap={8} className="bm-agent-status">
        {active ? (
          <LoadingOutlined spin />
        ) : phase === 'error' ? (
          <WarningOutlined />
        ) : (
          <CheckCircleOutlined />
        )}
        <Text type="secondary">{label}</Text>
      </Flex>

      {reasoning && (
        <details className="bm-agent-detail" open={phase === 'thinking'}>
          <summary>
            <BulbOutlined />
            <span>思考过程</span>
          </summary>
          <div className="bm-agent-reasoning">{reasoning}</div>
        </details>
      )}

      {tools.map((tool) => (
        <details className="bm-agent-detail" key={tool.id}>
          <summary>
            {tool.status === 'done' ? <CheckCircleOutlined /> : <ToolOutlined />}
            <span>{TOOL_LABEL[tool.name] ?? tool.name}</span>
            <Text type="secondary" className="bm-tool-status">
              {tool.status === 'preparing'
                ? '准备中'
                : tool.status === 'running'
                  ? '执行中'
                  : '已完成'}
            </Text>
          </summary>
          <div className="bm-tool-detail">
            <code>{tool.name}</code>
            {tool.args && <pre>{tool.args}</pre>}
            {tool.result && <pre>{tool.result}</pre>}
          </div>
        </details>
      ))}
    </div>
  )
}
