export type AgentProfile = 'work' | 'coding'
export interface ProfileModelLite { providerId: number; model: string }
export interface ConversationLite { id: number; title: string; providerId: number; model: string; agentProfile: AgentProfile; profileModels: Record<AgentProfile, ProfileModelLite>; createdAt: number; updatedAt: number }
export interface PlanViewLite { id: number; messageId: string; revision: number; currentRevision: number; status: string; content: string; generatedModel: string; executionModel?: string; agentProfile?: AgentProfile; createdAt: number }
export type PlanLite = PlanViewLite
export interface PlanRequestViewLite { id: string; messageId: string; content: unknown; planId: number; revision: number; createdAt: number }
export interface PlanRunViewLite { id: number; planId: number; revision: number; status: string; model: string; error?: string; startedAt: number; completedAt?: number }
export interface PlanExecutionViewLite { id: string; messageId: string; content: unknown; planId: number; revision: number; createdAt: number; run?: PlanRunViewLite }
export interface AGUIToolCallLite { id: string; type: string; function: { name: string; arguments: string } }
export interface ChatMetricsLite { model: string; promptTokens: number; completionTokens: number; totalTokens: number; reasoningTokens: number; cachedTokens: number; durationMs: number; firstTokenMs: number; tokensPerSecond: number }
export interface ChatAttachmentDraftLite { id: string; name: string; mimeType: string; size: number; width: number; height: number; thumbnailDataUri: string }
export interface MessageAttachmentLite extends ChatAttachmentDraftLite { messageId: number; kind: string; position: number; createdAt: number }
export interface ChatRunErrorLite { code: string; message: string }
import type { ArtifactRefLite } from './artifacts'
export interface AGUIMessageLite { id: string; role: string; content?: unknown; name?: string; agentProfile?: AgentProfile; toolCalls?: AGUIToolCallLite[]; toolCallId?: string; error?: string; runError?: ChatRunErrorLite; activityType?: string; metrics?: ChatMetricsLite; attachments?: Array<ChatAttachmentDraftLite | MessageAttachmentLite>; artifacts?: ArtifactRefLite[] }
export type ConversationTimelineItemLite =
  | { kind: 'message'; sequence: number; message: AGUIMessageLite }
  | { kind: 'plan_request'; sequence: number; request: PlanRequestViewLite }
  | { kind: 'plan'; sequence: number; plan: PlanViewLite }
  | { kind: 'plan_execution'; sequence: number; execution: PlanExecutionViewLite }
export interface ConversationSnapshotLite { type: 'CONVERSATION_SNAPSHOT' | string; timeline: ConversationTimelineItemLite[] }
