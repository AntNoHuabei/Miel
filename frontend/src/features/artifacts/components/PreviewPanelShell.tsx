import { useEffect, useRef, useState } from 'react'
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent, ReactNode } from 'react'
import { MAX_ARTIFACT_PREVIEW_WIDTH, MIN_ARTIFACT_PREVIEW_WIDTH, useArtifactPreviewStore } from '../artifactStore'

interface PreviewPanelShellProps {
  ariaLabel: string
  header: ReactNode
  toolbar?: ReactNode
  footer?: ReactNode
  children: ReactNode
  bodyClassName?: string
}

export function PreviewPanelShell({ ariaLabel, header, toolbar, footer, children, bodyClassName = '' }: PreviewPanelShellProps) {
  const panelWidth = useArtifactPreviewStore((state) => state.width)
  const setPanelWidth = useArtifactPreviewStore((state) => state.setWidth)
  const [resizing, setResizing] = useState(false)
  const panelRef = useRef<HTMLElement>(null)
  const resizeStart = useRef({ x: 0, width: panelWidth })
  const clampToViewport = (width: number) => {
    const available = typeof window === 'undefined' ? MAX_ARTIFACT_PREVIEW_WIDTH : window.innerWidth - 560
    const maximum = Math.max(MIN_ARTIFACT_PREVIEW_WIDTH, Math.min(MAX_ARTIFACT_PREVIEW_WIDTH, available))
    return Math.min(maximum, Math.max(MIN_ARTIFACT_PREVIEW_WIDTH, width))
  }
  const startResize = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (window.innerWidth <= 900 || !panelRef.current) return
    event.preventDefault()
    event.currentTarget.setPointerCapture?.(event.pointerId)
    resizeStart.current = { x: event.clientX, width: panelRef.current.getBoundingClientRect().width }
    setResizing(true)
  }
  const resizeWithKeyboard = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
    event.preventDefault()
    const step = event.shiftKey ? 40 : 16
    const currentWidth = panelRef.current?.getBoundingClientRect().width || panelWidth
    setPanelWidth(clampToViewport(currentWidth + (event.key === 'ArrowLeft' ? step : -step)))
  }
  useEffect(() => {
    if (!resizing) return
    document.body.classList.add('bm-preview-resizing')
    const move = (event: PointerEvent) => setPanelWidth(clampToViewport(resizeStart.current.width + resizeStart.current.x - event.clientX))
    const stop = () => setResizing(false)
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop, { once: true })
    window.addEventListener('pointercancel', stop, { once: true })
    return () => {
      document.body.classList.remove('bm-preview-resizing')
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', stop)
      window.removeEventListener('pointercancel', stop)
    }
  }, [resizing, setPanelWidth])
  const panelStyle = { '--bm-preview-width': `${panelWidth}px` } as CSSProperties
  return <aside ref={panelRef} className={`bm-preview-panel${resizing ? ' is-resizing' : ''}`} aria-label={ariaLabel} style={panelStyle}>
    <div className="bm-preview-resizer" role="separator" aria-label="调整预览宽度" aria-orientation="vertical" aria-valuemin={MIN_ARTIFACT_PREVIEW_WIDTH} aria-valuemax={MAX_ARTIFACT_PREVIEW_WIDTH} aria-valuenow={panelWidth} tabIndex={0} onPointerDown={startResize} onKeyDown={resizeWithKeyboard} />
    <header className="bm-preview-header">{header}</header>
    {toolbar && <div className="bm-preview-toolbar">{toolbar}</div>}
    <div className={`bm-preview-body ${bodyClassName}`.trim()}>{children}</div>
    {footer && <footer className="bm-preview-footer">{footer}</footer>}
  </aside>
}
