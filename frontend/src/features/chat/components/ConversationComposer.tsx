import { useMemo, useState } from 'react'
import type { ClipboardEvent as ReactClipboardEvent } from 'react'
import { Button, Dropdown, Flex, Input, Popover, Select, Slider, Space, Tooltip, Typography } from 'antd'
import {
  CameraOutlined,
  BulbOutlined,
  CheckOutlined,
  CloseOutlined,
  CloseCircleFilled,
  FolderAddOutlined,
  FolderOpenOutlined,
  PictureOutlined,
  PlusOutlined,
  RightOutlined,
  SearchOutlined,
  SendOutlined,
} from '@ant-design/icons'
import type { ChatAttachmentDraftLite, WorkspaceLite } from '../../../api'
import type { PermissionMode } from '../../../components/permissions'
import { ChatAttachmentStrip } from '../../../components/ChatAttachments'
import { PermissionModeSelector } from '../../../components/permissions'

const { Text } = Typography

interface ModelOptionGroup {
  label: string
  options: Array<{ value: string; label: string }>
}

interface ConversationComposerProps {
  input: string
  mode: 'chat' | 'plan'
  sending: boolean
  attachments: ChatAttachmentDraftLite[]
  supportsImages: boolean
  permissionMode: PermissionMode
  showWorkspaceControl: boolean
  workspaces: WorkspaceLite[]
  currentWorkspace?: WorkspaceLite
  selectedModel?: string
  modelOptions: ModelOptionGroup[]
  activeModelLabel: string
  modelAvailable: boolean
  profileReady: boolean
  reasoningPillLabel: string
  reasoningStatus: string
  reasoningSteps: string[]
  reasoningIndex: number
  reasoningMarks: Record<number, string>
  reasoningLocked: boolean
  showCompatibleModelNote: boolean
  onAddWorkspace: () => void | Promise<void>
  onChangeInput: (value: string) => void
  onChangeMode: (mode: 'chat' | 'plan') => void
  onChangePermissionMode: (mode: PermissionMode) => void | Promise<void>
  onChangeReasoning: (value: string) => void
  onChooseWorkspace: (path: string) => void | Promise<void>
  onPaste: (event: ReactClipboardEvent<HTMLTextAreaElement>) => void
  onPickImages: () => void | Promise<void>
  onRemoveAttachment: (id: string) => void
  onRemoveWorkspace: (workspace: WorkspaceLite) => void | Promise<void>
  onSend: () => void | Promise<void>
  onStop: () => void | Promise<void>
  onSwitchModel: (providerId: number, model: string) => void | Promise<void>
}

export function ConversationComposer(props: ConversationComposerProps) {
  const [workspacePickerOpen, setWorkspacePickerOpen] = useState(false)
  const [workspaceQuery, setWorkspaceQuery] = useState('')
  const filteredWorkspaces = useMemo(() => {
    const query = workspaceQuery.trim().toLowerCase()
    if (!query) return props.workspaces
    return props.workspaces.filter((workspace) => workspace.name.toLowerCase().includes(query) || workspace.path.toLowerCase().includes(query))
  }, [props.workspaces, workspaceQuery])
  const sendDisabled = !props.modelAvailable || (!props.input.trim() && props.attachments.length === 0) || (props.attachments.length > 0 && !props.supportsImages)

  const chooseWorkspace = async (path: string) => {
    await props.onChooseWorkspace(path)
    setWorkspacePickerOpen(false)
    setWorkspaceQuery('')
  }

  return (
    <div className="bm-chat-composer" style={{ padding: '6px 16px 14px', background: 'var(--bm-content-bg)' }}>
      <Flex justify="center" className="bm-chat-composer-track">
        <div className="bm-chat-composer-shell" style={{ border: '1px solid var(--bm-border)', background: 'var(--bm-header-bg)' }}>
          <ChatAttachmentStrip attachments={props.attachments} onRemove={props.onRemoveAttachment} />
          {props.attachments.length > 0 && !props.supportsImages && <div className="bm-chat-attachment-warning">当前模型不支持图片输入</div>}
          <Input.TextArea
            id="bm-chat-input"
            value={props.input}
            onChange={(event) => props.onChangeInput(event.target.value)}
            onPaste={props.onPaste}
            onPressEnter={(event) => {
              if (!event.shiftKey) {
                event.preventDefault()
                void props.onSend()
              }
            }}
            placeholder="给 Miel 发消息…(Enter 发送 / Shift+Enter 换行)"
            autoSize={{ minRows: 2, maxRows: 8 }}
            variant="borderless"
            style={{ padding: '12px 14px 4px', fontSize: 14, lineHeight: 1.6 }}
            disabled={props.sending}
          />
          <Flex className="bm-chat-composer-actions" justify="space-between" align="center" wrap="wrap" style={{ padding: '4px 6px 6px' }} gap={6}>
            <Space size={2} wrap className="bm-chat-composer-actions-left">
              <Dropdown menu={{ items: [
                { key: 'plan', icon: <BulbOutlined />, label: 'Plan', disabled: props.sending || !props.profileReady || props.mode === 'plan', onClick: () => props.onChangeMode('plan') },
                { key: 'image', icon: <PictureOutlined />, label: '添加图片', disabled: props.sending, onClick: () => void props.onPickImages() },
                { key: 'capture', icon: <CameraOutlined />, label: '截屏处理', onClick: () => window.dispatchEvent(new Event('blankmind:capture')) },
              ] }} trigger={['click']} placement="topLeft">
                <Button type="text" icon={<PlusOutlined />} aria-label="添加内容" />
              </Dropdown>
              <PermissionModeSelector mode={props.permissionMode} onChange={props.onChangePermissionMode} disabled={props.sending} />
              {props.mode === 'plan' && <Tooltip title="退出 Plan，回到普通对话"><Button type="text" className="bm-chat-active-mode" aria-label="退出 Plan 模式" onClick={() => props.onChangeMode('chat')} disabled={props.sending}><span className="bm-chat-active-mode-icon"><BulbOutlined className="is-idle" /><CloseCircleFilled className="is-close" /></span><span>Plan</span></Button></Tooltip>}
              {props.showWorkspaceControl && (
                <Popover
                  trigger="click"
                  open={workspacePickerOpen}
                  onOpenChange={(open) => { setWorkspacePickerOpen(open); if (!open) setWorkspaceQuery('') }}
                  placement="topLeft"
                  rootClassName="bm-chat-workspace-popover"
                  content={<div className="bm-chat-workspace-panel">
                    <Input allowClear autoFocus value={workspaceQuery} onChange={(event) => setWorkspaceQuery(event.target.value)} placeholder="搜索工作区" prefix={<SearchOutlined />} />
                    <div className="bm-chat-workspace-options" role="listbox" aria-label="工作区列表">
                      {filteredWorkspaces.map((workspace) => (
                        <div className={`bm-chat-workspace-option ${workspace.isCurrent ? 'is-current' : ''}`} key={workspace.path} role="option" aria-selected={workspace.isCurrent} tabIndex={0} title={workspace.path} onClick={() => void chooseWorkspace(workspace.path)} onKeyDown={(event) => {
                          if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); void chooseWorkspace(workspace.path) }
                        }}>
                          <FolderOpenOutlined className="bm-chat-workspace-option-icon" />
                          <span className="bm-chat-workspace-option-copy"><span className="bm-chat-workspace-option-name">{workspace.name}</span><span className="bm-chat-workspace-option-path">{workspace.path}</span></span>
                          {workspace.isCurrent && <CheckOutlined className="bm-chat-workspace-option-check" />}
                          <Tooltip title="移除工作区"><Button type="text" className="bm-chat-workspace-option-remove" aria-label={`移除工作区 ${workspace.name}`} icon={<CloseOutlined />} onClick={(event) => { event.stopPropagation(); void props.onRemoveWorkspace(workspace) }} /></Tooltip>
                        </div>
                      ))}
                      {filteredWorkspaces.length === 0 && <Text type="secondary" className="bm-chat-workspace-empty">未找到工作区</Text>}
                    </div>
                    <div className="bm-chat-workspace-divider" />
                    <Button type="text" className="bm-chat-workspace-action" icon={<FolderAddOutlined />} onClick={() => void props.onAddWorkspace()}>添加工作区</Button>
                    <Button type="text" className="bm-chat-workspace-action" icon={<CloseOutlined />} disabled={!props.currentWorkspace} onClick={() => void chooseWorkspace('')}>不在工作区中</Button>
                  </div>}
                >
                  <Tooltip title="工作区"><Button type="text" className="bm-chat-workspace-trigger" icon={<FolderOpenOutlined />} title={props.currentWorkspace?.path ?? '不在工作区'}><span>{props.currentWorkspace?.name ?? '不在工作区'}</span></Button></Tooltip>
                </Popover>
              )}
            </Space>
            <Space size={6} className="bm-chat-composer-actions-right">
              <Popover trigger="click" placement="topRight" rootClassName="bm-chat-model-popover" content={<div className="bm-chat-model-panel">
                <label className="bm-chat-model-panel-label" htmlFor="bm-chat-model-select">模型</label>
                <Select id="bm-chat-model-select" className="bm-chat-model-select" value={props.selectedModel} disabled={props.sending || !props.profileReady} onChange={(value) => {
                  const [providerId, ...modelParts] = String(value).split('::')
                  void props.onSwitchModel(Number(providerId), modelParts.join('::'))
                }} placeholder="未配置模型" popupMatchSelectWidth={false} options={props.modelOptions} getPopupContainer={(trigger) => trigger.parentElement ?? document.body} />
                <div className="bm-chat-model-panel-divider" />
                <Flex justify="space-between" align="center" gap={16}><span className="bm-chat-model-panel-label">思考级别</span><Text type="secondary" className="bm-chat-model-panel-value">{props.reasoningStatus}</Text></Flex>
                <Slider className="bm-chat-reasoning-slider" min={0} max={Math.max(props.reasoningSteps.length - 1, 0)} step={1} value={props.reasoningIndex} disabled={props.reasoningLocked} onChange={(value) => props.onChangeReasoning(props.reasoningSteps[value as number] ?? '')} marks={props.reasoningMarks} tooltip={{ open: false }} />
                {props.showCompatibleModelNote && <Text type="secondary" className="bm-chat-model-panel-note">OpenAI 兼容模型会透传 reasoning_effort。</Text>}
              </div>}>
                <Button type="text" className="bm-chat-model-trigger" disabled={props.sending || !props.profileReady}><span className="bm-chat-model-trigger-name">{props.activeModelLabel}</span>{props.reasoningPillLabel && <span className="bm-chat-model-trigger-reasoning">{props.reasoningPillLabel}</span>}<RightOutlined className="bm-chat-model-trigger-chevron" /></Button>
              </Popover>
              <Tooltip title={props.sending ? '停止生成' : !props.modelAvailable ? '请为当前模式选择模型' : props.attachments.length > 0 && !props.supportsImages ? '当前模型不支持图片输入' : '发送'}>
                <Button
                  type="primary"
                  className={props.sending ? 'bm-stop-btn' : undefined}
                  shape="circle"
                  size="large"
                  icon={props.sending ? <span className="bm-stop-btn-glyph" aria-hidden="true" /> : <SendOutlined />}
                  disabled={props.sending ? false : sendDisabled}
                  aria-label={props.sending ? '停止生成' : '发送'}
                  onClick={() => void (props.sending ? props.onStop() : props.onSend())}
                />
              </Tooltip>
            </Space>
          </Flex>
        </div>
      </Flex>
    </div>
  )
}
