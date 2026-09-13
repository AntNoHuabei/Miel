import { createStore } from 'zustand/vanilla'
import type { TodoLite, TodoStatsLite } from '../../api'

export interface TodoState {
  items: TodoLite[]
  stats: TodoStatsLite | null
  loading: boolean
  loaded: boolean
  error: string
  beginLoad: () => void
  setData: (items: TodoLite[], stats: TodoStatsLite | null) => void
  setError: (error: string) => void
}

export const todoStore = createStore<TodoState>((set) => ({
  items: [], stats: null, loading: false, loaded: false, error: '',
  beginLoad: () => set({ loading: true, error: '' }),
  setData: (items, stats) => set({ items, stats, loading: false, loaded: true, error: '' }),
  setError: (error) => set({ loading: false, loaded: true, error }),
}))
