export interface VocabularyWordLite {
  id: number
  term: string
  meaning: string
  example: string
  source: string
  createdAt: number
  updatedAt: number
  reviewCount: number
  masteredCount: number
  againCount: number
  lastReviewedAt: number
}

export interface VocabularyWordInputLite {
  id: number
  term: string
  meaning: string
  example: string
  source: string
}

export interface VocabularyReviewLite { words: VocabularyWordLite[]; seed: number }
export interface VocabularyStatsLite { total: number; unreviewed: number; practising: number; mastered: number; reviewCount: number }
