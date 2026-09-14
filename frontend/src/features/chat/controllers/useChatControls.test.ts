import { describe, expect, it } from 'vitest'
import type { DiscoveredModelLite, ModelOptionLite } from '../../../api'
import { chatModelLabel, reasoningStepsFor } from './useChatControls'

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
