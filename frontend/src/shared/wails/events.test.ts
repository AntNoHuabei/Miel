import { describe, expect, it } from 'vitest'
import { eventData, parseEventData } from './events'

describe('Wails event adapter', () => {
  it('unwraps runtime payloads and accepts direct values', () => {
    expect(eventData({ data: { id: 2 } })).toEqual({ id: 2 })
    expect(eventData('ready')).toBe('ready')
  })

  it('normalizes JSON payloads and malformed data', () => {
    expect(parseEventData<{ id: number }>('{"id":4}')).toEqual({ id: 4 })
    expect(parseEventData('{')).toBeNull()
  })
})
