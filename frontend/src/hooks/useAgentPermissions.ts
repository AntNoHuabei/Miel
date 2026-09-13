import { useCallback, useEffect, useRef, useState } from 'react'
import { App as AntApp } from 'antd'
import { permissionRepository } from '../shared/repositories'
import { useWailsEvent } from '../shared/wails/events'
import type {
  AgentPermissionContext,
  ApprovalDecision,
  ApprovalRequest,
  PermissionMode,
  PermissionState,
} from '../components/permissions'
import { normalizePermissionMode } from '../components/permissions/permissionUtils'

function createSessionId() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return `chat-${crypto.randomUUID()}`
  return `chat-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export function useAgentPermissions(): AgentPermissionContext {
  const { message } = AntApp.useApp()
  const [mode, setMode] = useState<PermissionMode>('ask')
  const [pendingApproval, setPendingApproval] = useState<ApprovalRequest | null>(null)
  const [sessionId, setSessionId] = useState(createSessionId)
  const [resolving, setResolving] = useState(false)
  const sessionIdRef = useRef(sessionId)
  const pendingApprovalRef = useRef<ApprovalRequest | null>(null)
  const resolvingRef = useRef(false)

  const updatePendingApproval = useCallback((request: ApprovalRequest | null) => {
    pendingApprovalRef.current = request
    setPendingApproval(request)
  }, [])

  useEffect(() => {
    let active = true
    permissionRepository.getState()
      .then((state) => {
        if (!active) return
        const value = state as PermissionState
        setMode(normalizePermissionMode(value?.mode))
        updatePendingApproval(
          (value?.pending ?? []).find((item) => item.sessionId === sessionIdRef.current) ?? null,
        )
      })
      .catch(() => {
        if (active) setMode('ask')
      })
    return () => {
      active = false
      void permissionRepository.cancelPending(sessionIdRef.current)
    }
  }, [updatePendingApproval])

  useWailsEvent<ApprovalRequest>(
    'permission.requested',
    useCallback((request) => {
      if (request?.sessionId === sessionIdRef.current) updatePendingApproval(request)
    }, [updatePendingApproval]),
  )

  useWailsEvent<{ id?: string } | ApprovalRequest>(
    'permission.resolved',
    useCallback((payload) => {
      if (payload?.id && pendingApprovalRef.current?.id === payload.id) updatePendingApproval(null)
    }, [updatePendingApproval]),
  )

  const clearFinishedApproval = useCallback((request: ApprovalRequest) => {
    if (request?.id && pendingApprovalRef.current?.id === request.id) updatePendingApproval(null)
  }, [updatePendingApproval])

  useWailsEvent<ApprovalRequest>('permission.expired', clearFinishedApproval, [clearFinishedApproval])
  useWailsEvent<ApprovalRequest>('permission.cancelled', clearFinishedApproval, [clearFinishedApproval])

  useWailsEvent<PermissionState>(
    'permission.changed',
    useCallback((state) => {
      if (state?.mode) setMode(normalizePermissionMode(state.mode))
    }, []),
  )

  const changeMode = useCallback(async (nextMode: PermissionMode) => {
    try {
      await permissionRepository.setMode(nextMode)
      setMode(nextMode)
    } catch (error) {
      message.error(`权限设置失败:${String(error)}`)
    }
  }, [message])

  const resolveApproval = useCallback(async (decision: ApprovalDecision) => {
    const request = pendingApprovalRef.current
    if (!request || resolvingRef.current) return
    resolvingRef.current = true
    setResolving(true)
    try {
      await permissionRepository.resolve(request.id, decision)
      if (pendingApprovalRef.current?.id === request.id) updatePendingApproval(null)
    } catch (error) {
      message.error(`审批处理失败:${String(error)}`)
    } finally {
      resolvingRef.current = false
      setResolving(false)
    }
  }, [message, updatePendingApproval])

  const resetSession = useCallback(async () => {
    const previousSessionId = sessionIdRef.current
    const nextSessionId = createSessionId()
    sessionIdRef.current = nextSessionId
    setSessionId(nextSessionId)
    resolvingRef.current = false
    setResolving(false)
    updatePendingApproval(null)
    try {
      await permissionRepository.cancelPending(previousSessionId)
    } catch (error) {
      message.error(`清理旧审批失败:${String(error)}`)
    }
  }, [message, updatePendingApproval])

  return {
    mode,
    pendingApproval,
    sessionId,
    resolving,
    changeMode,
    resolveApproval,
    resetSession,
  }
}
