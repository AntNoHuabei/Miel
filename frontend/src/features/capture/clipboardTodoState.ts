import type { ClipboardTodoDraftLite, ExtractedTodoLite } from '../../api'

export type ClipboardTodoStage = 'idle' | 'loading' | 'review' | 'confirming' | 'error'

export interface ClipboardTodoState {
  stage: ClipboardTodoStage
  draft: ClipboardTodoDraftLite | null
  items: ExtractedTodoLite[]
  selected: number[]
  error: string
}

export type ClipboardTodoAction =
  | { type: 'reset' }
  | { type: 'loading' }
  | { type: 'loaded'; draft: ClipboardTodoDraftLite }
  | { type: 'failed'; error: string }
  | { type: 'update'; index: number; patch: Partial<ExtractedTodoLite> }
  | { type: 'toggle'; index: number; selected: boolean }
  | { type: 'remove'; index: number }
  | { type: 'confirming' }
  | { type: 'confirmation-failed'; error: string }

export const initialClipboardTodoState: ClipboardTodoState = {
  stage: 'idle',
  draft: null,
  items: [],
  selected: [],
  error: '',
}

export function clipboardTodoReducer(state: ClipboardTodoState, action: ClipboardTodoAction): ClipboardTodoState {
  switch (action.type) {
    case 'reset':
      return initialClipboardTodoState
    case 'loading':
      return { ...initialClipboardTodoState, stage: 'loading' }
    case 'loaded':
      return {
        stage: 'review',
        draft: action.draft,
        items: action.draft.items ?? [],
        selected: (action.draft.items ?? []).map((_, index) => index),
        error: '',
      }
    case 'failed':
      return { ...initialClipboardTodoState, stage: 'error', error: action.error }
    case 'update':
      return { ...state, items: state.items.map((item, index) => index === action.index ? { ...item, ...action.patch } : item) }
    case 'toggle':
      return {
        ...state,
        selected: action.selected
          ? Array.from(new Set([...state.selected, action.index]))
          : state.selected.filter((index) => index !== action.index),
      }
    case 'remove':
      return { ...state, selected: state.selected.filter((index) => index !== action.index) }
    case 'confirming':
      return { ...state, stage: 'confirming', error: '' }
    case 'confirmation-failed':
      return { ...state, stage: 'review', error: action.error }
  }
}
