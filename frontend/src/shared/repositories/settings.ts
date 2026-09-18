import type {
  CatalogProviderLite,
  DiscoveredModelLite,
  ModelOptionLite,
  PingResultLite,
  ProviderInputLite,
  ProviderLite,
  ProviderModelLite,
  ProviderTemplateLite,
  WorkspaceLite,
	AgentProfileDefaultsLite,
} from '../types/settings'
import { Services } from './bindings'
import { normalizeList } from './helpers'

export const normalizeProviderInput = (input: ProviderInputLite): Parameters<typeof Services.SettingsService.SaveProvider>[0] => ({
  ...input,
  id: input.id ?? 0,
  models: (input.models ?? []).map((model) => ({
    model: model.model,
    label: model.label ?? '',
    custom: model.custom ?? false,
    multimodal: model.multimodal ?? false,
    contextWindow: model.contextWindow ?? 0,
  })),
})

export const settingsRepository = {
  hasProviders: () => Services.SettingsService.HasProviders(),
  listProviders: async () => normalizeList(await Services.SettingsService.ListProviders()) as ProviderLite[],
  modelCatalog: async () => normalizeList(await Services.SettingsService.ModelCatalog()) as CatalogProviderLite[],
  providerModels: async (id: number) => normalizeList(await Services.SettingsService.ProviderModels(id)) as ProviderModelLite[],
  providerTemplates: async () => normalizeList(await Services.SettingsService.ProviderTemplates()) as ProviderTemplateLite[],
  discoverProviderModels: async (input: ProviderInputLite) => normalizeList(await Services.SettingsService.DiscoverProviderModels(normalizeProviderInput(input))) as DiscoveredModelLite[],
  modelOptions: async () => normalizeList(await Services.SettingsService.ModelOptions()) as ModelOptionLite[],
  listWorkspaces: async () => normalizeList(await Services.SettingsService.ListWorkspaces()) as WorkspaceLite[],
  pickWorkspace: async () => (await Services.SettingsService.PickWorkspace()) as WorkspaceLite,
  setWorkspace: (path: string) => Services.SettingsService.SetWorkspace(path),
  removeWorkspace: (path: string) => Services.SettingsService.RemoveWorkspace(path),
  defaultModelSupportsVision: () => Services.SettingsService.DefaultModelSupportsVision(),
  getSetting: (key: string) => Services.SettingsService.GetSetting(key),
  setSetting: (key: string, value: string) => Services.SettingsService.SetSetting(key, value),
  setProviderModel: (id: number, model: string) => Services.SettingsService.SetProviderModel(id, model),
  agentProfileDefaults: async () => (await Services.SettingsService.GetAgentProfileDefaults()) as AgentProfileDefaultsLite,
  setAgentProfileDefault: (profile: 'work' | 'coding', providerId: number, model: string) => Services.SettingsService.SetAgentProfileDefault({ profile, providerId, model }),
  enableModel: (id: number, model: string, label: string, custom: boolean) => Services.SettingsService.EnableModel(id, model, label, custom),
  disableModel: (id: number, model: string) => Services.SettingsService.DisableModel(id, model),
  deleteProvider: (id: number) => Services.SettingsService.DeleteProvider(id),
  pingProvider: async (input: ProviderInputLite) => (await Services.SettingsService.PingProvider(normalizeProviderInput(input))) as PingResultLite,
  saveProvider: (input: ProviderInputLite) => Services.SettingsService.SaveProvider(normalizeProviderInput(input)),
}
