import { useMemo } from 'react'
import type { RefObject, UIEventHandler } from 'react'
import { Button, Flex, Typography } from 'antd'
import { ArrowRightOutlined, PlayCircleOutlined } from '@ant-design/icons'
import type { ConversationTimelineItemLite } from '../../../api'
import type { ApprovalDecision, ApprovalRequest } from '../../../components/permissions'
import { PermissionApprovalModal } from '../../../components/permissions'
import type { AgentPhase, AgentProcessStep, AgentToolCall } from '../model/agentRun'
import type { AgentRunError } from '../model/chatError'
import type { ArtifactRefLite } from '../../../shared/types/artifacts'
import type { PlanLite } from '../../../shared/types/chat'
import { AgentProcess, ChatRunErrorMessage, isPersistedTailError, PlanMessage, renderMarkdown, SnapshotMessage } from './ConversationMessages'
import { ArtifactItems } from '../../artifacts/components/ArtifactItems'

const { Text } = Typography

interface ConversationViewportProps {
  conversationId: number
  timeline: ConversationTimelineItemLite[]
  streaming: string
  sending: boolean
  phase: AgentPhase
  process: AgentProcessStep[]
  tools: AgentToolCall[]
  skillProgress?: { skill: string; stage: string; message?: string; state: string } | null
  artifacts: ArtifactRefLite[]
  error: AgentRunError | null
  pendingApproval: ApprovalRequest | null
  resolvingApproval: boolean
  scrollRef: RefObject<HTMLDivElement>
  onScroll?: UIEventHandler<HTMLDivElement>
  quickPrompts: string[]
  onQuickPrompt: (prompt: string) => void
  onResolveApproval: (decision: ApprovalDecision) => void | Promise<void>
  onExecutePlan?: (plan: PlanLite) => void | Promise<void>
  onRevisePlan?: (plan: PlanLite, instruction: string) => void | Promise<void>
  onAbandonPlan?: (plan: PlanLite) => void | Promise<void>
  onOpenPlan?: (plan: PlanLite) => void
}

export function ConversationViewport({
  conversationId,
  timeline,
  streaming,
  sending,
  phase,
  process,
  tools,
  skillProgress = null,
  artifacts,
  error,
  pendingApproval,
  resolvingApproval,
  scrollRef,
  onScroll,
  quickPrompts,
  onQuickPrompt,
  onResolveApproval,
  onExecutePlan,
  onRevisePlan,
  onAbandonPlan,
  onOpenPlan,
}: ConversationViewportProps) {
  const showProcess = sending || process.length > 0 || tools.length > 0 || skillProgress !== null
  const hasTimelineText = process.some((step) => step.type === 'text')
  const messages = useMemo(() => timeline.flatMap((item) => item.kind === 'message' ? [item.message] : []), [timeline])
  const toolNames = useMemo(() => {
    const names = new Map<string, string>()
    for (const item of messages) for (const call of item.toolCalls ?? []) names.set(call.id, call.function.name)
    return names
  }, [messages])

  return (
    <div className={`bm-chat-messages ${timeline.length === 0 && !streaming && !error ? 'is-empty' : ''}`} ref={scrollRef} onScroll={onScroll} style={{ flex: 1, overflowY: 'auto', minWidth: 0, minHeight: 0 }}>
      <Flex vertical className="bm-chat-message-track" style={{ padding: '8px 24px 24px' }}>
        {timeline.length === 0 && !streaming && !error && (
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
        {timeline.map((item) => {
          if (item.kind === 'message') return <SnapshotMessage key={`message:${item.message.id}`} message={item.message} conversationId={conversationId} toolName={item.message.toolCallId ? toolNames.get(item.message.toolCallId) : undefined} />
          if (item.kind === 'plan_request') return <SnapshotMessage key={`plan-request:${item.request.id}`} message={{ id: item.request.messageId, role: 'user', content: item.request.content }} conversationId={conversationId} />
          if (item.kind === 'plan') return <PlanMessage key={`plan:${item.plan.id}:${item.plan.revision}`} plan={item.plan} sending={sending} onOpen={onOpenPlan} onExecute={onExecutePlan} onRevise={onRevisePlan} onAbandon={onAbandonPlan} />
          return <div className="bm-plan-execution-event" key={`plan-execution:${item.execution.id}`}><PlayCircleOutlined /><span>{String(item.execution.content || `执行计划 v${item.execution.revision}`)}</span></div>
        })}
        {showProcess && <AgentProcess phase={phase} process={process} tools={tools} skillProgress={skillProgress} />}
        {streaming && !hasTimelineText && <div style={{ margin: '6px 0' }}><Text type="secondary" style={{ fontSize: 11, fontWeight: 600 }}>Miel</Text><div className="bm-md">{renderMarkdown(streaming)}<span className="bm-cursor" /></div></div>}
        <ArtifactItems artifacts={artifacts} />
        {error && !isPersistedTailError(messages, error) && <ChatRunErrorMessage error={error} />}
        {pendingApproval && <PermissionApprovalModal request={pendingApproval} onResolve={onResolveApproval} resolving={resolvingApproval} />}
      </Flex>
    </div>
  )
}
