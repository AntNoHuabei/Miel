import { describe, expect, it } from 'vitest'
import { normalizeProviderInput, normalizeSkillHubPage } from './index'

describe('repository adapters', () => {
  it('fills optional provider fields at the binding boundary', () => {
    expect(normalizeProviderInput({ name: 'Local', kind: 'custom', baseUrl: 'http://localhost', apiKey: '', model: 'm', multimodal: false, isDefault: true })).toMatchObject({ id: 0, models: [] })
  })

  it('normalizes nullable SkillHub pages and localized descriptions', () => {
    expect(normalizeSkillHubPage({ skills: null, total: 0, page: 1, pages: 0 }).skills).toEqual([])
    const page = normalizeSkillHubPage({ skills: [{ slug: 's', name: 'S', description_zh: '中文', description: 'English', category: 'tools', downloads: 1, stars: 2, version: '1', iconUrl: '', verified: true }], total: 1, page: 1, pages: 1 })
    expect(page.skills[0]).toMatchObject({ description: '中文', fallback: 'English' })
  })
})
