import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { App as AntApp } from 'antd'
import type { CatalogProviderLite, DiscoveredModelLite, ModelOptionLite, ProviderLite, WorkspaceLite } from '../../../api'
import { settingsRepository } from '../../../shared/repositories'
import { useWailsEvent } from '../../../shared/wails/events'

const REASONING_STORAGE_KEY = 'chat.reasoning.v1'
const LEVEL_LABEL: Record<string, string> = { low: '低', medium: '中', high: '高', xhigh: '极高', max: '最高' }

export function useChatControls() {
  const { message } = AntApp.useApp()
  const [providers, setProviders] = useState<ProviderLite[]>([])
  const [modelOptions, setModelOptions] = useState<ModelOptionLite[]>([])
  const [catalog, setCatalog] = useState<CatalogProviderLite[]>([])
  const [discoveredModels, setDiscoveredModels] = useState<Record<number, DiscoveredModelLite[]>>({})
  const [workspaces, setWorkspaces] = useState<WorkspaceLite[]>([])
  const [reasoning, setReasoning] = useState('')
  const [reasoningPreferences, setReasoningPreferences] = useState<Record<string, string>>({})
  const reasoningPreferencesRef = useRef<Record<string, string>>({})

  const defaultProvider = providers.find((provider) => provider.isDefault) ?? providers[0]
  const optionGroups = useMemo(() => {
    const groups = new Map<string, Array<{ value: string; label: string }>>()
    for (const option of modelOptions) {
      if (!groups.has(option.providerName)) groups.set(option.providerName, [])
      groups.get(option.providerName)?.push({
        value: `${option.providerId}::${option.model}`,
        label: option.custom ? `${option.model} (自定义)` : option.label || option.model,
      })
    }
    return Array.from(groups, ([label, options]) => ({ label, options }))
  }, [modelOptions])

  const catalogProvider = catalog.find((provider) => provider.kind.toLowerCase() === defaultProvider?.kind.toLowerCase())
  const catalogModel = catalogProvider?.models.find((model) => model.id.toLowerCase() === defaultProvider?.model.toLowerCase())
  const discoveredModel = defaultProvider
    ? discoveredModels[defaultProvider.id]?.find((model) => model.id.toLowerCase() === defaultProvider.model.toLowerCase())
    : undefined
  const reasoningSpec = discoveredModel?.reasoning ?? catalogModel?.reasoning
  const supportsImages = discoveredModel?.multimodal ?? catalogModel?.multimodal ?? defaultProvider?.multimodal ?? false
  const specType = reasoningSpec?.type ?? 'none'
  const customGateway = defaultProvider?.kind === 'custom'
  const herdsman = defaultProvider?.kind === 'herdsman'
  const compatibleGateway = customGateway || herdsman
  const canDisableReasoning = specType === 'toggle' || (specType === 'effort' && defaultProvider?.kind === 'deepseek')
  const reasoningSteps = useMemo(() => {
    if (specType === 'effort') {
      const levels = (reasoningSpec?.levels ?? []).filter((level) => LEVEL_LABEL[level])
      if (herdsman) return ['', 'off', ...levels]
      return canDisableReasoning ? ['', ...levels] : levels
    }
    if (specType === 'toggle') return herdsman ? ['', 'off', 'on'] : ['', 'on']
    if (customGateway) return ['', 'low', 'medium', 'high']
    return []
  }, [reasoningSpec, specType, customGateway, herdsman, canDisableReasoning])
  const reasoningLocked = reasoningSteps.length <= 1
  const effectiveReasoning = reasoningSteps.includes(reasoning) ? reasoning : (reasoningSteps[0] ?? '')
  const reasoningIndex = Math.max(reasoningSteps.indexOf(effectiveReasoning), 0)
  const reasoningMarks = useMemo(() => {
    const marks: Record<number, string> = {}
    reasoningSteps.forEach((step, index) => {
      if (step === '') marks[index] = compatibleGateway ? '服务商默认' : '关闭'
      else if (step === 'off') marks[index] = '关闭'
      else if (step === 'on') marks[index] = '开启'
      else marks[index] = LEVEL_LABEL[step] ?? step
    })
    return marks
  }, [reasoningSteps, compatibleGateway])
  const activeModel = modelOptions.find((option) => option.providerId === defaultProvider?.id && option.model === defaultProvider?.model)
  const activeModelLabel = activeModel ? (activeModel.custom ? `${activeModel.model} (自定义)` : activeModel.label || activeModel.model) : defaultProvider?.model || '未配置模型'
  const reasoningStatus = reasoningLocked ? (specType === 'always' ? '思考常开' : specType === 'none' ? '不支持思考' : '不可调节') : reasoningMarks[reasoningIndex] ?? '关闭'
  const reasoningPillLabel = reasoningLocked ? (specType === 'always' ? '常开' : '') : reasoningMarks[reasoningIndex] ?? '关闭'
  const reasoningPreferenceKey = defaultProvider ? `${defaultProvider.id}::${defaultProvider.model}` : ''
  const currentWorkspace = workspaces.find((workspace) => workspace.isCurrent)

  const reloadProviders = useCallback(async () => {
    try {
      const nextProviders = await settingsRepository.listProviders()
      setProviders(nextProviders)
      const entries = await Promise.all(nextProviders.filter((provider) => provider.kind.toLowerCase() === 'herdsman').map(async (provider) => {
        try { return [provider.id, await settingsRepository.discoverProviderModels({ ...provider, models: [] })] as const }
        catch { return [provider.id, []] as const }
      }))
      setDiscoveredModels(Object.fromEntries(entries))
    } catch { /* Models stay unavailable until the next change event. */ }
  }, [])

  const reloadModelOptions = useCallback(async () => {
    try { setModelOptions(await settingsRepository.modelOptions()) } catch { /* Keep the last valid options. */ }
  }, [])
  const reloadWorkspaces = useCallback(async () => {
    try { setWorkspaces(await settingsRepository.listWorkspaces()) } catch { /* Keep the last valid workspaces. */ }
  }, [])

  useEffect(() => {
    void reloadProviders()
    void reloadModelOptions()
    void reloadWorkspaces()
    settingsRepository.modelCatalog().then(setCatalog).catch(() => undefined)
    settingsRepository.getSetting(REASONING_STORAGE_KEY).then((raw) => {
      if (!raw) return
      const parsed: unknown = JSON.parse(raw)
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return
      const preferences = Object.fromEntries(Object.entries(parsed).filter((entry): entry is [string, string] => typeof entry[1] === 'string'))
      const merged = { ...preferences, ...reasoningPreferencesRef.current }
      reasoningPreferencesRef.current = merged
      setReasoningPreferences(merged)
    }).catch(() => undefined)
  }, [reloadModelOptions, reloadProviders, reloadWorkspaces])

  useWailsEvent<string>('models.changed', useCallback(() => {
    void reloadProviders()
    void reloadModelOptions()
  }, [reloadProviders, reloadModelOptions]))

  const modelKey = `${defaultProvider?.kind ?? ''}:${defaultProvider?.model ?? ''}`
  useEffect(() => {
    const saved = reasoningPreferenceKey ? reasoningPreferences[reasoningPreferenceKey] : undefined
    setReasoning(saved !== undefined && reasoningSteps.includes(saved) ? saved : (reasoningSteps[0] ?? ''))
  }, [modelKey, reasoningPreferenceKey, reasoningPreferences, reasoningSteps])

  const chooseWorkspace = useCallback(async (path: string) => {
    try { await settingsRepository.setWorkspace(path); await reloadWorkspaces() }
    catch (error) { message.error(`切换工作区失败: ${String(error)}`) }
  }, [message, reloadWorkspaces])
  const addWorkspace = useCallback(async () => {
    try { const workspace = await settingsRepository.pickWorkspace(); if (workspace?.path) await reloadWorkspaces() }
    catch (error) { message.error(`添加工作区失败: ${String(error)}`) }
  }, [message, reloadWorkspaces])
  const removeWorkspace = useCallback(async (workspace: WorkspaceLite) => {
    try { await settingsRepository.removeWorkspace(workspace.path); await reloadWorkspaces() }
    catch (error) { message.error(`移除工作区失败: ${String(error)}`) }
  }, [message, reloadWorkspaces])
  const switchModel = useCallback(async (providerId: number, model: string) => {
    try {
      await settingsRepository.setProviderModel(providerId, model)
      message.success(`已切换到 ${model}`)
      await Promise.all([reloadProviders(), reloadModelOptions()])
    } catch (error) { message.error(`切换失败:${String(error)}`) }
  }, [message, reloadModelOptions, reloadProviders])
  const changeReasoning = useCallback((value: string) => {
    setReasoning(value)
    if (!reasoningPreferenceKey) return
    const preferences = { ...reasoningPreferencesRef.current, [reasoningPreferenceKey]: value }
    reasoningPreferencesRef.current = preferences
    setReasoningPreferences(preferences)
    void settingsRepository.setSetting(REASONING_STORAGE_KEY, JSON.stringify(preferences)).catch((error) => message.error(`思考级别保存失败:${String(error)}`))
  }, [message, reasoningPreferenceKey])

  return {
    activeModelLabel,
    addWorkspace,
    changeReasoning,
    chooseWorkspace,
    currentWorkspace,
    effectiveReasoning,
    modelOptions: optionGroups,
    reasoningIndex,
    reasoningLocked,
    reasoningMarks,
    reasoningNote: reasoningSpec?.note,
    reasoningPillLabel,
    reasoningStatus,
    reasoningSteps,
    removeWorkspace,
    selectedModel: defaultProvider ? `${defaultProvider.id}::${defaultProvider.model}` : undefined,
    showCompatibleModelNote: customGateway && !reasoningSpec,
    supportsImages,
    switchModel,
    workspaces,
  }
}
