import { useCallback, useEffect, useRef, useState } from 'react'
import type { UIEvent } from 'react'
import { App as AntApp } from 'antd'
import { useStore } from 'zustand'
import type { StoreApi } from 'zustand/vanilla'
import type { ChatAttachmentDraftLite } from '../../../api'
import { chatRepository } from '../../../shared/repositories'
import { useWailsEvent } from '../../../shared/wails/events'
import type { AgentEnvelope } from '../model/agentRun'
import type { SkillInstallProgressLite } from '../../../api'
import type { ConversationState } from '../model/conversationStore'
import type { PlanLite } from '../../../shared/types/chat'
import type { AgentProfile } from '../../../shared/types/chat'

interface RequestContext {
  reasoning: string
  workspacePath: string
  permissionSessionId: string
  mode?: 'chat' | 'plan'
  agentProfile: AgentProfile
}

interface ConversationRuntimeOptions {
  store: StoreApi<ConversationState>
  attachments: ChatAttachmentDraftLite[]
  supportsImages: boolean
  enabled?: boolean
  requestPrefix: string
  unsupportedImagesMessage: string
  consumeAttachments: () => void
  discardAttachments: () => void
  beforeConversationChange?: () => void | Promise<void>
  getRequestContext: () => RequestContext | Promise<RequestContext>
  onConversationCompleted?: (conversationId: number) => void | Promise<void>
}

interface AgentInputSaved {
  conversationId: number
  messageId: number
  requestId?: string
}

function requestId(prefix: string) {
  return typeof crypto !== 'undefined' && crypto.randomUUID
    ? crypto.randomUUID()
    : `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export function useConversationRuntime(options: ConversationRuntimeOptions) {
  const { message } = AntApp.useApp()
  const conversationId = useStore(options.store, (state) => state.conversationId)
  const timeline = useStore(options.store, (state) => state.timeline)
  const input = useStore(options.store, (state) => state.input)
  const run = useStore(options.store, (state) => state.run)
  const setConversation = useStore(options.store, (state) => state.setConversation)
  const setTimeline = useStore(options.store, (state) => state.setTimeline)
  const setInput = useStore(options.store, (state) => state.setInput)
  const dispatchRun = useStore(options.store, (state) => state.dispatchRun)
  const [sending, setSending] = useState(false)
  const conversationRef = useRef(conversationId)
  const targetRef = useRef(0)
  const sendingRef = useRef(false)
  const activeRequestRef = useRef('')
  const activeCallRef = useRef<ReturnType<typeof chatRepository.chat> | null>(null)
  const stopRequestedRef = useRef(false)
  const inputSavedRef = useRef(false)
  const inputSavedConversationRef = useRef(0)
  const consumeAttachmentsOnSaveRef = useRef(false)
  const sessionVersionRef = useRef(0)
  const messageLoadVersionRef = useRef(0)
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const followOutputRef = useRef(true)

  useEffect(() => { conversationRef.current = conversationId }, [conversationId])

  const loadMessages = useCallback(async (id: number) => {
    const loadVersion = ++messageLoadVersionRef.current
    if (!id) {
      if (conversationRef.current === 0) setTimeline([])
      return
    }
    try {
      const next = (await chatRepository.messagesSnapshot(id)).timeline ?? []
      if (messageLoadVersionRef.current === loadVersion && conversationRef.current === id) setTimeline(next)
    } catch {
      if (messageLoadVersionRef.current === loadVersion && conversationRef.current === id) setTimeline([])
    }
  }, [setTimeline])

  useEffect(() => { if (conversationRef.current > 0) void loadMessages(conversationRef.current) }, [loadMessages])
  useEffect(() => {
    requestAnimationFrame(() => {
      if (followOutputRef.current && scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    })
  }, [timeline, run.artifacts, run.phase, run.process, run.streaming, run.tools])

  const onMessagesScroll = useCallback((event: UIEvent<HTMLDivElement>) => {
    const viewport = event.currentTarget
    followOutputRef.current = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight <= 48
  }, [])

  useWailsEvent<string>('artifacts.changed', useCallback(() => {
    if (!sendingRef.current && conversationRef.current > 0) void loadMessages(conversationRef.current)
  }, [loadMessages]))

  const accepts = useCallback((payload: { conversationId: number; requestId?: string }) => {
    if (!sendingRef.current) return false
    if (payload.requestId && payload.requestId !== activeRequestRef.current) return false
    if (targetRef.current === 0) targetRef.current = payload.conversationId
    return payload.conversationId === targetRef.current
  }, [])

  useWailsEvent<AgentEnvelope>('agent.chunk', useCallback((payload) => {
    if (accepts(payload)) dispatchRun({ type: 'chunk', payload })
  }, [accepts, dispatchRun]))
  useWailsEvent<AgentEnvelope>('agent.agui', useCallback((payload) => {
    if (payload?.event && accepts(payload)) dispatchRun({ type: 'event', payload })
  }, [accepts, dispatchRun]))
  useWailsEvent<SkillInstallProgressLite>('skill.dependency.progress', useCallback((payload) => {
    if (sendingRef.current) dispatchRun({ type: 'skill-progress', payload })
  }, [dispatchRun]))
  useWailsEvent<{ conversationId: number; requestId?: string }>('agent.start', useCallback((payload) => {
    if (accepts(payload)) dispatchRun({ type: 'event', payload: { ...payload, event: { type: 'RUN_STARTED' } } })
  }, [accepts, dispatchRun]))
  useWailsEvent<AgentInputSaved>('agent.input.saved', useCallback((payload) => {
    if (!sendingRef.current || payload.requestId !== activeRequestRef.current) return
    inputSavedRef.current = true
    inputSavedConversationRef.current = payload.conversationId
    conversationRef.current = payload.conversationId
    targetRef.current = payload.conversationId
    setConversation(payload.conversationId)
    dispatchRun({ type: 'input-saved', payload })
    if (consumeAttachmentsOnSaveRef.current) options.consumeAttachments()
  }, [dispatchRun, options.consumeAttachments, setConversation]))

  const cancelActiveRequest = useCallback((reportFailure: boolean) => {
    const requestId = activeRequestRef.current
    if (!sendingRef.current || !requestId) return
    stopRequestedRef.current = true
    void activeCallRef.current?.cancel?.()
    void Promise.resolve(chatRepository.cancelChat({
      conversationId: targetRef.current || conversationRef.current,
      requestId,
    })).then((accepted) => {
      if (!accepted && reportFailure) message.error('请求未处于可取消状态')
    }).catch((error) => {
      if (reportFailure) message.error(`停止生成失败：${String(error)}`)
    })
  }, [message])

  const reset = useCallback(() => {
    void options.beforeConversationChange?.()
    options.discardAttachments()
    cancelActiveRequest(false)
    sessionVersionRef.current += 1
    messageLoadVersionRef.current += 1
    activeRequestRef.current = ''
    activeCallRef.current = null
    stopRequestedRef.current = false
    targetRef.current = 0
    inputSavedRef.current = false
    inputSavedConversationRef.current = 0
    consumeAttachmentsOnSaveRef.current = false
    conversationRef.current = 0
    followOutputRef.current = true
    setConversation(0)
    setTimeline([])
    setInput('')
    dispatchRun({ type: 'reset' })
    sendingRef.current = false
    setSending(false)
  }, [cancelActiveRequest, dispatchRun, options.beforeConversationChange, options.discardAttachments, setConversation, setInput, setTimeline])

  const openConversation = useCallback((id: number) => {
    void options.beforeConversationChange?.()
    options.discardAttachments()
    cancelActiveRequest(false)
    sessionVersionRef.current += 1
    messageLoadVersionRef.current += 1
    activeRequestRef.current = ''
    activeCallRef.current = null
    stopRequestedRef.current = false
    inputSavedRef.current = false
    inputSavedConversationRef.current = 0
    consumeAttachmentsOnSaveRef.current = false
    conversationRef.current = id
    followOutputRef.current = true
    targetRef.current = id
    sendingRef.current = false
    setSending(false)
    setConversation(id)
    setTimeline([])
    dispatchRun({ type: 'reset' })
    void loadMessages(id)
  }, [cancelActiveRequest, dispatchRun, loadMessages, options.beforeConversationChange, options.discardAttachments, setConversation, setTimeline])

  const send = useCallback(async () => {
    const text = input.trim()
    if ((!text && options.attachments.length === 0) || sendingRef.current || options.enabled === false) return
    if (options.attachments.length > 0 && !options.supportsImages) {
      message.error(options.unsupportedImagesMessage)
      return
    }
    const sentAttachments = options.attachments
    const pendingId = `pending-${Date.now()}`
    const version = sessionVersionRef.current
    const requestConversationId = conversationRef.current
    const id = requestId(options.requestPrefix)
    followOutputRef.current = true
    inputSavedRef.current = false
    inputSavedConversationRef.current = 0
    consumeAttachmentsOnSaveRef.current = true
    activeRequestRef.current = id
    activeCallRef.current = null
    stopRequestedRef.current = false
    targetRef.current = requestConversationId
    setInput('')
    setTimeline((items) => [...items, { kind: 'message', sequence: items.length + 1, message: { id: pendingId, role: 'user', content: text, attachments: sentAttachments } }])
    sendingRef.current = true
    setSending(true)
    dispatchRun({ type: 'start', requestId: id, conversationId: requestConversationId })
    let failed = false
    try {
      const context = await options.getRequestContext()
      if (sessionVersionRef.current !== version) return
      const call = chatRepository.chat({
        conversationId: requestConversationId,
        message: text,
        reasoning: context.reasoning,
        requestId: id,
        attachmentIds: sentAttachments.map((item) => item.id),
        workspacePath: context.workspacePath,
        permissionSessionId: context.permissionSessionId,
        mode: context.mode ?? 'chat',
        agentProfile: context.agentProfile,
        planId: 0,
        planRevision: 0,
      })
      activeCallRef.current = call
      const result = await call
      if (sessionVersionRef.current !== version) return
      options.consumeAttachments()
      conversationRef.current = result.conversationId
      targetRef.current = result.conversationId
      setConversation(result.conversationId)
      await loadMessages(result.conversationId)
      if (sessionVersionRef.current !== version) return
      dispatchRun({ type: 'complete', conversationId: result.conversationId })
      await options.onConversationCompleted?.(result.conversationId)
    } catch (error) {
      if (sessionVersionRef.current !== version) return
      if (stopRequestedRef.current && activeRequestRef.current === id) {
        if (inputSavedConversationRef.current > 0) await loadMessages(inputSavedConversationRef.current)
        return
      }
      failed = true
      dispatchRun({ type: 'fail', error: String(error) })
      if (!inputSavedRef.current) {
        setInput(text)
        setTimeline((items) => items.filter((item) => item.kind !== 'message' || item.message.id !== pendingId))
      } else if (inputSavedConversationRef.current > 0) {
        await loadMessages(inputSavedConversationRef.current)
        if (sessionVersionRef.current !== version) return
        await options.onConversationCompleted?.(inputSavedConversationRef.current)
      }
    } finally {
      if (sessionVersionRef.current === version) {
        if (activeRequestRef.current === id) activeCallRef.current = null
        sendingRef.current = false
        setSending(false)
        if (!failed) dispatchRun({ type: 'reset' })
      }
    }
  }, [dispatchRun, input, loadMessages, message, options, setConversation, setInput, setTimeline])

  const stop = useCallback(async () => { cancelActiveRequest(true) }, [cancelActiveRequest])

  const runPlanAction = useCallback(async (action: 'revise' | 'execute', plan: PlanLite, instruction = '') => {
    if (sendingRef.current) return
    if (action === 'revise' && options.enabled === false) {
      message.error('当前模型尚未就绪，请稍后重试')
      return
    }
    const version = sessionVersionRef.current
    const requestConversationId = conversationRef.current
    const id = requestId(`plan-${action}`)
    followOutputRef.current = true
    inputSavedRef.current = false
    inputSavedConversationRef.current = 0
    consumeAttachmentsOnSaveRef.current = false
    activeRequestRef.current = id
    activeCallRef.current = null
    stopRequestedRef.current = false
    targetRef.current = requestConversationId
    sendingRef.current = true
    setSending(true)
    dispatchRun({ type: 'start', requestId: id, conversationId: requestConversationId })
    if (action === 'execute') {
      setTimeline((items) => [
        ...items.map((item) => item.kind === 'plan' && item.plan.id === plan.id && item.plan.revision === plan.revision
          ? { ...item, plan: { ...item.plan, status: 'executing' } }
          : item),
        {
          kind: 'plan_execution' as const,
          sequence: items.length + 1,
          execution: {
            id: `pending-${id}`,
            messageId: `pending-${id}`,
            content: `执行已批准计划 v${plan.revision}`,
            planId: plan.id,
            revision: plan.revision,
            createdAt: Math.floor(Date.now() / 1000),
          },
        },
      ])
    }
    let failed = false
    try {
      const context = await options.getRequestContext()
      if (sessionVersionRef.current !== version) return
      const request = {
        planId: plan.id,
        revision: plan.revision,
        instruction,
        reasoning: context.reasoning,
        requestId: id,
        workspacePath: context.workspacePath,
        permissionSessionId: context.permissionSessionId,
        agentProfile: action === 'execute' ? (plan.agentProfile ?? context.agentProfile) : context.agentProfile,
      }
      const call = action === 'execute'
        ? chatRepository.executePlan(request)
        : chatRepository.revisePlan(request)
      activeCallRef.current = call
      const result = await call
      if (sessionVersionRef.current !== version) return
      conversationRef.current = result.conversationId
      targetRef.current = result.conversationId
      setConversation(result.conversationId)
      await loadMessages(result.conversationId)
      if (sessionVersionRef.current !== version) return
      dispatchRun({ type: 'complete', conversationId: result.conversationId })
      await options.onConversationCompleted?.(result.conversationId)
    } catch (error) {
      if (sessionVersionRef.current !== version) return
      if (stopRequestedRef.current && activeRequestRef.current === id) {
        await loadMessages(requestConversationId)
        return
      }
      failed = true
      dispatchRun({ type: 'fail', error: String(error) })
      await loadMessages(requestConversationId)
      if (sessionVersionRef.current === version) await options.onConversationCompleted?.(requestConversationId)
    } finally {
      if (sessionVersionRef.current === version) {
        if (activeRequestRef.current === id) activeCallRef.current = null
        sendingRef.current = false
        setSending(false)
        if (!failed) dispatchRun({ type: 'reset' })
      }
    }
  }, [dispatchRun, loadMessages, options, setConversation, setTimeline])

  const abandonPlan = useCallback(async (plan: PlanLite) => {
    if (sendingRef.current || conversationRef.current <= 0) return
    try {
      await chatRepository.abandonPlan({
        planId: plan.id,
        revision: plan.revision,
        instruction: '',
        reasoning: '',
        requestId: requestId('plan-abandon'),
        workspacePath: '',
        permissionSessionId: '',
        agentProfile: plan.agentProfile ?? 'work',
      })
      await loadMessages(conversationRef.current)
      await options.onConversationCompleted?.(conversationRef.current)
    } catch (error) {
      message.error(`废弃计划失败：${String(error)}`)
    }
  }, [loadMessages, message, options])

  return { abandonPlan, conversationId, dispatchRun, input, loadMessages, timeline, onMessagesScroll, openConversation, reset, run, runPlanAction, scrollRef, send, sending, setInput, stop }
}
