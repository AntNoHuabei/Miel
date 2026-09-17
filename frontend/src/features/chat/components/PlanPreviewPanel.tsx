import { App as AntApp, Button, Tooltip, Typography } from 'antd'
import { CloseOutlined, CopyOutlined, DownloadOutlined } from '@ant-design/icons'
import type { PlanLite } from '../../../shared/types/chat'
import { systemClipboardRepository } from '../../../shared/repositories'
import { useArtifactPreviewStore } from '../../artifacts/artifactStore'
import { PreviewPanelShell } from '../../artifacts/components/PreviewPanelShell'
import type { AgentPhase, AgentProcessStep, AgentToolCall } from '../model/agentRun'
import { AgentProcess, isPlanActionable, PLAN_STATUS, PlanActions, planTitle, renderMarkdown } from './ConversationMessages'

interface PlanPreviewPanelProps {
  sending: boolean
  phase?: AgentPhase
  process?: AgentProcessStep[]
  tools?: AgentToolCall[]
  streaming?: string
  onExecute?: (plan: PlanLite) => void | Promise<void>
  onRevise?: (plan: PlanLite, instruction: string) => void | Promise<void>
  onAbandon?: (plan: PlanLite) => void | Promise<void>
}

function downloadPlan(plan: PlanLite) {
  const blob = new Blob([plan.content], { type: 'text/markdown;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `plan-v${plan.revision}.md`
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

export function PlanPreviewPanel({ sending, phase = 'idle', process = [], tools = [], streaming = '', onExecute, onRevise, onAbandon }: PlanPreviewPanelProps) {
  const { message } = AntApp.useApp()
  const plan = useArtifactPreviewStore((state) => state.selectedPlan)
  const close = useArtifactPreviewStore((state) => state.close)
  if (!plan) return null
  const liveOutput = sending && plan.status === 'generating'
  const header = <><div><Typography.Text strong ellipsis>{planTitle(plan.content)}</Typography.Text><span>Plan v{plan.revision} · {plan.agentProfile === 'coding' ? 'Coding' : 'Work'} · {PLAN_STATUS[plan.status] ?? plan.status} · {plan.generatedModel}{plan.executionModel ? ` · 执行 ${plan.executionModel}` : ''}</span></div><Tooltip title="关闭计划"><Button type="text" aria-label="关闭计划" icon={<CloseOutlined />} onClick={close} /></Tooltip></>
  const toolbar = <><Typography.Text type="secondary">{liveOutput ? '实时输出' : '完整计划'}</Typography.Text><span /><Tooltip title="复制 Markdown"><Button type="text" aria-label="复制计划" icon={<CopyOutlined />} onClick={() => void systemClipboardRepository.setText(plan.content).then(() => message.success('已复制')).catch((error) => message.error(String(error)))} /></Tooltip><Tooltip title="下载 Markdown"><Button type="text" aria-label="下载计划" icon={<DownloadOutlined />} onClick={() => downloadPlan(plan)} /></Tooltip></>
  const footer = isPlanActionable(plan) ? <PlanActions plan={plan} sending={sending} onExecute={onExecute} onRevise={onRevise} onAbandon={onAbandon} /> : undefined
  const body = liveOutput
    ? <div className="bm-plan-run-output"><AgentProcess phase={phase} process={process} tools={tools} skillProgress={null} />{streaming && process.filter((step) => step.type === 'text').length === 0 && <div className="bm-md">{renderMarkdown(streaming)}<span className="bm-cursor" /></div>}</div>
    : <div className="bm-md bm-plan-preview-markdown">{renderMarkdown(plan.content)}</div>
  return <PreviewPanelShell ariaLabel={liveOutput ? '规划输出' : '计划详情'} header={header} toolbar={toolbar} footer={footer} bodyClassName="bm-plan-preview-body">{body}</PreviewPanelShell>
}
