export interface MemoryConfigLite { enabled: boolean; autoExtract: boolean; strategy: string; customPrompt: string }
export interface MemoryStatusLite { state: 'idle' | 'extracting' | 'error' | string; pendingJobs: number; lastSuccessAt: string; lastError: string }
export interface MemoryStrategyLite { id: string; name: string; description: string; risk: string; prompt: string }
export interface MemorySettingsLite { config: MemoryConfigLite; strategies: MemoryStrategyLite[]; effectivePrompt: string; currentModel: string; status: MemoryStatusLite }
export interface MemoryInputLite { content: string; topics: string[]; kind: 'fact' | 'episode'; eventTime: string; participants: string[]; location: string }
export interface MemoryItemLite extends MemoryInputLite { id: string; createdAt: string; updatedAt: string }
