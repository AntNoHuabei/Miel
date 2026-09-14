import { useCallback, useEffect, useRef, useState } from 'react'
import { App as AntApp } from 'antd'
import { useStore } from 'zustand'
import type { StoreApi } from 'zustand/vanilla'
import type { ChatAttachmentDraftLite } from '../../../api'
import { chatRepository } from '../../../shared/repositories'
import { useWailsEvent } from '../../../shared/wails/events'
import type { AgentEnvelope } from '../model/agentRun'
import type { ConversationState } from '../model/conversationStore'

interface RequestContext {
  reasoning: string
  workspacePath: string
  permissionSessionId: string
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
  const messages = useStore(options.store, (state) => state.messages)
  const input = useStore(options.store, (state) => state.input)
  const run = useStore(options.store, (state) => state.run)
  const setConversation = useStore(options.store, (state) => state.setConversation)
  const setMessages = useStore(options.store, (state) => state.setMessages)
  const setInput = useStore(options.store, (state) => state.setInput)
  const dispatchRun = useStore(options.store, (state) => state.dispatchRun)
  const [sending, setSending] = useState(false)
  const conversationRef = useRef(conversationId)
  const targetRef = useRef(0)
  const sendingRef = useRef(false)
  const activeRequestRef = useRef('')
  const inputSavedRef = useRef(false)
  const inputSavedConversationRef = useRef(0)
  const sessionVersionRef = useRef(0)
  const messageLoadVersionRef = useRef(0)
  const scrollRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => { conversationRef.current = conversationId }, [conversationId])

  const loadMessages = useCallback(async (id: number) => {
    const loadVersion = ++messageLoadVersionRef.current
    if (!id) {
      if (conversationRef.current === 0) setMessages([])
      return
    }
    try {
      const next = (await chatRepository.messagesSnapshot(id)).messages ?? []
      if (messageLoadVersionRef.current === loadVersion && conversationRef.current === id) setMessages(next)
    } catch {
      if (messageLoadVersionRef.current === loadVersion && conversationRef.current === id) setMessages([])
    }
  }, [setMessages])

  useEffect(() => { if (conversationRef.current > 0) void loadMessages(conversationRef.current) }, [loadMessages])
  useEffect(() => {
    requestAnimationFrame(() => {
      if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    })
  }, [messages, run.phase, run.process, run.streaming, run.tools])

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
    options.consumeAttachments()
  }, [dispatchRun, options.consumeAttachments, setConversation]))

  const reset = useCallback(() => {
    void options.beforeConversationChange?.()
    options.discardAttachments()
    sessionVersionRef.current += 1
    messageLoadVersionRef.current += 1
    activeRequestRef.current = ''
    targetRef.current = 0
    inputSavedRef.current = false
    inputSavedConversationRef.current = 0
    conversationRef.current = 0
    setConversation(0)
    setMessages([])
    setInput('')
    dispatchRun({ type: 'reset' })
    sendingRef.current = false
    setSending(false)
  }, [dispatchRun, options.beforeConversationChange, options.discardAttachments, setConversation, setInput, setMessages])

  const openConversation = useCallback((id: number) => {
    void options.beforeConversationChange?.()
    options.discardAttachments()
    sessionVersionRef.current += 1
    messageLoadVersionRef.current += 1
    activeRequestRef.current = ''
    inputSavedRef.current = false
    inputSavedConversationRef.current = 0
    conversationRef.current = id
    targetRef.current = id
    sendingRef.current = false
    setSending(false)
    setConversation(id)
    setMessages([])
    dispatchRun({ type: 'reset' })
    void loadMessages(id)
  }, [dispatchRun, loadMessages, options.beforeConversationChange, options.discardAttachments, setConversation, setMessages])

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
    inputSavedRef.current = false
    inputSavedConversationRef.current = 0
    activeRequestRef.current = id
    targetRef.current = requestConversationId
    setInput('')
    setMessages((items) => [...items, { id: pendingId, role: 'user', content: text, attachments: sentAttachments }])
    sendingRef.current = true
    setSending(true)
    dispatchRun({ type: 'start', requestId: id, conversationId: requestConversationId })
    let failed = false
    try {
      const context = await options.getRequestContext()
      if (sessionVersionRef.current !== version) return
      const result = await chatRepository.chat({
        conversationId: requestConversationId,
        message: text,
        reasoning: context.reasoning,
        requestId: id,
        attachmentIds: sentAttachments.map((item) => item.id),
        workspacePath: context.workspacePath,
        permissionSessionId: context.permissionSessionId,
      })
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
      failed = true
      dispatchRun({ type: 'fail', error: String(error) })
      if (!inputSavedRef.current) {
        setInput(text)
        setMessages((items) => items.filter((item) => item.id !== pendingId))
      } else if (inputSavedConversationRef.current > 0) {
        await loadMessages(inputSavedConversationRef.current)
        if (sessionVersionRef.current !== version) return
        await options.onConversationCompleted?.(inputSavedConversationRef.current)
      }
    } finally {
      if (sessionVersionRef.current === version) {
        sendingRef.current = false
        setSending(false)
        if (!failed) dispatchRun({ type: 'reset' })
      }
    }
  }, [dispatchRun, input, loadMessages, message, options, setConversation, setInput, setMessages])

  return { conversationId, dispatchRun, input, loadMessages, messages, openConversation, reset, run, scrollRef, send, sending, setInput }
}
