export type CaptureStage = 'menu' | 'busy' | 'extracted' | 'answer' | 'error'
export interface CaptureState { stage: CaptureStage; busy: boolean; error: string }
export type CaptureAction =
  | { type: 'reset' }
  | { type: 'processing' }
  | { type: 'extracted' }
  | { type: 'answered' }
  | { type: 'saving' }
  | { type: 'confirming' }
  | { type: 'failed'; error: string }
  | { type: 'back' }

export const initialCaptureState: CaptureState = { stage: 'menu', busy: false, error: '' }
export function captureReducer(_: CaptureState, action: CaptureAction): CaptureState {
  switch (action.type) {
    case 'reset': case 'back': return initialCaptureState
    case 'processing': return { stage: 'busy', busy: true, error: '' }
    case 'saving': return { stage: 'menu', busy: true, error: '' }
    case 'confirming': return { stage: 'extracted', busy: true, error: '' }
    case 'extracted': return { stage: 'extracted', busy: false, error: '' }
    case 'answered': return { stage: 'answer', busy: false, error: '' }
    case 'failed': return { stage: 'error', busy: false, error: action.error }
  }
}
