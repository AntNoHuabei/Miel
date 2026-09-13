import { describe, expect, it } from 'vitest'
import { captureReducer, initialCaptureState } from './captureState'

describe('captureReducer', () => {
  it('describes processing, confirmation, failure and reset', () => {
    expect(captureReducer(initialCaptureState, { type: 'processing' })).toMatchObject({ stage: 'busy', busy: true })
    expect(captureReducer(initialCaptureState, { type: 'extracted' }).stage).toBe('extracted')
    expect(captureReducer(initialCaptureState, { type: 'failed', error: 'capture failed' })).toMatchObject({ stage: 'error', error: 'capture failed' })
    expect(captureReducer({ stage: 'answer', busy: false, error: '' }, { type: 'reset' })).toEqual(initialCaptureState)
  })
})
