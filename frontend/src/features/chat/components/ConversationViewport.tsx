import { Fragment, useMemo } from 'react'
import type { RefObject } from 'react'
import { Button, Flex, Typography } from 'antd'
import { ArrowRightOutlined } from '@ant-design/icons'
import type { AGUIMessageLite } from '../../../api'
import type { ApprovalDecision, ApprovalRequest } from '../../../components/permissions'
import { PermissionApprovalModal } from '../../../components/permissions'
import type { AgentPhase, AgentProcessStep, AgentToolCall } from '../model/agentRun'
import type { AgentRunError } from '../model/chatError'
import { AgentProcess, ChatRunErrorMessage, isPersistedTailError, renderMarkdown, SnapshotMessage } from './ConversationMessages'

const { Text } = Typography

interface ConversationViewportProps {
  messages: AGUIMessageLite[]
  streaming: string
  sending: boolean
  phase: AgentPhase
  process: AgentProcessStep[]
  tools: AgentToolCall[]
  error: AgentRunError | null
  pendingApproval: ApprovalRequest | null
  resolvingApproval: boolean
  scrollRef: RefObject<HTMLDivElement>
  quickPrompts: string[]
  onQuickPrompt: (prompt: string) => void
  onResolveApproval: (decision: ApprovalDecision) => void | Promise<void>
}

export function ConversationViewport({
  messages,
  streaming,
  sending,
  phase,
  process,
  tools,
  error,
  pendingApproval,
  resolvingApproval,
  scrollRef,
  quickPrompts,
  onQuickPrompt,
  onResolveApproval,
}: ConversationViewportProps) {
  const showProcess = sending || process.length > 0 || tools.length > 0
  const processBeforeIndex = showProcess && messages[messages.length - 1]?.role === 'assistant' ? messages.length - 1 : -1
  const toolNames = useMemo(() => {
    const names = new Map<string, string>()
    for (const item of messages) for (const call of item.toolCalls ?? []) names.set(call.id, call.function.name)
    return names
  }, [messages])

  return (
    <div className={`bm-chat-messages ${messages.length === 0 && !streaming && !error ? 'is-empty' : ''}`} ref={scrollRef} style={{ flex: 1, overflowY: 'auto', minWidth: 0, minHeight: 0 }}>
      <Flex vertical className="bm-chat-message-track" style={{ padding: '8px 24px 24px' }}>
        {messages.length === 0 && !streaming && !error && (
          <section className="bm-chat-empty-state" aria-labelledby="bm-chat-empty-title">
            <div className="bm-chat-empty-intro">
              <Typography.Title level={1} id="bm-chat-empty-title" className="bm-chat-empty-title">你好，我是 Miel</Typography.Title>
              <Text type="secondary" className="bm-chat-empty-copy">可以让我记录待办、安排里程碑、生成周报，或创建文档与表格。</Text>
            </div>
            <div className="bm-chat-quick-list">
              {quickPrompts.map((prompt) => <Button key={prompt} type="text" className="bm-chat-quick-item" onClick={() => onQuickPrompt(prompt)}><span>{prompt}</span><ArrowRightOutlined className="bm-chat-quick-arrow" /></Button>)}
            </div>
          </section>
        )}
        {messages.map((message, index) => (
          <Fragment key={message.id || index}>
            {index === processBeforeIndex && <AgentProcess phase={phase} process={process} tools={tools} />}
            <SnapshotMessage message={message} toolName={message.toolCallId ? toolNames.get(message.toolCallId) : undefined} />
          </Fragment>
        ))}
        {showProcess && processBeforeIndex < 0 && <AgentProcess phase={phase} process={process} tools={tools} />}
        {streaming && <div style={{ margin: '6px 0' }}><Text type="secondary" style={{ fontSize: 11, fontWeight: 600 }}>Miel</Text><div className="bm-md">{renderMarkdown(streaming)}<span className="bm-cursor" /></div></div>}
        {error && !isPersistedTailError(messages, error) && <ChatRunErrorMessage error={error} />}
        {pendingApproval && <PermissionApprovalModal request={pendingApproval} onResolve={onResolveApproval} resolving={resolvingApproval} />}
      </Flex>
    </div>
  )
}
