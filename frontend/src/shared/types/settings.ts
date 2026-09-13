export interface ProviderTemplateLite { name: string; kind: string; baseUrl: string; model: string; multimodal: boolean; docsUrl: string }
export interface ProviderLite { id: number; name: string; kind: string; baseUrl: string; apiKey: string; model: string; multimodal: boolean; isDefault: boolean; createdAt: number }
export interface ProviderInputLite { id?: number; name: string; kind: string; baseUrl: string; apiKey: string; model: string; multimodal: boolean; isDefault: boolean; models?: ProviderModelInputLite[] }
export interface ProviderModelLite { model: string; label: string; custom: boolean; multimodal: boolean }
export interface ProviderModelInputLite { model: string; label?: string; custom?: boolean; multimodal?: boolean }
export interface DiscoveredModelLite { id: string; status: string; reasoning: ReasoningSpecLite; multimodal: boolean }
export interface ModelOptionLite { providerId: number; providerName: string; kind: string; model: string; label: string; custom: boolean; multimodal: boolean; isDefault: boolean }
export interface WorkspaceLite { name: string; path: string; isCurrent: boolean }
export interface PingResultLite { ok: boolean; message: string; model: string; latencyMs: number }
export interface ReasoningSpecLite { type: 'toggle' | 'effort' | 'always' | 'none' | string; levels?: string[]; note?: string }
export interface CatalogModelLite { id: string; label: string; reasoning: ReasoningSpecLite; multimodal: boolean }
export interface CatalogProviderLite { kind: string; name: string; baseUrl: string; models: CatalogModelLite[] }
