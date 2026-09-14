import { create } from 'zustand'
import type { ArtifactRefLite } from '../../shared/types/artifacts'

const PREVIEW_WIDTH_STORAGE_KEY = 'artifact.preview.width.v1'
export const DEFAULT_ARTIFACT_PREVIEW_WIDTH = 520
export const MIN_ARTIFACT_PREVIEW_WIDTH = 340
export const MAX_ARTIFACT_PREVIEW_WIDTH = 720

function storedPreviewWidth() {
  if (typeof window === 'undefined') return DEFAULT_ARTIFACT_PREVIEW_WIDTH
  const value = Number(window.localStorage.getItem(PREVIEW_WIDTH_STORAGE_KEY))
  return Number.isFinite(value) && value >= MIN_ARTIFACT_PREVIEW_WIDTH && value <= MAX_ARTIFACT_PREVIEW_WIDTH
    ? value
    : DEFAULT_ARTIFACT_PREVIEW_WIDTH
}

interface ArtifactPreviewState {
  selected: ArtifactRefLite | null
  width: number
  open: (artifact: ArtifactRefLite) => void
  close: () => void
  setWidth: (width: number) => void
}

export const useArtifactPreviewStore = create<ArtifactPreviewState>((set) => ({
  selected: null,
  width: storedPreviewWidth(),
  open: (selected) => set({ selected }),
  close: () => set({ selected: null }),
  setWidth: (width) => {
    const next = Math.round(Math.min(MAX_ARTIFACT_PREVIEW_WIDTH, Math.max(MIN_ARTIFACT_PREVIEW_WIDTH, width)))
    if (typeof window !== 'undefined') window.localStorage.setItem(PREVIEW_WIDTH_STORAGE_KEY, String(next))
    set({ width: next })
  },
}))
