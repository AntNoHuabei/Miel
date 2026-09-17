import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { App as AntApp } from 'antd'
import type { CatalogProviderLite, DiscoveredModelLite, ModelOptionLite, ProviderLite, ReasoningSpecLite, WorkspaceLite } from '../../../api'
import type { AgentProfile, ProfileModelLite } from '../../../shared/types/chat'
import type { AgentProfileDefaultsLite } from '../../../shared/types/settings'
import { chatRepository, settingsRepository } from '../../../shared/repositories'
import { useWailsEvent } from '../../../shared/wails/events'

const REASONING_STORAGE_KEY = 'chat.reasoning.v1'
const LEVEL_LABEL: Record<string, string> = { low: '低', medium: '中', high: '高', xhigh: '极高', max: '最高' }

export function chatModelLabel(option: ModelOptionLite, discovered?: DiscoveredModelLite) {
  const label = option.custom ? `${option.model} (自定义)` : option.label || option.model
  return option.kind.toLowerCase() === 'openrouter' && discovered?.supportsTools === false
    ? `${label} (纯聊天)`
    : label
}

export function reasoningStepsFor(
  spec: ReasoningSpecLite | undefined,
  providerKind: string | undefined,
) {
  const kind = providerKind?.toLowerCase()
  const specType = spec?.type ?? 'none'
  const customGateway = kind === 'custom'
  const herdsman = kind === 'herdsman'
  const usesProviderDefault = kind === 'openrouter' || kind === 'volcengine-plan'
  const canDisable = specType === 'toggle' || (specType === 'effort' && kind === 'deepseek')
  if (specType === 'effort') {
    const levels = (spec?.levels ?? []).filter((level) => LEVEL_LABEL[level])
    if (herdsman) return ['', 'off', ...levels]
    if (usesProviderDefault) return ['', ...levels]
    return canDisable ? ['', ...levels] : levels
  }
  if (specType === 'toggle') return herdsman ? ['', 'off', 'on'] : ['', 'on']
  if (customGateway) return ['', 'low', 'medium', 'high']
  return []
}

export function useChatControls(conversationId = 0) {
  const { message } = AntApp.useApp()
  const [providers, setProviders] = useState<ProviderLite[]>([])
  const [modelOptions, setModelOptions] = useState<ModelOptionLite[]>([])
  const [catalog, setCatalog] = useState<CatalogProviderLite[]>([])
  const [discoveredModels, setDiscoveredModels] = useState<Record<number, DiscoveredModelLite[]>>({})
  const [workspaces, setWorkspaces] = useState<WorkspaceLite[]>([])
  const [reasoning, setReasoning] = useState('')
  const [reasoningPreferences, setReasoningPreferences] = useState<Record<string, string>>({})
  const [agentProfile, setAgentProfile] = useState<AgentProfile>('work')
  const [profileModels, setProfileModels] = useState<Partial<Record<AgentProfile, ProfileModelLite>>>({})
  const [profileDefaults, setProfileDefaults] = useState<AgentProfileDefaultsLite | null>(null)
  const [loadedConversationId, setLoadedConversationId] = useState(0)
  const reasoningPreferencesRef = useRef<Record<string, string>>({})
  const conversationRef = useRef(conversationId)
  const profileLoadVersionRef = useRef(0)
  const pendingProfileRef = useRef<{ conversationId: number; profile: AgentProfile } | null>(null)

  useEffect(() => {
    conversationRef.current = conversationId
    profileLoadVersionRef.current += 1
    pendingProfileRef.current = null
  }, [conversationId])

  const defaultProvider = providers.find((provider) => provider.isDefault) ?? providers[0]
  const profileModel = conversationId > 0 ? profileModels[agentProfile] : profileDefaults?.[agentProfile]
  const activeProvider = profileModel
    ? providers.find((provider) => provider.id === profileModel.providerId)
    : defaultProvider
  const activeProviderModel = profileModel?.model || activeProvider?.model
  const optionGroups = useMemo(() => {
    const groups = new Map<string, Array<{ value: string; label: string }>>()
    for (const option of modelOptions) {
      if (!groups.has(option.providerName)) groups.set(option.providerName, [])
      const discovered = discoveredModels[option.providerId]?.find((model) => model.id.toLowerCase() === option.model.toLowerCase())
      groups.get(option.providerName)?.push({
        value: `${option.providerId}::${option.model}`,
        label: chatModelLabel(option, discovered),
      })
    }
    return Array.from(groups, ([label, options]) => ({ label, options }))
  }, [discoveredModels, modelOptions])

  const catalogProvider = catalog.find((provider) => provider.kind.toLowerCase() === activeProvider?.kind.toLowerCase())
  const catalogModel = catalogProvider?.models.find((model) => model.id.toLowerCase() === activeProviderModel?.toLowerCase())
  const discoveredModel = activeProvider
    ? discoveredModels[activeProvider.id]?.find((model) => model.id.toLowerCase() === activeProviderModel?.toLowerCase())
    : undefined
  const reasoningSpec = discoveredModel?.reasoning ?? catalogModel?.reasoning
  const supportsImages = discoveredModel?.multimodal ?? catalogModel?.multimodal ?? defaultProvider?.multimodal ?? false
  const specType = reasoningSpec?.type ?? 'none'
  const customGateway = activeProvider?.kind === 'custom'
  const herdsman = activeProvider?.kind === 'herdsman'
  const openRouter = activeProvider?.kind === 'openrouter'
  const volcenginePlan = activeProvider?.kind === 'volcengine-plan'
  const compatibleGateway = customGateway || herdsman || openRouter || volcenginePlan
  const reasoningSteps = useMemo(() => {
    return reasoningStepsFor(reasoningSpec, activeProvider?.kind)
  }, [reasoningSpec, activeProvider?.kind])
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
  const activeModel = modelOptions.find((option) => option.providerId === activeProvider?.id && option.model === activeProviderModel)
  const profileReady = conversationId === 0 || loadedConversationId === conversationId
  const modelAvailable = profileReady && !!activeProvider && !!activeProviderModel && !!activeModel
  const activeModelLabel = activeModel ? chatModelLabel(activeModel, discoveredModel) : profileModel ? '需要重新选择模型' : activeProviderModel || '未配置模型'
  const reasoningStatus = reasoningLocked ? (specType === 'always' ? '思考常开' : specType === 'none' ? '不支持思考' : '不可调节') : reasoningMarks[reasoningIndex] ?? '关闭'
  const reasoningPillLabel = reasoningLocked ? (specType === 'always' ? '常开' : '') : reasoningMarks[reasoningIndex] ?? '关闭'
  const reasoningPreferenceKey = activeProvider && activeProviderModel ? `${activeProvider.id}::${activeProviderModel}` : ''
  const currentWorkspace = workspaces.find((workspace) => workspace.isCurrent)

  const reloadProviders = useCallback(async () => {
    try {
      const nextProviders = await settingsRepository.listProviders()
      setProviders(nextProviders)
      const entries = await Promise.all(nextProviders.filter((provider) => ['herdsman', 'openrouter'].includes(provider.kind.toLowerCase())).map(async (provider) => {
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
  const reloadConversationModel = useCallback(async () => {
    const targetConversationId = conversationId
    const loadVersion = ++profileLoadVersionRef.current
    if (conversationId <= 0) {
      if (conversationRef.current !== targetConversationId || profileLoadVersionRef.current !== loadVersion) return
      setAgentProfile('work')
      setProfileModels({})
      setLoadedConversationId(0)
      return
    }
    try {
      const conversations = await chatRepository.listConversations()
      if (conversationRef.current !== targetConversationId || profileLoadVersionRef.current !== loadVersion) return
      const current = conversations.find((item) => item.id === targetConversationId)
      if (current) {
        const loadedProfile = current.agentProfile === 'coding' ? 'coding' : 'work'
        const pending = pendingProfileRef.current
        if (!pending || pending.conversationId !== targetConversationId || pending.profile === loadedProfile) {
          setAgentProfile(loadedProfile)
          if (pending?.conversationId === targetConversationId) pendingProfileRef.current = null
        }
        setProfileModels(current.profileModels ?? {})
        setLoadedConversationId(targetConversationId)
      }
    } catch { /* Keep the last known model while the conversation list reloads. */ }
  }, [conversationId])

  useEffect(() => {
    void reloadProviders()
    void reloadModelOptions()
    void reloadWorkspaces()
    settingsRepository.agentProfileDefaults().then(setProfileDefaults).catch(() => undefined)
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

  useEffect(() => { void reloadConversationModel() }, [reloadConversationModel])

  useWailsEvent<string>('models.changed', useCallback(() => {
    void reloadProviders()
    void reloadModelOptions()
    settingsRepository.agentProfileDefaults().then(setProfileDefaults).catch(() => undefined)
  }, [reloadProviders, reloadModelOptions]))
  useWailsEvent<string>('conversations.changed', useCallback(() => {
    void reloadConversationModel()
  }, [reloadConversationModel]))

  const modelKey = `${activeProvider?.kind ?? ''}:${activeProviderModel ?? ''}`
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
      if (conversationId > 0) {
        await chatRepository.setConversationModel({ conversationId, profile: agentProfile, providerId, model })
        setProfileModels((current) => ({ ...current, [agentProfile]: { providerId, model } }))
      } else {
        await settingsRepository.setAgentProfileDefault(agentProfile, providerId, model)
        setProfileDefaults((current) => current ? { ...current, [agentProfile]: { providerId, model } } : current)
      }
      message.success(`已切换到 ${model}`)
      await Promise.all([reloadProviders(), reloadModelOptions()])
    } catch (error) { message.error(`切换失败:${String(error)}`) }
  }, [agentProfile, conversationId, message, reloadModelOptions, reloadProviders])

  const switchProfile = useCallback(async (profile: AgentProfile) => {
    const targetConversationId = conversationId
    try {
      if (targetConversationId > 0) {
        pendingProfileRef.current = { conversationId: targetConversationId, profile }
        profileLoadVersionRef.current += 1
        await chatRepository.setConversationProfile({ conversationId: targetConversationId, profile })
      }
      setAgentProfile(profile)
      message.success(profile === 'coding' ? '已切换到 Coding' : '已切换到 Work')
      return true
    } catch (error) {
      if (pendingProfileRef.current?.conversationId === targetConversationId) pendingProfileRef.current = null
      message.error(`切换模式失败:${String(error)}`)
      return false
    }
  }, [conversationId, message])

  const resetProfile = useCallback(() => setAgentProfile('work'), [])
  const changeReasoning = useCallback((value: string) => {
    setReasoning(value)
    if (!reasoningPreferenceKey) return
    const preferences = { ...reasoningPreferencesRef.current, [reasoningPreferenceKey]: value }
    reasoningPreferencesRef.current = preferences
    setReasoningPreferences(preferences)
    void settingsRepository.setSetting(REASONING_STORAGE_KEY, JSON.stringify(preferences)).catch((error) => message.error(`思考级别保存失败:${String(error)}`))
  }, [message, reasoningPreferenceKey])

  return {
    agentProfile,
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
    reasoningPillLabel,
    reasoningStatus,
    reasoningSteps,
    removeWorkspace,
    modelAvailable,
    profileReady,
    selectedModel: modelAvailable ? `${activeProvider.id}::${activeProviderModel}` : undefined,
    showCompatibleModelNote: customGateway && !reasoningSpec,
    supportsImages: modelAvailable && supportsImages,
    switchProfile,
    switchModel,
    resetProfile,
    workspaces,
  }
}
