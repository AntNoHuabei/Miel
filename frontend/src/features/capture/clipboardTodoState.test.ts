import { describe, expect, it } from 'vitest'
import { clipboardTodoReducer, initialClipboardTodoState } from './clipboardTodoState'

const draft = {
  draftId: 'draft-1',
  kind: 'clipboard_text',
  text: 'ship it',
  dataUri: '',
  createdAt: 1,
  items: [
    { title: 'First', description: '', milestone: false, dueDate: '' },
    { title: 'Second', description: '', milestone: false, dueDate: '' },
  ],
}

describe('clipboardTodoReducer', () => {
  it('moves extraction through loading, review and confirmation', () => {
    const loading = clipboardTodoReducer(initialClipboardTodoState, { type: 'loading' })
    expect(loading.stage).toBe('loading')
    const review = clipboardTodoReducer(loading, { type: 'loaded', draft })
    expect(review).toMatchObject({ stage: 'review', selected: [0, 1] })
    expect(clipboardTodoReducer(review, { type: 'confirming' }).stage).toBe('confirming')
  })

  it('updates fields and selection without losing the draft', () => {
    const review = clipboardTodoReducer(initialClipboardTodoState, { type: 'loaded', draft })
    const updated = clipboardTodoReducer(review, { type: 'update', index: 0, patch: { title: 'Updated' } })
    const removed = clipboardTodoReducer(updated, { type: 'remove', index: 1 })
    expect(removed.items[0].title).toBe('Updated')
    expect(removed.selected).toEqual([0])
    expect(removed.draft?.draftId).toBe('draft-1')
  })

  it('keeps extracted data available after a confirmation failure', () => {
    const review = clipboardTodoReducer(initialClipboardTodoState, { type: 'loaded', draft })
    const failed = clipboardTodoReducer(clipboardTodoReducer(review, { type: 'confirming' }), { type: 'confirmation-failed', error: 'save failed' })
    expect(failed).toMatchObject({ stage: 'review', error: 'save failed' })
    expect(failed.items).toHaveLength(2)
  })
})
