import { Fragment, useCallback, useEffect, useRef, useState } from 'react'
import {
  App as AntApp,
  Button,
  Checkbox,
  DatePicker,
  Empty,
  Flex,
  Input,
  Spin,
  Tooltip,
  Typography,
} from 'antd'
import {
  CheckSquareOutlined,
  CloseOutlined,
  DeleteOutlined,
  FileImageOutlined,
  FileTextOutlined,
  PictureOutlined,
  PlusOutlined,
  SendOutlined,
} from '@ant-design/icons'
import dayjs from 'dayjs'
import { Window as WailsWindow } from '@wailsio/runtime'
import {
  AgentService,
  ClipboardService,
  SettingsService,
  useWailsEvent,
} from '../api'
import type {
  AGUIMessageLite,
  AGUIMessagesSnapshotLite,
  ClipboardTodoDraftLite,
  ExtractedTodoLite,
  ProviderLite,
} from '../api'
import { SnapshotMessage } from './ChatView'
import { ChatAttachmentStrip, useChatAttachments } from '../components/ChatAttachments'
import '../styles/chat-md.css'

const QUICK_CONVERSATION_KEY = 'blankmind.quick.currentConversation'
const REASONING_KEY = 'chat.reasoning.v1'

type Mode = 'chat' | 'clipboard'

interface AgentEnvelope {
  conversationId: number
  requestId?: string
  delta?: string
  event?: { type?: string }
}

interface AgentInputSaved {
  conversationId: number
  messageId: number
  requestId?: string
}

function initialConversation() {
  const value = Number(window.localStorage.getItem(QUICK_CONVERSATION_KEY))
  return Number.isSafeInteger(value) && value > 0 ? value : 0
}

export default function QuickAssistantWindow({
  configured,
  checking,
}: {
  configured: boolean
  checking: boolean
}) {
  const { message } = AntApp.useApp()
  const [mode, setMode] = useState<Mode>('chat')
  const [clipboardRequest, setClipboardRequest] = useState(0)
  const [conversationID, setConversationID] = useState(initialConversation)
  const [messages, setMessages] = useState<AGUIMessageLite[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [streaming, setStreaming] = useState('')
  const [phase, setPhase] = useState('')
  const [modelLabel, setModelLabel] = useState('')
  const [supportsImages, setSupportsImages] = useState(false)
  const {
    attachments,
    pickImages,
    onPaste,
    removeAttachment,
    discardAttachments,
    consumeAttachments,
  } = useChatAttachments()
  const activeRequest = useRef('')
  const sessionVersion = useRef(0)
  const sendingRef = useRef(false)
  const clipboardDraftRef = useRef('')
  const inputSavedRef = useRef(false)
  const inputSavedConversationRef = useRef(0)
  const scrollRef = useRef<HTMLDivElement | null>(null)

  const loadMessages = useCallback(async (id: number) => {
    if (!id) {
      setMessages([])
      return
    }
    try {
      const snapshot = await AgentService.MessagesSnapshot(id) as unknown as AGUIMessagesSnapshotLite
      setMessages(snapshot?.messages ?? [])
    } catch {
      setConversationID(0)
      setMessages([])
    }
  }, [])

  const loadModel = useCallback(async () => {
    try {
      const providers = await SettingsService.ListProviders() as unknown as ProviderLite[]
      const current = providers.find((provider) => provider.isDefault) ?? providers[0]
      setModelLabel(current?.model ?? '')
      setSupportsImages(await SettingsService.DefaultModelSupportsVision())
    } catch {
      setModelLabel('')
      setSupportsImages(false)
    }
  }, [])

  useEffect(() => {
    void loadMessages(conversationID)
    void loadModel()
    // Restore once; subsequent conversation changes are already reflected locally.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadMessages, loadModel])

  useEffect(() => {
    if (conversationID > 0) window.localStorage.setItem(QUICK_CONVERSATION_KEY, String(conversationID))
    else window.localStorage.removeItem(QUICK_CONVERSATION_KEY)
  }, [conversationID])

  useEffect(() => {
    requestAnimationFrame(() => {
      if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    })
  }, [messages, streaming, phase])

  const updateClipboardDraft = useCallback((id: string) => {
    clipboardDraftRef.current = id
  }, [])

  const discardClipboard = useCallback(() => {
    if (clipboardDraftRef.current) void ClipboardService.DiscardDraft(clipboardDraftRef.current)
    clipboardDraftRef.current = ''
  }, [])

  useWailsEvent<string>('quickchat.show', useCallback(() => {
    discardClipboard()
    setMode('chat')
  }, [discardClipboard]))
  useWailsEvent<string>(
    'clipboard.todo.show',
    useCallback(() => {
      discardClipboard()
      setMode('clipboard')
      setClipboardRequest((request) => request + 1)
    }, [discardClipboard]),
  )
  useWailsEvent<string>('models.changed', useCallback(() => void loadModel(), [loadModel]))

  useWailsEvent<AgentEnvelope>(
    'agent.chunk',
    useCallback((payload) => {
      if (!sendingRef.current || payload.requestId !== activeRequest.current) return
      setStreaming((content) => content + (payload.delta ?? ''))
      setPhase('正在生成')
    }, []),
  )
  useWailsEvent<AgentInputSaved>(
    'agent.input.saved',
    useCallback((payload) => {
      if (!sendingRef.current || payload.requestId !== activeRequest.current) return
      inputSavedRef.current = true
      inputSavedConversationRef.current = payload.conversationId
      setConversationID(payload.conversationId)
      consumeAttachments()
    }, [consumeAttachments]),
  )
  useWailsEvent<AgentEnvelope>(
    'agent.agui',
    useCallback((payload) => {
      if (!sendingRef.current || payload.requestId !== activeRequest.current) return
      const type = payload.event?.type ?? ''
      if (type.includes('REASONING') || type.includes('THINKING')) setPhase('正在思考')
      else if (type.includes('TOOL_CALL')) setPhase('正在使用工具')
      else if (type.includes('TEXT_MESSAGE')) setPhase('正在生成')
    }, []),
  )

  const newConversation = useCallback(() => {
    discardClipboard()
    discardAttachments()
    setMode('chat')
    sessionVersion.current += 1
    activeRequest.current = ''
    setConversationID(0)
    setMessages([])
    setInput('')
    setStreaming('')
    setPhase('')
    sendingRef.current = false
    setSending(false)
  }, [discardAttachments, discardClipboard])

  const close = () => {
    newConversation()
    void WailsWindow.Hide()
  }

  const startClipboard = () => {
    if (sending) return
    discardClipboard()
    setMode('clipboard')
    setClipboardRequest((request) => request + 1)
  }

  const send = async () => {
    const text = input.trim()
    if ((!text && attachments.length === 0) || sending || !configured) return
    if (attachments.length > 0 && !supportsImages) {
      message.error('当前模型不支持图片输入，请在主窗口切换模型')
      return
    }
    const sentAttachments = attachments
    const version = sessionVersion.current
    const requestId = typeof crypto !== 'undefined' && crypto.randomUUID
      ? crypto.randomUUID()
      : `quick-${Date.now()}-${Math.random().toString(16).slice(2)}`
    activeRequest.current = requestId
    inputSavedRef.current = false
    inputSavedConversationRef.current = 0
    setInput('')
    const pendingID = `pending-${Date.now()}`
    setMessages((items) => [...items, {
      id: pendingID,
      role: 'user',
      content: text,
      attachments: sentAttachments,
    }])
    setStreaming('')
    setPhase('正在等待模型响应')
    sendingRef.current = true
    setSending(true)
    try {
      let reasoning = ''
      try {
        const providers = await SettingsService.ListProviders() as unknown as ProviderLite[]
        const current = providers.find((provider) => provider.isDefault) ?? providers[0]
        const raw = await SettingsService.GetSetting(REASONING_KEY)
        const preferences = raw ? JSON.parse(raw) as Record<string, string> : {}
        reasoning = current ? preferences[`${current.id}::${current.model}`] ?? '' : ''
      } catch {
        reasoning = ''
      }
      const result = await AgentService.Chat({
        conversationId: conversationID,
        message: text,
        reasoning,
        requestId,
        attachmentIds: sentAttachments.map((item) => item.id),
      }) as unknown as { conversationId: number }
      if (sessionVersion.current !== version) return
      consumeAttachments()
      setConversationID(result.conversationId)
      await loadMessages(result.conversationId)
    } catch (error) {
      if (sessionVersion.current === version) {
        message.error(`对话失败：${String(error)}`)
        if (!inputSavedRef.current) {
          setInput(text)
          setMessages((items) => items.filter((item) => item.id !== pendingID))
        } else if (inputSavedConversationRef.current > 0) {
          await loadMessages(inputSavedConversationRef.current)
        }
      }
    } finally {
      if (sessionVersion.current === version) {
        sendingRef.current = false
        setSending(false)
        setStreaming('')
        setPhase('')
      }
    }
  }

  return (
    <div className="bm-quick-window">
      <header className="bm-quick-titlebar">
        <div className="bm-quick-brand">BlankMind</div>
        <div className="bm-quick-drag" />
        <Tooltip title="新建会话"><Button type="text" icon={<PlusOutlined />} disabled={sending} onClick={newConversation} /></Tooltip>
        <Tooltip title="从粘贴板生成待办"><Button type="text" icon={<CheckSquareOutlined />} disabled={sending} onClick={startClipboard} /></Tooltip>
        <Tooltip title="关闭"><Button type="text" icon={<CloseOutlined />} onClick={close} /></Tooltip>
      </header>

      {checking ? (
        <Flex flex={1} align="center" justify="center"><Spin /></Flex>
      ) : !configured ? (
        <Flex vertical flex={1} align="center" justify="center" gap={8}>
          <Typography.Text strong>请先在主窗口配置模型</Typography.Text>
          <Typography.Text type="secondary">配置完成后即可使用浮动助手。</Typography.Text>
        </Flex>
      ) : mode === 'clipboard' ? (
        <ClipboardTodoPanel
          request={clipboardRequest}
          onDraftChange={updateClipboardDraft}
          onCancel={() => {
            discardClipboard()
            setMode('chat')
          }}
          onCreated={() => {
            updateClipboardDraft('')
            setMode('chat')
          }}
        />
      ) : (
        <>
          <div className="bm-quick-messages" ref={scrollRef}>
            {messages.length === 0 && !streaming ? (
              <div className="bm-quick-empty">
                <Typography.Title level={2}>有什么需要处理？</Typography.Title>
                <Typography.Text type="secondary">对话会保存到 BlankMind 的会话列表。</Typography.Text>
              </div>
            ) : (
              messages.map((item, index) => <Fragment key={item.id || index}><SnapshotMessage message={item} /></Fragment>)
            )}
            {phase && <div className="bm-quick-phase"><Spin size="small" /><span>{phase}</span></div>}
            {streaming && <div className="bm-chat-assistant-message"><Typography.Text type="secondary">BlankMind</Typography.Text><div className="bm-md">{streaming}</div></div>}
          </div>
          <div className="bm-quick-composer">
            <ChatAttachmentStrip attachments={attachments} onRemove={removeAttachment} />
            {attachments.length > 0 && !supportsImages && (
              <div className="bm-chat-attachment-warning">当前模型不支持图片输入</div>
            )}
            <Input.TextArea
              value={input}
              onChange={(event) => setInput(event.target.value)}
              onPaste={onPaste}
              onPressEnter={(event) => {
                if (!event.shiftKey) {
                  event.preventDefault()
                  void send()
                }
              }}
              autoSize={{ minRows: 2, maxRows: 6 }}
              variant="borderless"
              placeholder="给 BlankMind 发消息"
              disabled={sending}
            />
            <Flex align="center" justify="space-between" className="bm-quick-composer-footer">
              <Flex align="center" gap={4} className="bm-quick-composer-left">
                <Tooltip title="添加图片">
                  <Button type="text" icon={<PictureOutlined />} disabled={sending} onClick={() => void pickImages()} />
                </Tooltip>
                <Typography.Text type="secondary" ellipsis>{modelLabel || '当前模型'}</Typography.Text>
              </Flex>
              <Tooltip title={attachments.length > 0 && !supportsImages ? '当前模型不支持图片输入' : '发送'}>
                <Button
                  type="primary"
                  shape="circle"
                  icon={<SendOutlined />}
                  loading={sending}
                  disabled={(!input.trim() && attachments.length === 0) || (attachments.length > 0 && !supportsImages)}
                  onClick={() => void send()}
                />
              </Tooltip>
            </Flex>
          </div>
        </>
      )}
    </div>
  )
}

function ClipboardTodoPanel({
  request,
  onDraftChange,
  onCancel,
  onCreated,
}: {
  request: number
  onDraftChange: (id: string) => void
  onCancel: () => void
  onCreated: () => void
}) {
  const { message } = AntApp.useApp()
  const [draft, setDraft] = useState<ClipboardTodoDraftLite | null>(null)
  const [items, setItems] = useState<ExtractedTodoLite[]>([])
  const [selected, setSelected] = useState<number[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const handledRequest = useRef(0)

  useEffect(() => {
    if (!request || handledRequest.current === request) return
    handledRequest.current = request
    setLoading(true)
    setError('')
    setDraft(null)
    void ClipboardService.ExtractTodos()
      .then((result) => {
        const next = result as unknown as ClipboardTodoDraftLite
        setDraft(next)
        setItems(next.items ?? [])
        setSelected((next.items ?? []).map((_, index) => index))
        onDraftChange(next.draftId)
      })
      .catch((reason) => setError(String(reason)))
      .finally(() => setLoading(false))
  }, [request, onDraftChange])

  const update = (index: number, patch: Partial<ExtractedTodoLite>) => {
    setItems((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, ...patch } : item))
  }

  const confirm = async () => {
    if (!draft) return
    const chosen = items.filter((_, index) => selected.includes(index))
    setSaving(true)
    try {
      const created = await ClipboardService.ConfirmTodos({ draftId: draft.draftId, items: chosen })
      message.success(`已创建 ${created?.length ?? 0} 条待办`)
      onCreated()
    } catch (reason) {
      message.error(`保存失败：${String(reason)}`)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Flex vertical className="bm-clipboard-panel">
      <header className="bm-clipboard-heading">
        <div>
          <Typography.Text strong>从粘贴板生成待办</Typography.Text>
          <Typography.Text type="secondary">确认前可以修改提取结果。</Typography.Text>
        </div>
        <Button type="text" onClick={onCancel}>返回对话</Button>
      </header>
      {loading ? (
        <Flex flex={1} align="center" justify="center" vertical gap={8}><Spin /><Typography.Text type="secondary">正在提取待办</Typography.Text></Flex>
      ) : error ? (
        <Flex flex={1} align="center" justify="center" vertical gap={10} className="bm-clipboard-error"><Typography.Text type="danger">{error}</Typography.Text><Button onClick={onCancel}>返回对话</Button></Flex>
      ) : draft ? (
        <>
          <div className="bm-clipboard-source-preview">
            {draft.kind === 'clipboard_image' ? <><FileImageOutlined /><img src={draft.dataUri} alt="粘贴板来源" /></> : <><FileTextOutlined /><div>{draft.text}</div></>}
          </div>
          <div className="bm-clipboard-items">
            {items.length === 0 ? <Empty description="没有识别到待办" /> : items.map((item, index) => (
              <div className="bm-clipboard-item" key={index}>
                <Checkbox checked={selected.includes(index)} onChange={(event) => setSelected((current) => event.target.checked ? [...current, index] : current.filter((value) => value !== index))} />
                <div className="bm-clipboard-item-fields">
                  <Input value={item.title} placeholder="标题" onChange={(event) => update(index, { title: event.target.value })} />
                  <Input.TextArea value={item.description} placeholder="描述（可选）" autoSize={{ minRows: 1, maxRows: 3 }} onChange={(event) => update(index, { description: event.target.value })} />
                  <Flex gap={10} align="center" wrap>
                    <DatePicker showTime value={item.dueDate ? dayjs(item.dueDate) : null} placeholder="截止时间" onChange={(value) => update(index, { dueDate: value ? value.format('YYYY-MM-DD HH:mm') : '' })} />
                    <Checkbox checked={item.milestone} onChange={(event) => update(index, { milestone: event.target.checked })}>里程碑</Checkbox>
                  </Flex>
                </div>
                <Tooltip title="不保存此项"><Button type="text" aria-label="不保存此项" icon={<DeleteOutlined />} onClick={() => setSelected((current) => current.filter((value) => value !== index))} /></Tooltip>
              </div>
            ))}
          </div>
          <footer className="bm-clipboard-footer">
            <Typography.Text type="secondary">已选择 {selected.length} 项</Typography.Text>
            <Button type="primary" loading={saving} disabled={selected.length === 0} onClick={() => void confirm()}>创建待办</Button>
          </footer>
        </>
      ) : null}
    </Flex>
  )
}
