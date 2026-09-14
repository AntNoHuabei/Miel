import { describe, expect, it } from 'vitest'
import { formatApprovalExpiry } from './PermissionApprovalExpiry'

describe('formatApprovalExpiry', () => {
  const now = Date.parse('2026-09-14T12:00:00Z')

  it('formats the remaining approval time with stable-width numerals', () => {
    expect(formatApprovalExpiry('2026-09-14T12:05:00Z', now)).toBe('05:00 后自动拒绝')
    expect(formatApprovalExpiry('2026-09-14T12:01:05Z', now)).toBe('01:05 后自动拒绝')
  })

  it('handles expired and unavailable deadlines', () => {
    expect(formatApprovalExpiry('2026-09-14T11:59:59Z', now)).toBe('即将自动拒绝')
    expect(formatApprovalExpiry('', now)).toBe('超时后自动拒绝')
  })
})
