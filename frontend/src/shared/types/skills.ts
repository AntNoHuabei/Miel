export interface SkillLite { name: string; description: string; source: 'builtin' | 'local' | 'skillhub' | string; enabled: boolean; path: string; updatedAt: number }
export interface SkillHubSkillLite { slug: string; name: string; description: string; fallback: string; category: string; downloads: number; stars: number; version: string; iconUrl: string; verified: boolean }
export interface SkillHubPageLite { skills: SkillHubSkillLite[]; total: number; page: number; pages: number }
