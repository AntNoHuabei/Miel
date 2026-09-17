import { App as AntApp, Button, Flex, Input, Popconfirm, Space, Tooltip, Typography } from 'antd'
import {
  BulbOutlined,
  CheckCircleOutlined,
  CopyOutlined,
  DeleteOutlined,
  EditOutlined,
  LoadingOutlined,
  PlayCircleOutlined,
  ToolOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useState } from 'react'
import type { AGUIMessageLite } from '../../../api'
import type { PlanLite } from '../../../shared/types/chat'
import type { AgentPhase, AgentProcessStep, AgentToolCall } from '../model/agentRun'
import type { AgentRunError } from '../model/chatError'
import { friendlyChatError, normalizeAgentRunError } from '../model/chatError'
import { ChatAttachmentStrip } from '../../../components/ChatAttachments'
import { getPermissionToolLabel } from '../../../components/permissions'
import { systemClipboardRepository } from '../../../shared/repositories'
import { ArtifactItems } from '../../artifacts/components/ArtifactItems'
import { SaveArtifactButton } from '../../artifacts/components/SaveArtifactDialog'

const { Text } = Typography

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

export function getToolLabel(name: string) {
  return TOOL_LABEL[name] ?? getPermissionToolLabel(name)
}

export function messageContentText(content: unknown) {
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content.map((part) => {
      if (typeof part === 'string') return part
      if (part && typeof part === 'object' && 'text' in part) return String((part as { text?: unknown }).text ?? '')
      return ''
    }).filter(Boolean).join('\n')
  }
  if (content == null) return ''
  try {
    return JSON.stringify(content, null, 2)
  } catch {
    return String(content)
  }
}

export function renderMarkdown(content: string) {
  return <ReactMarkdown remarkPlugins={[remarkGfm]} components={{ a: (props) => <a {...props} target="_blank" rel="noreferrer" /> }}>{content}</ReactMarkdown>
}

export function ChatRunErrorMessage({ error }: { error: AgentRunError }) {
  const friendly = friendlyChatError(error)
  return (
    <section className="bm-chat-run-error" role="alert">
      <div className="bm-chat-run-error-heading"><WarningOutlined /><strong>{friendly.title}</strong></div>
      <div className="bm-chat-run-error-description">{friendly.description}</div>
      <details className="bm-chat-run-error-detail">
        <summary>查看技术详情</summary>
        <pre>{error.message}</pre>
      </details>
    </section>
  )
}

export function isPersistedTailError(messages: AGUIMessageLite[], error: AgentRunError) {
  const tail = messages[messages.length - 1]
  return tail?.role === 'error' && tail.runError?.code === error.code && tail.runError.message === error.message
}

function formatDuration(milliseconds: number) {
  if (milliseconds < 1000) return `${Math.round(milliseconds)} ms`
  return `${(milliseconds / 1000).toFixed(milliseconds < 10_000 ? 1 : 0)} s`
}

function AssistantMessageFooter({ message, content, conversationId }: { message: AGUIMessageLite; content: string; conversationId: number }) {
  const { message: messageApi } = AntApp.useApp()
  const metrics = message.metrics
  const totalTokens = metrics ? (metrics.totalTokens || metrics.promptTokens + metrics.completionTokens) : 0
  const copy = async () => {
    try {
      await systemClipboardRepository.setText(content)
      messageApi.success('已复制')
    } catch (error) {
      messageApi.error(`复制失败：${String(error)}`)
    }
  }
  return (
    <div className="bm-chat-response-meta">
      <Tooltip title="复制回复">
        <Button type="text" className="bm-chat-copy-button" aria-label="复制回复" icon={<CopyOutlined />} disabled={!content} onClick={() => void copy()} />
      </Tooltip>
      {conversationId > 0 && message.id && <SaveArtifactButton conversationId={conversationId} messageId={message.id} suggestedName={`reply-${message.id}.md`} />}
      <div className="bm-chat-response-stats" aria-label="回复统计">
        {totalTokens > 0 && <span>{totalTokens.toLocaleString()} tokens</span>}
        {!!metrics?.durationMs && <span>{formatDuration(metrics.durationMs)}</span>}
        {!!metrics?.tokensPerSecond && <span>{metrics.tokensPerSecond.toFixed(1)} tok/s</span>}
        {!!metrics?.firstTokenMs && <span>TTFT {formatDuration(metrics.firstTokenMs)}</span>}
        {metrics?.model && <span className="bm-chat-response-model">{metrics.model}</span>}
      </div>
    </div>
  )
}

const codeExtension: Record<string, string> = { typescript: 'ts', tsx: 'tsx', javascript: 'js', jsx: 'jsx', python: 'py', go: 'go', json: 'json', css: 'css', html: 'html', markup: 'html', shell: 'sh', powershell: 'ps1' }

function AssistantMarkdown({ content, conversationId, messageId }: { content: string; conversationId: number; messageId: string }) {
  let codeBlock = 0
  return <ReactMarkdown remarkPlugins={[remarkGfm]} components={{
    a: (props) => <a {...props} target="_blank" rel="noreferrer" />,
    pre: ({ children }) => {
      const index = codeBlock++
      const element = Array.isArray(children) ? children[0] : children
      const className = element && typeof element === 'object' && 'props' in element ? String((element.props as { className?: string }).className ?? '') : ''
      const language = className.replace(/^language-/, '')
      const extension = codeExtension[language] ?? (language || 'txt')
      return <div className="bm-chat-code-block"><div className="bm-chat-code-actions"><span>{language || '代码'}</span><SaveArtifactButton conversationId={conversationId} messageId={messageId} codeBlock={index} suggestedName={`snippet-${index + 1}.${extension}`} /></div><pre>{children}</pre></div>
    },
  }}>{content}</ReactMarkdown>
}

const PLAN_STATUS: Record<string, string> = {
  pending: '待确认',
  executing: '执行中',
  completed: '已完成',
  failed: '执行失败',
  interrupted: '已中断',
  abandoned: '已废弃',
  revised: '已修订',
}

function PlanMessage({
  content,
  plan,
  sending,
  onExecute,
  onRevise,
  onAbandon,
}: {
  content: string
  plan: PlanLite
  sending: boolean
  onExecute?: (plan: PlanLite) => void | Promise<void>
  onRevise?: (plan: PlanLite, instruction: string) => void | Promise<void>
  onAbandon?: (plan: PlanLite) => void | Promise<void>
}) {
  const [revising, setRevising] = useState(false)
  const [instruction, setInstruction] = useState('')
  const actionable = ['pending', 'failed', 'interrupted'].includes(plan.status) && plan.revision === plan.currentRevision
  const submitRevision = async () => {
    const value = instruction.trim()
    if (!value || !onRevise) return
    await onRevise(plan, value)
    setInstruction('')
    setRevising(false)
  }
  return (
    <section className={`bm-plan-message is-${plan.status}`} aria-label={`计划 v${plan.revision}`}>
      <div className="bm-plan-message-header">
        <div><strong>Plan v{plan.revision}</strong><span className="bm-plan-status">{PLAN_STATUS[plan.status] ?? plan.status}</span></div>
        <Text type="secondary">{plan.generatedModel}{plan.executionModel ? ` · 执行 ${plan.executionModel}` : ''}</Text>
      </div>
      <div className="bm-md">{renderMarkdown(content)}</div>
      {revising && actionable && (
        <div className="bm-plan-revise">
          <Input.TextArea value={instruction} autoSize={{ minRows: 2, maxRows: 5 }} autoFocus placeholder="说明要调整的目标、范围或约束" onChange={(event) => setInstruction(event.target.value)} />
          <Space>
            <Button size="small" onClick={() => { setRevising(false); setInstruction('') }}>取消</Button>
            <Button size="small" type="primary" disabled={!instruction.trim()} loading={sending} onClick={() => void submitRevision()}>生成新版本</Button>
          </Space>
        </div>
      )}
      {actionable && !revising && (
        <div className="bm-plan-actions">
          <Button type="primary" icon={<PlayCircleOutlined />} disabled={sending || !onExecute} onClick={() => void onExecute?.(plan)}>{plan.status === 'pending' ? '确认执行' : '重新执行'}</Button>
          <Button icon={<EditOutlined />} disabled={sending || !onRevise} onClick={() => setRevising(true)}>Revise</Button>
          <Popconfirm title="废弃这个计划？" description="历史版本仍会保留在会话中。" okText="废弃" cancelText="取消" onConfirm={() => onAbandon?.(plan)}>
            <Button danger icon={<DeleteOutlined />} disabled={sending || !onAbandon}>废弃</Button>
          </Popconfirm>
        </div>
      )}
    </section>
  )
}

export function SnapshotMessage({ message, toolName, conversationId = 0, sending = false, onExecutePlan, onRevisePlan, onAbandonPlan }: { message: AGUIMessageLite; toolName?: string; conversationId?: number; sending?: boolean; onExecutePlan?: (plan: PlanLite) => void | Promise<void>; onRevisePlan?: (plan: PlanLite, instruction: string) => void | Promise<void>; onAbandonPlan?: (plan: PlanLite) => void | Promise<void> }) {
  const content = messageContentText(message.content)
  if (message.role === 'error') {
    return <ChatRunErrorMessage error={normalizeAgentRunError(message.runError ?? message.error ?? content)} />
  }
  if (message.role === 'user' && message.messageType === 'plan_execution') {
    return <div className="bm-plan-execution-event"><PlayCircleOutlined /><span>{content}</span></div>
  }
  if (message.role === 'user') {
    return <Flex justify="flex-end" style={{ margin: '4px 0' }}><div className="bm-chat-user-message"><ChatAttachmentStrip attachments={message.attachments ?? []} />{content && <div className="bm-chat-user-message-text">{content}</div>}</div></Flex>
  }
  if (message.role === 'reasoning') {
    return <div className="bm-agent-process"><details className="bm-agent-detail"><summary><BulbOutlined /><span>思考过程</span></summary><div className="bm-agent-reasoning">{content}</div></details></div>
  }
  if (message.role === 'tool') {
    return (
      <div className="bm-agent-process"><details className="bm-agent-detail"><summary>{message.error ? <WarningOutlined /> : <CheckCircleOutlined />}<span>{toolName ? getToolLabel(toolName) : '工具调用'}</span><Text type="secondary" className="bm-tool-status">{message.error ? '执行失败' : '已完成'}</Text></summary><div className="bm-tool-detail">{message.toolCallId && <code>{message.toolCallId}</code>}{content && <pre>{content}</pre>}{message.error && <pre>{message.error}</pre>}</div></details></div>
    )
  }
  if (message.role === 'activity') {
    return <div className="bm-agent-process"><details className="bm-agent-detail"><summary><ToolOutlined /><span>{message.activityType || '活动'}</span></summary>{content && <div className="bm-tool-detail"><pre>{content}</pre></div>}</details></div>
  }
  if (message.role === 'artifact') return <ArtifactItems artifacts={message.artifacts} />
  if (message.role !== 'assistant') return null
  if (message.plan) return <PlanMessage content={content} plan={message.plan} sending={sending} onExecute={onExecutePlan} onRevise={onRevisePlan} onAbandon={onAbandonPlan} />
  return (
    <>
      {(message.toolCalls ?? []).map((call) => <div className="bm-agent-process" key={call.id}><details className="bm-agent-detail"><summary><ToolOutlined /><span>{getToolLabel(call.function.name)}</span><Text type="secondary" className="bm-tool-status">已调用</Text></summary><div className="bm-tool-detail"><code>{call.function.name}</code>{call.function.arguments && <pre>{call.function.arguments}</pre>}</div></details></div>)}
      {content && <div className="bm-chat-assistant-message"><Text type="secondary" style={{ fontSize: 11, fontWeight: 600 }}>Miel</Text><div className="bm-md"><AssistantMarkdown content={content} conversationId={conversationId} messageId={message.id} /></div><ArtifactItems artifacts={message.artifacts} /><AssistantMessageFooter message={message} content={content} conversationId={conversationId} /></div>}
    </>
  )
}

export function AgentProcess({ phase, process, tools, skillProgress }: { phase: AgentPhase; process: AgentProcessStep[]; tools: AgentToolCall[]; skillProgress: { skill: string; stage: string; message?: string; state: string } | null }) {
  const active = !['done', 'error', 'idle'].includes(phase)
  const label = phase === 'thinking' ? '正在思考' : phase === 'tool' ? '正在使用工具' : phase === 'responding' ? '正在组织回复' : phase === 'done' ? '处理完成' : phase === 'error' ? '处理失败，对话已结束' : '正在等待模型响应'
  return (
    <div className="bm-agent-process">
      <Flex align="center" gap={8} className="bm-agent-status">
        {active ? <LoadingOutlined spin /> : phase === 'error' ? <WarningOutlined /> : <CheckCircleOutlined />}
        <Text type="secondary">{label}</Text>
      </Flex>
      {skillProgress && <div className="bm-tool-detail"><Text type="secondary">{skillProgress.skill} · {skillProgress.message || skillProgress.stage}</Text></div>}
      {process.map((step) => {
        if (step.type === 'reasoning') return step.content && <details className="bm-agent-detail" key={`reasoning-${step.id}`} open={step.status === 'thinking' && phase === 'thinking'}><summary><BulbOutlined /><span>思考过程</span></summary><div className="bm-agent-reasoning">{step.content}</div></details>
        if (step.type === 'text') return step.content && <div className="bm-chat-assistant-message" key={`text-${step.id}`}><Text type="secondary" style={{ fontSize: 11, fontWeight: 600 }}>Miel</Text><div className="bm-md">{renderMarkdown(step.content)}{step.status === 'streaming' && <span className="bm-cursor" />}</div></div>
        const tool = tools.find((item) => item.id === step.id)
        if (!tool) return null
        return <details className="bm-agent-detail" key={`tool-${tool.id}`}><summary>{tool.status === 'done' ? <CheckCircleOutlined /> : tool.status === 'failed' ? <WarningOutlined /> : <ToolOutlined />}<span>{getToolLabel(tool.name)}</span><Text type="secondary" className="bm-tool-status">{tool.status === 'preparing' ? '准备中' : tool.status === 'running' ? '执行中' : tool.status === 'failed' ? '已终止' : '已完成'}</Text></summary><div className="bm-tool-detail"><code>{tool.name}</code>{tool.args && <pre>{tool.args}</pre>}{tool.result && <pre>{tool.result}</pre>}</div></details>
      })}
    </div>
  )
}
