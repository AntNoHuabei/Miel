import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ArtifactItems } from './ArtifactItems'
import { useArtifactPreviewStore } from '../artifactStore'

vi.mock('../../../shared/repositories', () => ({ artifactRepository: { preview: vi.fn().mockResolvedValue({ url: '/image' }) } }))

const artifact = { id: 'a1', version: 1, name: 'demo.png', mimeType: 'image/png', kind: 'image' as const, size: 2048, availability: 'available' as const, width: 20, height: 10 }

describe('ArtifactItems', () => {
  it('shows real metadata, removes duplicate events, and opens preview', async () => {
    useArtifactPreviewStore.getState().close()
    render(<ArtifactItems artifacts={[artifact, artifact]} />)
    expect(screen.getAllByText('demo.png')).toHaveLength(1)
    expect(screen.getByText('IMAGE · 2.0 KB · 20 x 10 · 第 2 版')).toBeVisible()
    await userEvent.click(screen.getByRole('button'))
    expect(useArtifactPreviewStore.getState().selected).toEqual(artifact)
  })

  it('disables missing files', () => {
    render(<ArtifactItems artifacts={[{ ...artifact, availability: 'missing' }]} />)
    expect(screen.getByRole('button')).toBeDisabled()
  })
})
