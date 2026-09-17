import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ArtifactPreviewPanel } from './ArtifactPreviewPanel'
import { DEFAULT_ARTIFACT_PREVIEW_WIDTH, useArtifactPreviewStore } from '../artifactStore'

const { preview } = vi.hoisted(() => ({ preview: vi.fn() }))
vi.mock('../../../shared/repositories', () => ({ artifactRepository: { preview, versions: vi.fn().mockResolvedValue([]), open: vi.fn(), exportFile: vi.fn() }, systemClipboardRepository: { setText: vi.fn() } }))
vi.mock('pdfjs-dist', () => ({ GlobalWorkerOptions: {}, getDocument: vi.fn() }))
vi.mock('pdfjs-dist/build/pdf.worker.min.mjs?url', () => ({ default: 'worker.js' }))

const artifact = { id: 'a1', version: 0, name: 'page.html', mimeType: 'text/html', kind: 'html' as const, size: 30, availability: 'available' as const, width: 0, height: 0 }

describe('ArtifactPreviewPanel', () => {
  beforeEach(() => {
    preview.mockReset()
    useArtifactPreviewStore.getState().close()
    useArtifactPreviewStore.getState().setWidth(DEFAULT_ARTIFACT_PREVIEW_WIDTH)
  })

  it('loads the complete HTML resource without script or network restrictions', async () => {
    preview.mockResolvedValue({ artifact, url: '/artifact', text: '<script>window.bad=true</script><p>result</p>', truncated: false })
    useArtifactPreviewStore.getState().open(artifact)
    const { container } = render(<ArtifactPreviewPanel />)
    await waitFor(() => expect(container.querySelector('iframe')).toBeInTheDocument())
    const frame = container.querySelector('iframe')!
    expect(frame).toHaveAttribute('src', '/artifact')
    expect(frame).not.toHaveAttribute('sandbox')
    expect(frame).not.toHaveAttribute('referrerpolicy')
    expect(frame).not.toHaveAttribute('srcdoc')
  })

  it('reports the one megabyte text truncation', async () => {
    const textArtifact = { ...artifact, name: 'large.txt', kind: 'text' as const, mimeType: 'text/plain' }
    preview.mockResolvedValue({ artifact: textArtifact, url: '/artifact', text: 'content', truncated: true })
    useArtifactPreviewStore.getState().open(textArtifact)
    render(<ArtifactPreviewPanel />)
    expect(await screen.findByText('文件较大，仅加载前 1 MB')).toBeVisible()
  })

  it('resizes the panel with the keyboard and persists the width', async () => {
    const viewport = vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(1440)
    preview.mockResolvedValue({ artifact, url: '/artifact', text: '', truncated: false })
    useArtifactPreviewStore.getState().open(artifact)
    render(<ArtifactPreviewPanel />)
    const separator = await screen.findByRole('separator', { name: '调整预览宽度' })
    fireEvent.keyDown(separator, { key: 'ArrowLeft' })
    expect(screen.getByRole('complementary', { name: '产物预览' })).toHaveStyle('--bm-preview-width: 536px')
    expect(window.localStorage.getItem('artifact.preview.width.v1')).toBe('536')
    viewport.mockRestore()
  })

  it('resizes the panel by dragging its left edge', async () => {
    const viewport = vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(1440)
    preview.mockResolvedValue({ artifact, url: '/artifact', text: '', truncated: false })
    useArtifactPreviewStore.getState().open(artifact)
    render(<ArtifactPreviewPanel />)
    const panel = await screen.findByRole('complementary', { name: '产物预览' })
    vi.spyOn(panel, 'getBoundingClientRect').mockReturnValue({ width: 520 } as DOMRect)
    const separator = screen.getByRole('separator', { name: '调整预览宽度' })
    fireEvent.pointerDown(separator, { clientX: 600 })
    fireEvent.pointerMove(window, { clientX: 520 })
    expect(panel).toHaveStyle('--bm-preview-width: 600px')
    fireEvent.pointerUp(window)
    viewport.mockRestore()
  })
})
