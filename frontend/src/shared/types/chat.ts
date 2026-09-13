export interface ConversationLite { id: number; title: string; createdAt: number; updatedAt: number }
export interface AGUIToolCallLite { id: string; type: string; function: { name: string; arguments: string } }
export interface ChatMetricsLite { model: string; promptTokens: number; completionTokens: number; totalTokens: number; reasoningTokens: number; cachedTokens: number; durationMs: number; firstTokenMs: number; tokensPerSecond: number }
export interface ChatAttachmentDraftLite { id: string; name: string; mimeType: string; size: number; width: number; height: number; thumbnailDataUri: string }
export interface MessageAttachmentLite extends ChatAttachmentDraftLite { messageId: number; kind: string; position: number; createdAt: number }
export interface AGUIMessageLite { id: string; role: string; content?: unknown; name?: string; toolCalls?: AGUIToolCallLite[]; toolCallId?: string; error?: string; activityType?: string; metrics?: ChatMetricsLite; attachments?: Array<ChatAttachmentDraftLite | MessageAttachmentLite> }
export interface AGUIMessagesSnapshotLite { type: 'MESSAGES_SNAPSHOT' | string; messages: AGUIMessageLite[] }
