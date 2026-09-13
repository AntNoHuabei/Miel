import { App } from 'antd'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const repositories = vi.hoisted(() => ({
  listProviders: vi.fn(),
  modelCatalog: vi.fn(),
  listSkills: vi.fn(),
  listHub: vi.fn(),
  setSkillEnabled: vi.fn(),
}))

vi.mock('../shared/repositories', () => ({
  settingsRepository: {
    listProviders: repositories.listProviders,
    modelCatalog: repositories.modelCatalog,
    providerModels: vi.fn().mockResolvedValue([]),
    providerTemplates: vi.fn().mockResolvedValue([]),
  },
  skillRepository: {
    list: repositories.listSkills,
    listHub: repositories.listHub,
    pickLocal: vi.fn(),
    setEnabled: repositories.setSkillEnabled,
    delete: vi.fn(),
    installHub: vi.fn(),
  },
  directoryRepository: { paths: vi.fn().mockResolvedValue({ root: 'D:/data' }), openDataDir: vi.fn() },
  memoryRepository: {
    getSettings: vi.fn(), list: vi.fn(), saveSettings: vi.fn(), add: vi.fn(), update: vi.fn(), delete: vi.fn(), clear: vi.fn(), export: vi.fn(),
  },
}))

vi.mock('../components/ProviderFormModal', () => ({ default: () => null }))
vi.mock('../components/MemorySettingsPanel', () => ({ default: () => <div>memory panel</div> }))
vi.mock('../theme/ThemeContext', () => ({
  BM_THEMES: [],
  useBMTheme: () => ({ themeId: 'system', setTheme: vi.fn() }),
}))

import SettingsView from './SettingsView'

describe('SettingsView loading boundaries', () => {
  beforeEach(() => {
    repositories.listProviders.mockReset().mockResolvedValue([])
    repositories.modelCatalog.mockReset().mockResolvedValue([])
    repositories.listSkills.mockReset().mockResolvedValue([])
    repositories.listHub.mockReset().mockResolvedValue({ skills: [], total: 0, page: 1, pages: 0 })
    repositories.setSkillEnabled.mockReset().mockResolvedValue(undefined)
  })

  it('loads only the active section', async () => {
    render(<App><SettingsView /></App>)
    await waitFor(() => expect(repositories.listProviders).toHaveBeenCalledTimes(1))
    expect(repositories.listSkills).not.toHaveBeenCalled()
    expect(repositories.listHub).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: /技能管理/ }))
    await waitFor(() => expect(repositories.listSkills).toHaveBeenCalledTimes(1))
    expect(repositories.listHub).not.toHaveBeenCalled()
  })

  it('requests SkillHub only after its tab is selected', async () => {
    render(<App><SettingsView /></App>)
    await userEvent.click(screen.getByRole('button', { name: /技能管理/ }))
    await waitFor(() => expect(repositories.listSkills).toHaveBeenCalled())
    await userEvent.click(screen.getByRole('tab', { name: /SkillHub/ }))
    await waitFor(() => expect(repositories.listHub).toHaveBeenCalledWith(1, 12, ''))
  })

  it('allows built-in skills to be disabled without allowing deletion', async () => {
    repositories.listSkills.mockResolvedValue([{ name: 'todo', description: 'Todo', source: 'builtin', enabled: true, path: 'skills/todo', updatedAt: 1 }])
    render(<App><SettingsView /></App>)
    await userEvent.click(screen.getByRole('button', { name: /技能管理/ }))
    const toggle = await screen.findByRole('switch', { name: '停用 todo' })
    expect(toggle).toBeEnabled()
    expect(screen.queryByRole('button', { name: '删除 todo' })).not.toBeInTheDocument()
    await userEvent.click(toggle)
    await waitFor(() => expect(repositories.setSkillEnabled).toHaveBeenCalledWith('todo', false))
  })
})
