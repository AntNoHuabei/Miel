import type { ProviderInputLite } from '../../api'
import { settingsRepository } from '../../shared/repositories'

export const providerEditorController = {
  loadCatalog: () => settingsRepository.modelCatalog(),
  loadEnabledModels: (providerId: number) => settingsRepository.providerModels(providerId),
  discoverModels: (input: ProviderInputLite) => settingsRepository.discoverProviderModels(input),
  testConnection: (input: ProviderInputLite) => settingsRepository.pingProvider(input),
  save: (input: ProviderInputLite) => settingsRepository.saveProvider(input),
}
