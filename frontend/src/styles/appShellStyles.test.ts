import { describe, expect, it } from 'vitest'
import appShellStyles from './app-shell.css?raw'

function ruleFor(selector: string) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return appShellStyles.match(new RegExp(`${escaped}\\s*\\{[^}]*\\}`, 's'))?.[0] ?? ''
}

describe('feature page viewport', () => {
  it('provides an opaque app-level surface over the mounted chat', () => {
    const layerRule = ruleFor('.bm-shell-feature-layer')
    const contentRule = ruleFor('.bm-feature-content')

    expect(layerRule).toMatch(/background:\s*var\(--bm-content-bg,[^)]+\)\s*;/)
    expect(contentRule).toMatch(/background:\s*var\(--bm-content-bg,[^)]+\)\s*;/)
    expect(`${layerRule}${contentRule}`).not.toMatch(/--bm-chat-/)
  })

  it('shrinks between the sidebar and the right edge', () => {
    const layerRule = ruleFor('.bm-shell-feature-layer')

    expect(layerRule).toMatch(/inset:\s*0\s*;/)
    expect(layerRule).toMatch(/width:\s*auto\s*;/)
  })

  it('keeps every feature page on the shared rounded main content surface', () => {
    const layerRule = ruleFor('.bm-shell-feature-layer')
    const headerRule = ruleFor('.bm-feature-header')

    expect(layerRule).toMatch(/border-top-left-radius:\s*18px\s*;/)
    expect(headerRule).toMatch(/background:\s*var\(--bm-content-bg,[^)]+\)\s*;/)
  })
})
