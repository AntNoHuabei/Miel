import type { ApprovalRequest, PermissionMode } from './permissionTypes'

export const PERMISSION_MODES: PermissionMode[] = ['ask', 'auto', 'full']

export const PERMISSION_LABELS: Record<PermissionMode, string> = {
  ask: '询问批准',
  auto: '为我批准',
  full: '完全访问',
}

export const PERMISSION_NOTES: Record<PermissionMode, string> = {
  ask: '每次使用文件、命令或网络工具前询问',
  auto: '工作区内和低风险工具自动执行，工作区外文件仍询问',
  full: '文件、命令和网络工具自动执行，保留审计记录',
}

const PERMISSION_TOOL_LABELS: Record<string, string> = {
  list_directory: '列出目录',
  read_file: '读取文件',
  write_file: '写入文件',
  execute_command: '执行命令',
  fetch_url: '访问网络',
}

const OPERATION_LABELS: Record<string, string> = {
  list: '列出目录',
  read: '读取',
  write: '写入',
  execute: '执行',
  fetch: '访问',
}

const RISK_LABELS: Record<string, string> = {
  low: '低风险',
  medium: '中风险',
  high: '高风险',
}

export function getPermissionToolLabel(tool: string) {
  return PERMISSION_TOOL_LABELS[tool] ?? tool
}

export function formatApprovalOperation(request: ApprovalRequest) {
  const operation = OPERATION_LABELS[request.operation] ?? request.operation
  return request.outsideWorkspace ? `${operation} · 工作区外` : operation
}

export function formatApprovalRisk(riskLevel: string) {
  return RISK_LABELS[riskLevel] ?? '未知风险'
}

export function formatApprovalTarget(request: ApprovalRequest) {
  return request.target.trim() || request.scopeRoot.trim() || '未提供目标'
}

export function normalizePermissionMode(mode: string | undefined): PermissionMode {
  return mode === 'auto' || mode === 'full' ? mode : 'ask'
}
