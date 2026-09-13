// 统一封装 wails bindings 服务与运行时事件,供页面组件引用。
// 服务随业务包移动到 internal/app,绑定镜像到对应子路径。
import * as Services from '../bindings/github.com/AntNoHuabei/blankmind/internal/app'
import { Events } from '@wailsio/runtime'
import { useEffect } from 'react'

export const AgentService = Services.AgentService
export const SettingsService = Services.SettingsService
export const DirectoryService = Services.DirectoryService
export const MemoryService = Services.MemoryService
export const TodoService = Services.TodoService
export const ScreenshotService = Services.ScreenshotService
export const ClipboardService = Services.ClipboardService
export const ChatAttachmentService = Services.ChatAttachmentService
export const WindowThemeService = Services.WindowThemeService
export { Events }

// 前端自用的轻量类型(与 Go 侧 json tag 对齐,不依赖 bindings JSDoc 推导)。
export interface ProviderTemplateLite {
  name: string
  kind: string
  baseUrl: string
  model: string
  multimodal: boolean
  docsUrl: string
}

export interface ProviderLite {
  id: number
  name: string
  kind: string
  baseUrl: string
  apiKey: string
  model: string
  multimodal: boolean
  isDefault: boolean
  createdAt: number
}

export interface ProviderInputLite {
  id?: number
  name: string
  kind: string
  baseUrl: string
  apiKey: string
  model: string
  multimodal: boolean
  isDefault: boolean
  models?: ProviderModelInputLite[]
}

export interface ProviderModelLite {
  model: string
  label: string
  custom: boolean
  multimodal: boolean
}

export interface ProviderModelInputLite {
  model: string
  label?: string
  custom?: boolean
  multimodal?: boolean
}

export interface DiscoveredModelLite {
  id: string
  status: string
  reasoning: ReasoningSpecLite
  multimodal: boolean
}

// 对话页模型切换下拉:provider × 启用模型 扁平项
export interface ModelOptionLite {
  providerId: number
  providerName: string
  kind: string
  model: string
  label: string
  custom: boolean
  multimodal: boolean
  isDefault: boolean
}

export interface WorkspaceLite {
  name: string
  path: string
  isCurrent: boolean
}

export interface TodoLite {
  id: number
  title: string
  description: string
  deadline: number
  isMilestone: boolean
  status: string
  source: string
  sourceId: number
  createdAt: number
  doneAt: number
}

export interface TodoStatsLite {
  total: number
  pending: number
  done: number
  milestones: number
  overdue: number
  dueSoon: number
}

export interface EventLite {
  id: number
  ts: number
  type: string
  summary: string
  refId: number
}

export interface ConversationLite {
  id: number
  title: string
  createdAt: number
  updatedAt: number
}

export interface AGUIToolCallLite {
  id: string
  type: string
  function: {
    name: string
    arguments: string
  }
}

export interface TodoSourceLite {
  id: number
  kind: 'clipboard_text' | 'clipboard_image' | 'screenshot' | 'conversation' | string
  textContent: string
  filePath: string
  mimeType: string
  dataUri: string
  conversationId: number
  messageId: number
  conversationTitle: string
  conversationAvailable: boolean
  screenshotId: number
  screenshotNote: string
  createdAt: number
  available: boolean
  error: string
}

export interface ExtractedTodoLite {
  title: string
  description: string
  milestone: boolean
  dueDate: string
}

export interface ClipboardTodoDraftLite {
  draftId: string
  kind: string
  text: string
  dataUri: string
  createdAt: number
  items: ExtractedTodoLite[]
}

export interface ChatMetricsLite {
  model: string
  promptTokens: number
  completionTokens: number
  totalTokens: number
  reasoningTokens: number
  cachedTokens: number
  durationMs: number
  firstTokenMs: number
  tokensPerSecond: number
}

export interface ChatAttachmentDraftLite {
  id: string
  name: string
  mimeType: string
  size: number
  width: number
  height: number
  thumbnailDataUri: string
}

export interface MessageAttachmentLite extends ChatAttachmentDraftLite {
  messageId: number
  kind: string
  position: number
  createdAt: number
}

export interface AGUIMessageLite {
  id: string
  role: string
  content?: unknown
  name?: string
  toolCalls?: AGUIToolCallLite[]
  toolCallId?: string
  error?: string
  activityType?: string
  metrics?: ChatMetricsLite
  attachments?: Array<ChatAttachmentDraftLite | MessageAttachmentLite>
}

export interface AGUIMessagesSnapshotLite {
  type: 'MESSAGES_SNAPSHOT' | string
  messages: AGUIMessageLite[]
}

export interface PingResultLite {
  ok: boolean
  message: string
  model: string
  latencyMs: number
}

export interface MemoryConfigLite {
  enabled: boolean
  autoExtract: boolean
  strategy: string
  customPrompt: string
}

export interface MemoryStatusLite {
  state: 'idle' | 'extracting' | 'error' | string
  pendingJobs: number
  lastSuccessAt: string
  lastError: string
}

export interface MemoryStrategyLite {
  id: string
  name: string
  description: string
  risk: string
  prompt: string
}

export interface MemorySettingsLite {
  config: MemoryConfigLite
  strategies: MemoryStrategyLite[]
  effectivePrompt: string
  currentModel: string
  status: MemoryStatusLite
}

export interface MemoryInputLite {
  content: string
  topics: string[]
  kind: 'fact' | 'episode'
  eventTime: string
  participants: string[]
  location: string
}

export interface MemoryItemLite extends MemoryInputLite {
  id: string
  createdAt: string
  updatedAt: string
}

// 内置模型目录(catalog.json,经 SettingsService.ModelCatalog 下发)
export interface ReasoningSpecLite {
  type: 'toggle' | 'effort' | 'always' | 'none' | string
  levels?: string[]
  note?: string
}

export interface CatalogModelLite {
  id: string
  label: string
  reasoning: ReasoningSpecLite
  multimodal: boolean
}

export interface CatalogProviderLite {
  kind: string
  name: string
  baseUrl: string
  models: CatalogModelLite[]
}

/**
 * 订阅 wails 事件;组件卸载时自动取消。
 * payload 形如 { data: T }。
 */
export function useWailsEvent<T = unknown>(
  name: string,
  handler: (data: T) => void,
  deps: unknown[] = [],
) {
  useEffect(() => {
    const off = Events.On(name, (payload: { data: T }) => {
      handler(payload.data)
    }) as unknown
    return () => {
      if (typeof off === 'function') {
        ;(off as () => void)()
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)
}
