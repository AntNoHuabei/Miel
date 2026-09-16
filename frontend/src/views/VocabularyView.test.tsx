import { App } from 'antd'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const repository = vi.hoisted(() => ({
  list: vi.fn(),
  stats: vi.fn(),
  createReview: vi.fn(),
  generateSentence: vi.fn(),
  recordReview: vi.fn(),
  add: vi.fn(),
  update: vi.fn(),
  delete: vi.fn(),
}))

vi.mock('../shared/repositories', () => ({ vocabularyRepository: repository }))
vi.mock('../shared/wails/events', () => ({ useWailsEvent: vi.fn() }))

import VocabularyView from './VocabularyView'

const word = {
  id: 7,
  term: 'serendipity',
  meaning: '意外发现美好事物的运气',
  example: '',
  source: 'selection',
  createdAt: 1,
  updatedAt: 1,
  reviewCount: 0,
  masteredCount: 0,
  againCount: 0,
  lastReviewedAt: 0,
}

describe('VocabularyView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    repository.list.mockResolvedValue([word])
    repository.stats.mockResolvedValue({ total: 1, unreviewed: 1, practising: 0, mastered: 0, reviewCount: 0 })
    repository.createReview.mockResolvedValue({ words: [word], seed: 1 })
    repository.generateSentence.mockResolvedValue('We found the cafe by pure serendipity.')
    repository.recordReview.mockResolvedValue(undefined)
  })

  it('shows saved words and completes a sentence review', async () => {
    const user = userEvent.setup()
    render(<App><VocabularyView /></App>)

    expect(await screen.findByText('serendipity')).toBeInTheDocument()
    expect(screen.getByText('意外发现美好事物的运气')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /开始复习/ }))
    expect(await screen.findByPlaceholderText('在这里写下你的句子')).toBeInTheDocument()
    await user.type(screen.getByPlaceholderText('在这里写下你的句子'), 'I met her by serendipity.')
    await user.click(screen.getByRole('button', { name: '查看参考句' }))
    expect(await screen.findByText('We found the cafe by pure serendipity.')).toBeInTheDocument()
    expect(repository.generateSentence).toHaveBeenCalledWith([7])

    await user.click(screen.getByRole('button', { name: /已经掌握/ }))
    await waitFor(() => expect(repository.recordReview).toHaveBeenCalledWith([7], true))
  })
})
