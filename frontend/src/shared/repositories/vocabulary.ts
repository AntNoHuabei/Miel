import type { VocabularyReviewLite, VocabularyStatsLite, VocabularyWordInputLite, VocabularyWordLite } from '../types/vocabulary'
import { Services } from './bindings'
import { normalizeList } from './helpers'

export const vocabularyRepository = {
  list: async (query = '') => normalizeList(await Services.VocabularyService.ListWords(query)) as VocabularyWordLite[],
  get: async (id: number) => await Services.VocabularyService.GetWord(id) as VocabularyWordLite,
  add: async (input: VocabularyWordInputLite) => await Services.VocabularyService.AddWord(input) as VocabularyWordLite,
  update: async (input: VocabularyWordInputLite) => await Services.VocabularyService.UpdateWord(input) as VocabularyWordLite,
  delete: (id: number) => Services.VocabularyService.DeleteWord(id),
  createReview: async (count: number) => {
    const result = await Services.VocabularyService.CreateReview(count) as VocabularyReviewLite
    return { ...result, words: normalizeList(result.words) as VocabularyWordLite[] }
  },
  generateSentence: (ids: number[]) => Services.VocabularyService.GenerateSentence(ids),
  recordReview: (ids: number[], mastered: boolean) => Services.VocabularyService.RecordReview(ids, mastered),
  stats: async () => await Services.VocabularyService.Stats() as VocabularyStatsLite,
}
