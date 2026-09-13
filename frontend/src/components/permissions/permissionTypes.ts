export type PermissionMode = 'ask' | 'auto' | 'full'

export type ApprovalDecision = 'once' | 'session' | 'deny'

export interface ApprovalRequest {
  id: string
  sessionId: string
  tool: string
  operation: string
  workspacePath: string
  target: string
  riskLevel: string
  outsideWorkspace: boolean
  scopeRoot: string
  expiresAt: string
}

export interface PermissionState {
  mode: PermissionMode
  pending: ApprovalRequest[]
}

export interface AgentPermissionContext {
  mode: PermissionMode
  pendingApproval: ApprovalRequest | null
  sessionId: string
  resolving: boolean
  changeMode: (mode: PermissionMode) => Promise<void>
  resolveApproval: (decision: ApprovalDecision) => Promise<void>
  resetSession: () => Promise<void>
}
