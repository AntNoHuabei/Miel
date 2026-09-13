import { useCallback, useEffect, useState } from 'react'
import type { CatalogModelLite, CatalogProviderLite, ProviderLite, ProviderModelLite } from '../../api'
import { settingsRepository } from '../../shared/repositories'

export function useProviderController() {
  const [providers, setProviders] = useState<ProviderLite[]>([])
  const [catalog, setCatalog] = useState<CatalogProviderLite[]>([])
  const [modelsByProvider, setModelsByProvider] = useState<Record<number, ProviderModelLite[]>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)

  const reload = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [nextProviders, nextCatalog] = await Promise.all([
        settingsRepository.listProviders(),
        settingsRepository.modelCatalog(),
      ])
      const modelEntries = await Promise.all(nextProviders.map(async (provider) => {
        try {
          return [provider.id, await settingsRepository.providerModels(provider.id)] as const
        } catch {
          return [provider.id, []] as const
        }
      }))
      setProviders(nextProviders)
      setCatalog(nextCatalog)
      setModelsByProvider(Object.fromEntries(modelEntries))
    } catch (cause) {
      setError(cause)
      throw cause
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void reload().catch(() => undefined) }, [reload])

  const deleteProvider = useCallback(async (id: number) => {
    await settingsRepository.deleteProvider(id)
    await reload()
  }, [reload])

  const toggleBuiltin = useCallback(async (provider: ProviderLite, model: CatalogModelLite, enabled: boolean) => {
    if (enabled) await settingsRepository.enableModel(provider.id, model.id, model.label, false)
    else await settingsRepository.disableModel(provider.id, model.id)
    await reload()
  }, [reload])

  const toggleCustom = useCallback(async (provider: ProviderLite, model: ProviderModelLite, enabled: boolean) => {
    if (enabled) await settingsRepository.enableModel(provider.id, model.model, model.label, true)
    else await settingsRepository.disableModel(provider.id, model.model)
    await reload()
  }, [reload])

  const setCurrent = useCallback(async (providerId: number, model: string) => {
    await settingsRepository.setProviderModel(providerId, model)
    await reload()
  }, [reload])

  return { providers, catalog, modelsByProvider, loading, error, reload, deleteProvider, toggleBuiltin, toggleCustom, setCurrent }
}
