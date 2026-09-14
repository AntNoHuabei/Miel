import { Fragment, useCallback, useEffect, useRef, useState } from 'react'
import {
  Button,
  Flex,
  Input,
  Spin,
  Tooltip,
  Typography,
} from 'antd'
import {
  CheckSquareOutlined,
  CloseOutlined,
  PictureOutlined,
  PlusOutlined,
  SendOutlined,
} from '@ant-design/icons'
import { Window as WailsWindow } from '@wailsio/runtime'
import { clipboardRepository, settingsRepository } from '../shared/repositories'
import { useWailsEvent } from '../shared/wails/events'
import { ChatRunErrorMessage, isPersistedTailError, SnapshotMessage } from '../features/chat/components/ConversationMessages'
import { quickConversationStore } from '../features/chat/model/conversationStore'
import { useConversationRuntime } from '../features/chat/controllers/useConversationRuntime'
import { ClipboardTodoPanel } from '../features/capture/components/ClipboardTodoPanel'
import { ChatAttachmentStrip, useChatAttachments } from '../components/ChatAttachments'
import '../styles/chat-md.css'

const REASONING_KEY = 'chat.reasoning.v1'

type Mode = 'chat' | 'clipboard'

export default function QuickAssistantWindow({
  configured,
  checking,
}: {
  configured: boolean
  checking: boolean
}) {
  const [mode, setMode] = useState<Mode>('chat')
  const [clipboardRequest, setClipboardRequest] = useState(0)
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
  const clipboardDraftRef = useRef('')

  const loadModel = useCallback(async () => {
    try {
      const providers = await settingsRepository.listProviders()
      const current = providers.find((provider) => provider.isDefault) ?? providers[0]
      setModelLabel(current?.model ?? '')
      setSupportsImages(await settingsRepository.defaultModelSupportsVision())
    } catch {
      setModelLabel('')
      setSupportsImages(false)
    }
  }, [])

  useEffect(() => { void loadModel() }, [loadModel])

  const updateClipboardDraft = useCallback((id: string) => {
    clipboardDraftRef.current = id
  }, [])

  const discardClipboard = useCallback(() => {
    if (clipboardDraftRef.current) void clipboardRepository.discardDraft(clipboardDraftRef.current)
    clipboardDraftRef.current = ''
  }, [])

  const getRequestContext = useCallback(async () => {
    let reasoning = ''
    try {
      const providers = await settingsRepository.listProviders()
      const current = providers.find((provider) => provider.isDefault) ?? providers[0]
      const raw = await settingsRepository.getSetting(REASONING_KEY)
      const preferences = raw ? JSON.parse(raw) as Record<string, string> : {}
      reasoning = current ? preferences[`${current.id}::${current.model}`] ?? '' : ''
    } catch { /* Use the provider default when preferences are unavailable. */ }
    return { reasoning, workspacePath: '', permissionSessionId: '' }
  }, [])

  const runtime = useConversationRuntime({
    store: quickConversationStore,
    attachments,
    supportsImages,
    enabled: configured,
    requestPrefix: 'quick',
    unsupportedImagesMessage: '当前模型不支持图片输入，请在主窗口切换模型',
    consumeAttachments,
    discardAttachments,
    getRequestContext,
  })
  const phase = runtime.run.phase === 'thinking'
    ? '正在思考'
    : runtime.run.phase === 'tool'
      ? '正在使用工具'
      : ['waiting', 'responding'].includes(runtime.run.phase)
        ? (runtime.run.phase === 'waiting' ? '正在等待模型响应' : '正在生成')
        : ''

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

  const newConversation = useCallback(() => {
    discardClipboard()
    setMode('chat')
    runtime.reset()
  }, [discardClipboard, runtime])

  const close = () => {
    newConversation()
    void WailsWindow.Hide()
  }

  const startClipboard = () => {
    if (runtime.sending) return
    discardClipboard()
    setMode('clipboard')
    setClipboardRequest((request) => request + 1)
  }

  return (
    <div className="bm-quick-window">
      <header className="bm-quick-titlebar">
        <div className="bm-quick-brand">BlankMind</div>
        <div className="bm-quick-drag" />
        <Tooltip title="新建会话"><Button type="text" icon={<PlusOutlined />} disabled={runtime.sending} onClick={newConversation} /></Tooltip>
        <Tooltip title="从粘贴板生成待办"><Button type="text" icon={<CheckSquareOutlined />} disabled={runtime.sending} onClick={startClipboard} /></Tooltip>
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
          <div className="bm-quick-messages" ref={runtime.scrollRef}>
            {runtime.messages.length === 0 && !runtime.run.streaming && !runtime.run.error ? (
              <div className="bm-quick-empty">
                <Typography.Title level={2}>有什么需要处理？</Typography.Title>
                <Typography.Text type="secondary">对话会保存到 BlankMind 的会话列表。</Typography.Text>
              </div>
            ) : (
              runtime.messages.map((item, index) => <Fragment key={item.id || index}><SnapshotMessage message={item} /></Fragment>)
            )}
            {phase && <div className="bm-quick-phase"><Spin size="small" /><span>{phase}</span></div>}
            {runtime.run.streaming && <div className="bm-chat-assistant-message"><Typography.Text type="secondary">BlankMind</Typography.Text><div className="bm-md">{runtime.run.streaming}</div></div>}
            {runtime.run.error && !isPersistedTailError(runtime.messages, runtime.run.error) && <ChatRunErrorMessage error={runtime.run.error} />}
          </div>
          <div className="bm-quick-composer">
            <ChatAttachmentStrip attachments={attachments} onRemove={removeAttachment} />
            {attachments.length > 0 && !supportsImages && (
              <div className="bm-chat-attachment-warning">当前模型不支持图片输入</div>
            )}
            <Input.TextArea
              value={runtime.input}
              onChange={(event) => runtime.setInput(event.target.value)}
              onPaste={onPaste}
              onPressEnter={(event) => {
                if (!event.shiftKey) {
                  event.preventDefault()
                  void runtime.send()
                }
              }}
              autoSize={{ minRows: 2, maxRows: 6 }}
              variant="borderless"
              placeholder="给 BlankMind 发消息"
              disabled={runtime.sending}
            />
            <Flex align="center" justify="space-between" className="bm-quick-composer-footer">
              <Flex align="center" gap={4} className="bm-quick-composer-left">
                <Tooltip title="添加图片">
                  <Button type="text" icon={<PictureOutlined />} disabled={runtime.sending} onClick={() => void pickImages()} />
                </Tooltip>
                <Typography.Text type="secondary" ellipsis>{modelLabel || '当前模型'}</Typography.Text>
              </Flex>
              <Tooltip title={attachments.length > 0 && !supportsImages ? '当前模型不支持图片输入' : '发送'}>
                <Button
                  type="primary"
                  shape="circle"
                  icon={<SendOutlined />}
                  loading={runtime.sending}
                  disabled={(!runtime.input.trim() && attachments.length === 0) || (attachments.length > 0 && !supportsImages)}
                  onClick={() => void runtime.send()}
                />
              </Tooltip>
            </Flex>
          </div>
        </>
      )}
    </div>
  )
}
