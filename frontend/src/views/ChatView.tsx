import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
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
  CheckCircleOutlined,
  DeleteOutlined,
  FolderOpenOutlined,
  HistoryOutlined,
  LoadingOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  PlusOutlined,
  SendOutlined,
  ToolOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { AgentService, SettingsService, useWailsEvent } from '../api'
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
  event: AGUIEvent
}

interface AgentStarted {
  conversationId: number
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

function readPersistedConversationId() {
  if (typeof window === 'undefined') return 0
  const value = Number(window.localStorage.getItem(CURRENT_CONVERSATION_STORAGE_KEY))
  return Number.isSafeInteger(value) && value > 0 ? value : 0
}

// Codex 风格 Agent 对话页:
// 左侧历史会话 aside + 右侧消息流(Markdown)与 Composer。
// Composer 内集中:工作区、模型切换、思考(档位随模型能力)、发送。
export default function ChatView() {
  const { message } = AntApp.useApp()
  const [convs, setConvs] = useState<{ id: number; title: string }[]>([])
  const [current, setCurrent] = useState(() => readPersistedConversationId()) // 0 = 新对话
  const [msgs, setMsgs] = useState<AGUIMessageLite[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [streaming, setStreaming] = useState('')
  const [reasoning, setReasoning] = useState('')
  const [agentPhase, setAgentPhase] = useState<AgentPhase>('idle')
  const [reasoningTrace, setReasoningTrace] = useState('')
  const [toolCalls, setToolCalls] = useState<ToolCallUI[]>([])
  const [historyOpen, setHistoryOpen] = useState(false)

  // 模型与工作区
  const [providers, setProviders] = useState<ProviderLite[]>([])
  const [modelOpts, setModelOpts] = useState<ModelOptionLite[]>([])
  const [catalog, setCatalog] = useState<CatalogProviderLite[]>([])
  const [dataDir, setDataDir] = useState('')

  const currentRef = useRef(current)
  const sendingRef = useRef(false)
  const targetRef = useRef(0)
  const scrollRef = useRef<HTMLDivElement | null>(null)

  const defaultP = providers.find((p) => p.isDefault) ?? providers[0]

  // 模型切换下拉:按服务商分组展示已启用模型
  const modelOptGroups = useMemo(() => {
    const map = new Map<string, { value: string; label: string }[]>()
    for (const o of modelOpts) {
      if (!map.has(o.providerName)) map.set(o.providerName, [])
      map.get(o.providerName)?.push({
        value: `${o.providerId}::${o.model}`,
        label: `${o.isDefault ? '✓ ' : ''}${o.custom ? o.model + '(自定义)' : o.label || o.model}`,
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

  // 模型切换后重置思考档位(不同模型档位集合不同)
  const modelKey = `${defaultP?.kind ?? ''}:${defaultP?.model ?? ''}`
  useEffect(() => {
    setReasoning(thinkSteps[0] ?? '')
  }, [modelKey, thinkSteps])

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
    try {
      const res = (await AgentService.Chat({
        conversationId: currentRef.current,
        message: text,
        reasoning: effectiveReasoning,
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
      if (targetRef.current === 0) targetRef.current = payload.conversationId
      if (payload.conversationId === targetRef.current) setAgentPhase('waiting')
    }, []),
  )

  useWailsEvent<string>(
    'conversations.changed',
    useCallback(() => void reloadConvs(), [reloadConvs]),
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

  const currentTitle = convs.find((c) => c.id === current)?.title
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

  return (
    <Flex vertical className="bm-chat" style={{ height: '100%' }}>
      <Flex className="bm-chat-workspace" style={{ flex: 1, minHeight: 0 }}>
        <aside
          className={`bm-chat-history-sidebar ${historyOpen ? 'is-open' : 'is-collapsed'}`}
          aria-label="历史会话"
        >
          {historyOpen ? (
            <>
            <Flex align="center" justify="space-between" className="bm-chat-history-heading">
              <Flex align="center" gap={8} style={{ minWidth: 0 }}>
                <HistoryOutlined />
                <Text strong ellipsis style={{ fontSize: 13 }}>
                  历史会话
                </Text>
              </Flex>
              <Space size={0}>
                {current > 0 && (
                  <Tooltip title="删除当前会话">
                    <Button
                      type="text"
                      size="small"
                      aria-label="删除当前会话"
                      icon={<DeleteOutlined />}
                      onClick={() => void delCurrent()}
                    />
                  </Tooltip>
                )}
                <Tooltip title="收起历史会话">
                <Button
                  type="text"
                  size="small"
                  aria-label="收起历史会话"
                  icon={<MenuFoldOutlined />}
                  onClick={() => setHistoryOpen(false)}
                />
                </Tooltip>
              </Space>
            </Flex>
            <Button
              block
              type="default"
              icon={<PlusOutlined />}
              style={{ marginBottom: 10, justifyContent: 'flex-start' }}
              onClick={newChat}
            >
              新对话
            </Button>
            <div className="bm-chat-history-list">
              {convs.length === 0 ? (
                <Text type="secondary" style={{ display: 'block', padding: '8px 10px', fontSize: 12 }}>
                  暂无历史会话
                </Text>
              ) : (
                convs.map((conversation) => (
                  <Button
                    key={conversation.id}
                    type="text"
                    block
                    className={conversation.id === current ? 'is-active' : undefined}
                    style={{ justifyContent: 'flex-start', textAlign: 'left', marginBottom: 2 }}
                    onClick={() => openConv(conversation.id)}
                  >
                    <span className="bm-chat-history-item-label">
                      <HistoryOutlined />
                      {conversation.title}
                    </span>
                  </Button>
                ))
              )}
            </div>
            </>
          ) : (
            <>
              <Tooltip title="新对话" placement="right">
                <Button
                  type="text"
                  size="small"
                  aria-label="新对话"
                  icon={<PlusOutlined />}
                  onClick={newChat}
                />
              </Tooltip>
              <Tooltip title="展开历史会话" placement="right">
                <Button
                  type="text"
                  size="small"
                  aria-label="展开历史会话"
                  icon={<MenuUnfoldOutlined />}
                  onClick={() => setHistoryOpen(true)}
                />
              </Tooltip>
              <span
                className={`bm-chat-history-collapsed-marker ${current > 0 ? 'has-current' : ''}`}
                aria-label={currentTitle ? `当前会话:${currentTitle}` : '暂无当前会话'}
              />
            </>
          )}
        </aside>

        <Flex vertical className="bm-chat-main" style={{ flex: 1, minWidth: 0, minHeight: 0 }}>
          {/* 消息流 */}
          <div
            className={`bm-chat-messages ${msgs.length === 0 && !streaming ? 'is-empty' : ''}`}
            ref={scrollRef}
            style={{ flex: 1, overflowY: 'auto', minWidth: 0, minHeight: 0 }}
          >
          <Flex vertical style={{ maxWidth: 800, margin: '0 auto', padding: '8px 24px 24px' }}>
            {msgs.length === 0 && !streaming && (
              <Flex vertical align="flex-start" gap={8} className="bm-chat-empty-state">
                <span className="bm-chat-empty-kicker">BLANKMIND / READY</span>
                <Text strong className="bm-chat-empty-title">
                  从一个问题开始
                </Text>
                <Text type="secondary" className="bm-chat-empty-copy">
                  记录待办、安排里程碑、生成周报,或创建文档与表格。
                </Text>
                <Flex vertical className="bm-chat-quick-list">
                  {QUICK.map((q) => (
                    <Button
                      key={q}
                      type="text"
                      className="bm-chat-quick-item"
                      onClick={() => {
                        setInput(q)
                        void (document.querySelector('#bm-chat-input') as
                          | HTMLTextAreaElement
                          | null)?.focus()
                      }}
                    >
                      <span className="bm-chat-quick-index">0{QUICK.indexOf(q) + 1}</span>
                      <span>{q}</span>
                      <ArrowRightOutlined className="bm-chat-quick-arrow" />
                    </Button>
                  ))}
                </Flex>
              </Flex>
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

          {/* Composer:输入 + 工具(工作区/模型/思考/发送) */}
          <div
            className="bm-chat-composer"
            style={{
              padding: '6px 16px 14px',
              background: 'var(--bm-content-bg)',
            }}
          >
        <Flex justify="center">
          <div
            className="bm-chat-composer-shell"
            style={{
              width: 800,
              maxWidth: '100%',
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
              justify="space-between"
              align="center"
              style={{ padding: '4px 6px 6px' }}
              wrap
              gap={6}
            >
              <Space size={2} wrap>
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

                {/* 工作区 */}
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

                {/* 模型切换:列出各服务商已启用的模型 */}
                <Select
                  size="middle"
                  variant="borderless"
                  style={{ minWidth: 210, maxWidth: 300 }}
                  value={
                    defaultP ? `${defaultP.id}::${defaultP.model}` : undefined
                  }
                  onChange={(v) => {
                    const [pid, ...rest] = String(v).split('::')
                    void switchModel(Number(pid), rest.join('::'))
                  }}
                  placeholder="未配置模型"
                  popupMatchSelectWidth={false}
                  options={modelOptGroups}
                />

                {/* 思考:点击弹出档位滑块 */}
                <Popover
                  trigger="click"
                  placement="top"
                  content={
                    <Flex vertical gap={6} style={{ width: 260, padding: '2px 4px' }}>
                      <Flex justify="space-between" align="center">
                        <Typography.Text strong style={{ fontSize: 13 }}>
                          思考档位
                        </Typography.Text>
                        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                          {thinkLocked
                            ? specType === 'always'
                              ? '该模型思考常开'
                              : specType === 'none'
                                ? '该模型不支持思考'
                                : '无可用档位'
                            : thinkMarks[thinkIdx] ?? '关闭'}
                        </Typography.Text>
                      </Flex>
                      <Slider
                        min={0}
                        max={Math.max(thinkSteps.length - 1, 0)}
                        step={1}
                        value={thinkIdx}
                        disabled={thinkLocked}
                        onChange={(v) => setReasoning(thinkSteps[v as number] ?? '')}
                        marks={thinkMarks}
                        tooltip={{ open: false }}
                      />
                      {spec?.note && (
                        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                          {spec.note}
                        </Typography.Text>
                      )}
                      {isCustom && !spec && (
                        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                          自定义模型:按 OpenAI 兼容 reasoning_effort 尽力透传
                        </Typography.Text>
                      )}
                    </Flex>
                  }
                >
                  <Tooltip title="思考档位" placement="top">
                    <Button
                      type="text"
                      size="middle"
                      style={{
                        width: 88,
                        justifyContent: 'flex-start',
                        paddingInline: 8,
                        color: 'inherit',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                      icon={
                        <BulbOutlined
                          style={{
                            color: effectiveReasoning && !thinkLocked ? '#faad14' : '#999',
                          }}
                        />
                      }
                    >
                      {thinkLocked
                        ? specType === 'always'
                          ? '思考(常开)'
                          : specType === 'none'
                            ? '不支持思考'
                            : '思考'
                        : (thinkMarks[thinkIdx] ?? '关闭')}
                    </Button>
                  </Tooltip>
                </Popover>
              </Space>

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
            </Flex>
          </div>
        </Flex>
          </div>
        </Flex>
      </Flex>
    </Flex>
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

function SnapshotMessage({
  message,
  toolName,
}: {
  message: AGUIMessageLite
  toolName?: string
}) {
  const content = snapshotContentText(message.content)

  if (message.role === 'user') {
    return (
      <Flex justify="flex-end" style={{ margin: '4px 0' }}>
        <div
          className="bm-chat-user-message"
          style={{
            maxWidth: '78%',
            background: 'var(--bm-user-bg, #1677ff)',
            color: '#fff',
            padding: '8px 14px',
            borderRadius: 14,
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
            fontSize: 14,
          }}
        >
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
        <div style={{ margin: '6px 0' }}>
          <Text type="secondary" style={{ fontSize: 11, fontWeight: 600 }}>
            BlankMind
          </Text>
          <div className="bm-md">{renderMarkdown(content)}</div>
        </div>
      )}
    </>
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
