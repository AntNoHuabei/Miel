import { App as AntApp, Button, Flex, Tooltip, Typography } from 'antd'
import {
  BulbOutlined,
  CheckCircleOutlined,
  CopyOutlined,
  LoadingOutlined,
  ToolOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { AGUIMessageLite } from '../../../api'
import type { AgentPhase, AgentToolCall } from '../model/agentRun'
import type { AgentRunError } from '../model/chatError'
import { friendlyChatError, normalizeAgentRunError } from '../model/chatError'
import { ChatAttachmentStrip } from '../../../components/ChatAttachments'
import { getPermissionToolLabel } from '../../../components/permissions'
import { systemClipboardRepository } from '../../../shared/repositories'

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

function AssistantMessageFooter({ message, content }: { message: AGUIMessageLite; content: string }) {
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

export function SnapshotMessage({ message, toolName }: { message: AGUIMessageLite; toolName?: string }) {
  const content = messageContentText(message.content)
  if (message.role === 'error') {
    return <ChatRunErrorMessage error={normalizeAgentRunError(message.runError ?? message.error ?? content)} />
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
  if (message.role !== 'assistant') return null
  return (
    <>
      {(message.toolCalls ?? []).map((call) => <div className="bm-agent-process" key={call.id}><details className="bm-agent-detail"><summary><ToolOutlined /><span>{getToolLabel(call.function.name)}</span><Text type="secondary" className="bm-tool-status">已调用</Text></summary><div className="bm-tool-detail"><code>{call.function.name}</code>{call.function.arguments && <pre>{call.function.arguments}</pre>}</div></details></div>)}
      {content && <div className="bm-chat-assistant-message"><Text type="secondary" style={{ fontSize: 11, fontWeight: 600 }}>BlankMind</Text><div className="bm-md">{renderMarkdown(content)}</div><AssistantMessageFooter message={message} content={content} /></div>}
    </>
  )
}

export function AgentProcess({ phase, reasoning, tools }: { phase: AgentPhase; reasoning: string; tools: AgentToolCall[] }) {
  const active = !['done', 'error', 'idle'].includes(phase)
  const label = phase === 'thinking' ? '正在思考' : phase === 'tool' ? '正在使用工具' : phase === 'responding' ? '正在组织回复' : phase === 'done' ? '处理完成' : phase === 'error' ? '处理失败' : '正在等待模型响应'
  return (
    <div className="bm-agent-process">
      <Flex align="center" gap={8} className="bm-agent-status">
        {active ? <LoadingOutlined spin /> : phase === 'error' ? <WarningOutlined /> : <CheckCircleOutlined />}
        <Text type="secondary">{label}</Text>
      </Flex>
      {reasoning && <details className="bm-agent-detail" open={phase === 'thinking'}><summary><BulbOutlined /><span>思考过程</span></summary><div className="bm-agent-reasoning">{reasoning}</div></details>}
      {tools.map((tool) => <details className="bm-agent-detail" key={tool.id}><summary>{tool.status === 'done' ? <CheckCircleOutlined /> : <ToolOutlined />}<span>{getToolLabel(tool.name)}</span><Text type="secondary" className="bm-tool-status">{tool.status === 'preparing' ? '准备中' : tool.status === 'running' ? '执行中' : '已完成'}</Text></summary><div className="bm-tool-detail"><code>{tool.name}</code>{tool.args && <pre>{tool.args}</pre>}{tool.result && <pre>{tool.result}</pre>}</div></details>)}
    </div>
  )
}
