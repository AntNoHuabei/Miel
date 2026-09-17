export interface ConversationLite { id: number; title: string; providerId: number; model: string; createdAt: number; updatedAt: number }
export interface PlanLite { id: number; revision: number; currentRevision: number; status: string; generatedModel: string; executionModel?: string }
export interface AGUIToolCallLite { id: string; type: string; function: { name: string; arguments: string } }
export interface ChatMetricsLite { model: string; promptTokens: number; completionTokens: number; totalTokens: number; reasoningTokens: number; cachedTokens: number; durationMs: number; firstTokenMs: number; tokensPerSecond: number }
export interface ChatAttachmentDraftLite { id: string; name: string; mimeType: string; size: number; width: number; height: number; thumbnailDataUri: string }
export interface MessageAttachmentLite extends ChatAttachmentDraftLite { messageId: number; kind: string; position: number; createdAt: number }
export interface ChatRunErrorLite { code: string; message: string }
import type { ArtifactRefLite } from './artifacts'
export interface AGUIMessageLite { id: string; role: string; content?: unknown; name?: string; toolCalls?: AGUIToolCallLite[]; toolCallId?: string; error?: string; runError?: ChatRunErrorLite; activityType?: string; messageType?: string; plan?: PlanLite; metrics?: ChatMetricsLite; attachments?: Array<ChatAttachmentDraftLite | MessageAttachmentLite>; artifacts?: ArtifactRefLite[] }
export interface AGUIMessagesSnapshotLite { type: 'MESSAGES_SNAPSHOT' | string; messages: AGUIMessageLite[] }
