import { App } from 'antd'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })
})

const fixtures = vi.hoisted(() => ({
  models: [
    ['ark-code-latest', 'Ark Code Latest'],
    ['doubao-seed-evolving', 'Doubao Seed Evolving'],
    ['doubao-seed-2.0-mini', 'Doubao Seed 2.0 Mini'],
    ['doubao-seed-2.1-turbo', 'Doubao Seed 2.1 Turbo'],
    ['doubao-seed-2.0-lite', 'Doubao Seed 2.0 Lite'],
    ['minimax-m3', 'MiniMax M3'],
    ['glm-5.3', 'GLM-5.3'],
    ['glm-latest', 'GLM Latest'],
    ['glm-5.3-flash', 'GLM-5.3 Flash'],
    ['deepseek-v4-flash', 'DeepSeek V4 Flash'],
    ['deepseek-v4-pro', 'DeepSeek V4 Pro'],
    ['kimi-k2.7-code', 'Kimi K2.7 Code'],
    ['kimi-k3', 'Kimi K3'],
  ].map(([id, label]) => ({
    id,
    label,
    reasoning: { type: id === 'glm-5.3' || id === 'glm-latest' ? 'always' : 'none' },
    multimodal: id === 'glm-5.3-flash',
  })),
}))

vi.mock('../features/settings/providerEditorController', () => ({
  providerEditorController: {
    loadCatalog: vi.fn().mockResolvedValue([
      {
        kind: 'volcengine-plan',
        name: '火山方舟 Agent Plan',
        baseUrl: 'https://ark.cn-beijing.volces.com/api/plan/v3',
        models: fixtures.models,
      },
      {
        kind: 'openai',
        name: 'OpenAI',
        baseUrl: 'https://api.openai.com/v1',
        models: [
          { id: 'gpt-4o-mini', label: 'GPT-4o mini', reasoning: { type: 'none' }, multimodal: true },
          { id: 'o3-mini', label: 'o3-mini', reasoning: { type: 'effort', levels: ['low', 'medium', 'high'] }, multimodal: false },
        ],
      },
    ]),
    loadEnabledModels: vi.fn().mockResolvedValue([]),
    discoverModels: vi.fn().mockResolvedValue([]),
    testConnection: vi.fn(),
    save: vi.fn(),
  },
}))

import ProviderFormModal from './ProviderFormModal'
import { providerEditorController } from '../features/settings/providerEditorController'

describe('ProviderFormModal Agent Plan catalog', () => {
  beforeEach(() => {
    vi.mocked(providerEditorController.loadEnabledModels).mockResolvedValue([])
  })

  it('shows the built-in model list without offering unsupported remote discovery', async () => {
    render(
      <App>
        <ProviderFormModal
          open
          template={{
            name: '火山方舟 Agent Plan',
            kind: 'volcengine-plan',
            baseUrl: 'https://ark.cn-beijing.volces.com/api/plan/v3',
            model: 'ark-code-latest',
            multimodal: false,
            docsUrl: 'https://docs.volcengine.com/docs/82379/2366394?lang=zh',
          }}
          existing={null}
          onCancel={vi.fn()}
          onSaved={vi.fn()}
        />
      </App>,
    )

    expect(await screen.findByText('Ark Code Latest')).toBeInTheDocument()
    expect(screen.getByText('Doubao Seed 2.0 Mini')).toBeInTheDocument()
    expect(screen.getByText('预设 13 个')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /获取模型列表/ })).not.toBeInTheDocument()
    expect(screen.getByPlaceholderText('输入自定义模型 ID')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '添加自定义模型' })).toBeInTheDocument()
  })

  it('switches preset models and clears the previous provider draft', async () => {
    const user = userEvent.setup()
    render(
      <App>
        <ProviderFormModal
          open
          template={{
            name: '火山方舟 Agent Plan',
            kind: 'volcengine-plan',
            baseUrl: 'https://ark.cn-beijing.volces.com/api/plan/v3',
            model: 'ark-code-latest',
            multimodal: false,
            docsUrl: '',
          }}
          existing={null}
          onCancel={vi.fn()}
          onSaved={vi.fn()}
        />
      </App>,
    )

    expect(await screen.findByText('Ark Code Latest')).toBeInTheDocument()
    await user.type(screen.getByPlaceholderText('输入自定义模型 ID'), 'local-model')
    await user.click(screen.getByRole('button', { name: '添加自定义模型' }))
    expect(screen.getByText('local-model')).toBeInTheDocument()
    await user.type(screen.getByPlaceholderText('输入自定义模型 ID'), 'stale-draft')
    await user.click(screen.getByRole('combobox', { name: '服务商类型' }))
    await user.click(await screen.findByText('OpenAI'))

    expect(await screen.findByText('GPT-4o mini')).toBeInTheDocument()
    expect(screen.queryByText('Ark Code Latest')).not.toBeInTheDocument()
    expect(screen.queryByText('local-model')).not.toBeInTheDocument()
    expect(screen.getByText('预设 2 个')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('输入自定义模型 ID')).toHaveValue('')
  })

  it('maps a legacy Volcengine provider and restores its enabled models when editing', async () => {
    vi.mocked(providerEditorController.loadEnabledModels).mockResolvedValue([
      {
        model: 'ark-code-latest',
        label: 'Ark Code Latest',
        custom: false,
        multimodal: false,
      },
      {
        model: 'legacy-private-model',
        label: 'Legacy Private Model',
        custom: false,
        multimodal: true,
      },
    ])

    render(
      <App>
        <ProviderFormModal
          open
          template={null}
          existing={{
            id: 7,
            name: '我的火山套餐',
            kind: 'volcengine-coding',
            baseUrl: 'https://ark.cn-beijing.volces.com/api/coding/v3',
            apiKey: 'saved-key',
            model: 'ark-code-latest',
            multimodal: false,
            isDefault: true,
            createdAt: 1,
          }}
          onCancel={vi.fn()}
          onSaved={vi.fn()}
        />
      </App>,
    )

    expect(await screen.findByText('预设 13 个')).toBeInTheDocument()
    expect(screen.getByText('火山方舟 Agent Plan')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'API Base URL' })).toHaveValue(
      'https://ark.cn-beijing.volces.com/api/plan/v3',
    )
    expect(await screen.findByText('Legacy Private Model')).toBeInTheDocument()
    expect(screen.getByText('当前')).toBeInTheDocument()
  })
})
