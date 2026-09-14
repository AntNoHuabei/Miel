export type ArtifactKind = 'text' | 'markdown' | 'code' | 'html' | 'image' | 'audio' | 'video' | 'csv' | 'pdf' | 'file'
export interface ArtifactRefLite { id: string; version: number; name: string; mimeType: string; kind: ArtifactKind; size: number; availability: 'available' | 'deleted' | 'missing'; width: number; height: number }
export interface ArtifactPreviewLite { artifact: ArtifactRefLite; url: string; text: string; truncated: boolean }
export interface SaveMessageArtifactInput { conversationId: number; messageId: string; name: string; codeBlock?: number }
