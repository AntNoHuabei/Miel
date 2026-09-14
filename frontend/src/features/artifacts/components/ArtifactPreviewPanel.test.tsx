import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ArtifactPreviewPanel } from './ArtifactPreviewPanel'
import { useArtifactPreviewStore } from '../artifactStore'

const { preview } = vi.hoisted(() => ({ preview: vi.fn() }))
vi.mock('../../../shared/repositories', () => ({ artifactRepository: { preview, versions: vi.fn().mockResolvedValue([]), open: vi.fn(), exportFile: vi.fn() }, systemClipboardRepository: { setText: vi.fn() } }))
vi.mock('pdfjs-dist', () => ({ GlobalWorkerOptions: {}, getDocument: vi.fn() }))
vi.mock('pdfjs-dist/build/pdf.worker.min.mjs?url', () => ({ default: 'worker.js' }))

const artifact = { id: 'a1', version: 0, name: 'page.html', mimeType: 'text/html', kind: 'html' as const, size: 30, availability: 'available' as const, width: 0, height: 0 }

describe('ArtifactPreviewPanel', () => {
  beforeEach(() => { preview.mockReset(); useArtifactPreviewStore.getState().close() })

  it('renders HTML in an unscripted sandbox with an offline CSP', async () => {
    preview.mockResolvedValue({ artifact, url: '/artifact', text: '<script>window.bad=true</script><p>result</p>', truncated: false })
    useArtifactPreviewStore.getState().open(artifact)
    const { container } = render(<ArtifactPreviewPanel />)
    await waitFor(() => expect(container.querySelector('iframe')).toBeInTheDocument())
    const frame = container.querySelector('iframe')!
    expect(frame).toHaveAttribute('sandbox', '')
    expect(frame.getAttribute('srcdoc')).toContain("default-src 'none'")
  })

  it('reports the one megabyte text truncation', async () => {
    const textArtifact = { ...artifact, name: 'large.txt', kind: 'text' as const, mimeType: 'text/plain' }
    preview.mockResolvedValue({ artifact: textArtifact, url: '/artifact', text: 'content', truncated: true })
    useArtifactPreviewStore.getState().open(textArtifact)
    render(<ArtifactPreviewPanel />)
    expect(await screen.findByText('文件较大，仅加载前 1 MB')).toBeVisible()
  })
})
