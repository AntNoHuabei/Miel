import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

vi.mock('../../../components/ChatAttachments', () => ({
  ChatAttachmentStrip: ({ attachments }: { attachments: unknown[] }) => <div data-testid="attachments">{attachments.length}</div>,
}))
vi.mock('../../../components/permissions', () => ({
  PermissionModeSelector: () => <button type="button">权限模式</button>,
}))

import { ConversationComposer } from './ConversationComposer'

const attachment = { id: 'image-1', name: 'image.png', mimeType: 'image/png', size: 1, width: 1, height: 1, thumbnailDataUri: 'data:image/png;base64,' }

function props() {
  return {
    input: '', sending: false, attachments: [attachment], supportsImages: false, permissionMode: 'ask' as const,
    showWorkspaceControl: false, workspaces: [], currentWorkspace: undefined, selectedModel: '1::model',
    modelOptions: [{ label: 'Provider', options: [{ value: '1::model', label: 'Model' }] }], activeModelLabel: 'Model',
    reasoningPillLabel: '关闭', reasoningStatus: '关闭', reasoningSteps: ['', 'low'], reasoningIndex: 0,
    reasoningMarks: { 0: '关闭', 1: '低' }, reasoningLocked: false, showCompatibleModelNote: false,
    onAddWorkspace: vi.fn(), onChangeInput: vi.fn(), onChangePermissionMode: vi.fn(), onChangeReasoning: vi.fn(),
    onChooseWorkspace: vi.fn(), onPaste: vi.fn(), onPickImages: vi.fn(), onRemoveAttachment: vi.fn(),
    onRemoveWorkspace: vi.fn(), onSend: vi.fn(), onSwitchModel: vi.fn(),
  }
}

describe('ConversationComposer', () => {
  it('hides workspace selection outside a new empty conversation and blocks unsupported attachments', () => {
    const view = render(<ConversationComposer {...props()} />)
    expect(screen.queryByText('不在工作区')).not.toBeInTheDocument()
    expect(screen.getByText('当前模型不支持图片输入')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'send' })).toBeDisabled()

    view.rerender(<ConversationComposer {...props()} showWorkspaceControl currentWorkspace={{ name: 'BlankMind', path: 'D:/code/BlankMind', isCurrent: true }} />)
    expect(screen.getByText('BlankMind')).toBeInTheDocument()
  })
})
