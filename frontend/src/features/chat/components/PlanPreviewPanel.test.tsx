import { App } from 'antd'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useArtifactPreviewStore } from '../../artifacts/artifactStore'
import { PlanPreviewPanel } from './PlanPreviewPanel'

vi.mock('../../../shared/repositories', () => ({ systemClipboardRepository: { setText: vi.fn().mockResolvedValue(undefined) } }))

const plan = { id: 4, messageId: 'plan-4', revision: 2, currentRevision: 2, status: 'pending', content: '# Release plan\n\nShip the timeline.', generatedModel: 'model-a', createdAt: 1 }

describe('PlanPreviewPanel', () => {
  beforeEach(() => useArtifactPreviewStore.getState().close())

  it('shows the complete plan and executes from the fixed footer', async () => {
    const execute = vi.fn()
    useArtifactPreviewStore.getState().openPlan(plan)
    render(<App><PlanPreviewPanel sending={false} onExecute={execute} /></App>)
    expect(screen.getByRole('complementary', { name: '计划详情' })).toHaveTextContent('Ship the timeline.')
    expect(screen.getByRole('button', { name: '复制计划' })).toBeVisible()
    expect(screen.getByRole('button', { name: '下载计划' })).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: /确认执行/ }))
    expect(execute).toHaveBeenCalledWith(plan)
  })

  it('keeps plan and artifact previews mutually exclusive', () => {
    useArtifactPreviewStore.getState().open({ id: 'artifact', version: 0, name: 'a.md', mimeType: 'text/markdown', kind: 'markdown', size: 1, availability: 'available', width: 0, height: 0 })
    useArtifactPreviewStore.getState().openPlan(plan)
    expect(useArtifactPreviewStore.getState().selected).toBeNull()
    expect(useArtifactPreviewStore.getState().selectedPlan).toEqual(plan)
  })

  it('does not render an action footer after execution has started', () => {
    useArtifactPreviewStore.getState().openPlan({ ...plan, status: 'interrupted', executionModel: 'model-b' })
    const { container } = render(<App><PlanPreviewPanel sending={false} onExecute={vi.fn()} onRevise={vi.fn()} onAbandon={vi.fn()} /></App>)
    expect(screen.getByText(/已中断/)).toBeVisible()
    expect(container.querySelector('.bm-preview-footer')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /执行/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Revise/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '废弃' })).not.toBeInTheDocument()
  })

  it('keeps the saved plan visible while it is being executed', () => {
    useArtifactPreviewStore.getState().openPlan({ ...plan, status: 'executing' })
    render(<App><PlanPreviewPanel sending /></App>)
    expect(screen.getByRole('complementary', { name: '计划详情' })).toHaveTextContent('Ship the timeline.')
  })
})
