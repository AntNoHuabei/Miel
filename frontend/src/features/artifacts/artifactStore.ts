import { create } from 'zustand'
import type { ArtifactRefLite } from '../../shared/types/artifacts'

interface ArtifactPreviewState {
  selected: ArtifactRefLite | null
  open: (artifact: ArtifactRefLite) => void
  close: () => void
}

export const useArtifactPreviewStore = create<ArtifactPreviewState>((set) => ({
  selected: null,
  open: (selected) => set({ selected }),
  close: () => set({ selected: null }),
}))
