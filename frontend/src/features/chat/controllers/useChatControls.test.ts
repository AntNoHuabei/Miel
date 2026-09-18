import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { DiscoveredModelLite, ModelOptionLite } from '../../../api'

const mocks = vi.hoisted(() => ({
  listConversations: vi.fn(),
  listProviders: vi.fn(),
  modelOptions: vi.fn(),
  listWorkspaces: vi.fn(),
  agentProfileDefaults: vi.fn(),
  modelCatalog: vi.fn(),
  getSetting: vi.fn(),
}))

vi.mock('antd', () => ({ App: { useApp: () => ({ message: { error: vi.fn(), success: vi.fn() } }) } }))
vi.mock('../../../shared/repositories', () => ({
  chatRepository: { listConversations: mocks.listConversations, setConversationProfile: vi.fn(), setConversationModel: vi.fn() },
  settingsRepository: {
    listProviders: mocks.listProviders,
    modelOptions: mocks.modelOptions,
    listWorkspaces: mocks.listWorkspaces,
    agentProfileDefaults: mocks.agentProfileDefaults,
    modelCatalog: mocks.modelCatalog,
    getSetting: mocks.getSetting,
    discoverProviderModels: vi.fn(),
    setAgentProfileDefault: vi.fn(),
    setWorkspace: vi.fn(),
    pickWorkspace: vi.fn(),
    removeWorkspace: vi.fn(),
    setSetting: vi.fn(),
  },
}))
vi.mock('../../../shared/wails/events', () => ({ useWailsEvent: vi.fn() }))

import { chatModelLabel, reasoningStepsFor } from './useChatControls'
import { useChatControls } from './useChatControls'

const option: ModelOptionLite = {
  providerId: 1,
  providerName: 'OpenRouter',
  kind: 'openrouter',
  model: 'vendor/model',
  label: 'Vendor Model',
  custom: false,
  multimodal: false,
  isDefault: true,
}

function discovered(supportsTools: boolean): DiscoveredModelLite {
  return {
    id: option.model,
    status: '',
    reasoning: { type: 'none', levels: [], note: '' },
    multimodal: false,
    supportsTools,
  }
}

describe('chatModelLabel', () => {
  it('marks only OpenRouter models known not to support tools as plain chat', () => {
    expect(chatModelLabel(option, discovered(false))).toBe('Vendor Model (纯聊天)')
    expect(chatModelLabel(option, discovered(true))).toBe('Vendor Model')
    expect(chatModelLabel(option)).toBe('Vendor Model')
  })
})

describe('reasoningStepsFor', () => {
  it('keeps the provider default and exposes the official Volcengine Agent Plan levels', () => {
    expect(reasoningStepsFor(
      { type: 'effort', levels: ['low', 'medium', 'high'] },
      'volcengine-plan',
    )).toEqual(['', 'low', 'medium', 'high'])
  })
})

describe('useChatControls', () => {
  beforeEach(() => {
    mocks.listConversations.mockReset()
    mocks.listProviders.mockResolvedValue([])
    mocks.modelOptions.mockResolvedValue([])
    mocks.listWorkspaces.mockResolvedValue([])
    mocks.agentProfileDefaults.mockResolvedValue({ work: { providerId: 0, model: '' }, coding: { providerId: 0, model: '' } })
    mocks.modelCatalog.mockResolvedValue([])
    mocks.getSetting.mockResolvedValue('')
  })

  it('keeps Coding selected when a new conversation clears the active conversation ID', async () => {
    mocks.listConversations.mockResolvedValue([{ id: 9, agentProfile: 'coding', profileModels: {} }])
    const { result, rerender } = renderHook(({ conversationId }) => useChatControls(conversationId), { initialProps: { conversationId: 9 } })
    await waitFor(() => expect(result.current.agentProfile).toBe('coding'))

    act(() => rerender({ conversationId: 0 }))
    await waitFor(() => expect(result.current.profileReady).toBe(true))
    expect(result.current.agentProfile).toBe('coding')
  })
})
