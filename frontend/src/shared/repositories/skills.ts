import type { SkillHubPageLite, SkillLite } from '../types/skills'
import { Services } from './bindings'
import { normalizeList } from './helpers'

export const skillRepository = {
  list: async () => normalizeList(await Services.SkillService.ListSkills()) as SkillLite[],
  listHub: async (page: number, size: number, query: string): Promise<SkillHubPageLite> => normalizeSkillHubPage(await Services.SkillService.ListSkillHubSkills(page, size, query)),
  pickLocal: () => Services.SkillService.PickLocal(),
  setEnabled: (name: string, enabled: boolean) => Services.SkillService.SetEnabled(name, enabled),
  delete: (name: string) => Services.SkillService.Delete(name),
  installHub: (slug: string) => Services.SkillService.InstallSkillHub(slug),
}

export function normalizeSkillHubPage(result: {
  skills?: Array<{ slug: string; name: string; description_zh?: string; description: string; category: string; downloads: number; stars: number; version: string; iconUrl: string; verified: boolean }> | null
  total: number
  page: number
  pages: number
}): SkillHubPageLite {
  return {
    skills: normalizeList(result.skills).map((skill) => ({ ...skill, description: skill.description_zh || skill.description, fallback: skill.description })),
    total: result.total,
    page: result.page,
    pages: result.pages,
  }
}
