import { useEffect, useMemo, useRef, useState } from 'react'
import { App as AntApp, Button, Empty, Flex, Segmented, Select, Spin, Table, Tooltip, Typography } from 'antd'
import { CloseOutlined, CopyOutlined, DownloadOutlined, FolderOpenOutlined } from '@ant-design/icons'
import Papa from 'papaparse'
import Prism from 'prismjs'
import 'prismjs/components/prism-typescript'
import 'prismjs/components/prism-jsx'
import 'prismjs/components/prism-tsx'
import 'prismjs/components/prism-go'
import 'prismjs/components/prism-python'
import { artifactRepository, systemClipboardRepository } from '../../../shared/repositories'
import type { ArtifactPreviewLite } from '../../../shared/types/artifacts'
import type { ArtifactRefLite } from '../../../shared/types/artifacts'
import { useArtifactPreviewStore } from '../artifactStore'
import { formatArtifactSize } from './ArtifactItems'
import { renderMarkdown } from '../../chat/components/ConversationMessages'

function languageFor(name: string) {
  const ext = name.split('.').pop()?.toLowerCase() ?? ''
  return ({ ts: 'typescript', tsx: 'tsx', js: 'javascript', jsx: 'jsx', go: 'go', py: 'python', html: 'markup', htm: 'markup', css: 'css', json: 'javascript' } as Record<string, string>)[ext] ?? 'plain'
}

function SafeHTML({ html }: { html: string }) {
  const source = useMemo(() => `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data: blob:; media-src data: blob:; form-action 'none'; base-uri 'none'">${html}`, [html])
  return <iframe title="HTML 产物预览" className="bm-artifact-html" sandbox="" srcDoc={source} referrerPolicy="no-referrer" />
}

function PDFPreview({ url }: { url: string }) {
  const canvas = useRef<HTMLCanvasElement>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let cancelled = false
    let destroy: (() => void) | undefined
    void Promise.all([import('pdfjs-dist'), import('pdfjs-dist/build/pdf.worker.min.mjs?url')]).then(async ([pdfjs, worker]) => {
      pdfjs.GlobalWorkerOptions.workerSrc = worker.default
      const task = pdfjs.getDocument({ url }); destroy = () => { void task.destroy() }
      const pdf = await task.promise; const page = await pdf.getPage(1); if (cancelled || !canvas.current) return
      const base = page.getViewport({ scale: 1 }); const scale = Math.min(1.6, 780 / base.width); const viewport = page.getViewport({ scale })
      const context = canvas.current.getContext('2d'); if (!context) return
      canvas.current.width = viewport.width; canvas.current.height = viewport.height
      await page.render({ canvas: canvas.current, canvasContext: context, viewport }).promise
    }).catch((value) => { if (!cancelled) setError(String(value)) })
    return () => { cancelled = true; destroy?.() }
  }, [url])
  if (error) return <Empty description="PDF 预览不可用，可另存或用系统打开" />
  return <canvas className="bm-artifact-pdf" ref={canvas} />
}

function CSVPreview({ text, truncated }: { text: string; truncated: boolean }) {
  const parsed = useMemo(() => Papa.parse<string[]>(text, { skipEmptyLines: true }), [text])
  const rows = parsed.data.slice(0, 1001); const headers = rows.shift() ?? []
  const columns = headers.map((title, index) => ({ title: title || `第 ${index + 1} 列`, dataIndex: String(index), key: String(index), ellipsis: true }))
  const data = rows.slice(0, 1000).map((row, index) => ({ key: index, ...Object.fromEntries(row.map((cell, column) => [String(column), cell])) }))
  return <Flex vertical gap={8}>{(truncated || parsed.data.length > 1001) && <Typography.Text type="secondary">仅展示前 1,000 行</Typography.Text>}<Table size="small" pagination={false} scroll={{ x: true }} columns={columns} dataSource={data} /></Flex>
}

function CodePreview({ preview }: { preview: ArtifactPreviewLite }) {
  const language = languageFor(preview.artifact.name); const grammar = Prism.languages[language]
  const html = grammar ? Prism.highlight(preview.text, grammar, language) : preview.text.replace(/[&<>]/g, (value) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[value]!))
  return <pre className={`bm-artifact-code language-${language}`}><code dangerouslySetInnerHTML={{ __html: html }} /></pre>
}

export function ArtifactPreviewPanel() {
  const { message } = AntApp.useApp()
  const selected = useArtifactPreviewStore((state) => state.selected)
  const close = useArtifactPreviewStore((state) => state.close)
  const [preview, setPreview] = useState<ArtifactPreviewLite | null>(null)
  const [versions, setVersions] = useState<ArtifactRefLite[]>([])
  const [mode, setMode] = useState<string>('预览')
  useEffect(() => {
    setPreview(null); setMode('预览')
    if (!selected) return
    let active = true
    void Promise.all([artifactRepository.preview(selected.id, selected.version), artifactRepository.versions(selected.id)]).then(([value, availableVersions]) => { if (active) { setPreview(value); setVersions(availableVersions) } }).catch((error) => message.error(`预览失败：${String(error)}`))
    return () => { active = false }
  }, [message, selected])
  if (!selected) return null
  const renderBody = () => {
    if (!preview) return <Spin />
    const { artifact } = preview
    if (artifact.availability !== 'available') return <Empty description={artifact.availability === 'deleted' ? '产物已删除' : '产物文件不可用'} />
    if (artifact.kind === 'image') return <img className="bm-artifact-media-image" src={preview.url} alt={artifact.name} />
    if (artifact.kind === 'audio') return <audio className="bm-artifact-audio" controls preload="metadata" src={preview.url} />
    if (artifact.kind === 'video') return <video className="bm-artifact-video" controls preload="metadata" src={preview.url} />
    if (artifact.kind === 'pdf') return <PDFPreview url={preview.url} />
    if (artifact.kind === 'csv') return <CSVPreview text={preview.text} truncated={preview.truncated} />
    if (artifact.kind === 'html' && mode === '预览') return <SafeHTML html={preview.text} />
    if (artifact.kind === 'markdown' && mode === '预览') return <div className="bm-md bm-artifact-markdown">{renderMarkdown(preview.text)}</div>
    if (['code', 'html'].includes(artifact.kind)) return <CodePreview preview={preview} />
    if (artifact.kind === 'text' || artifact.kind === 'markdown') return <pre className="bm-artifact-text">{preview.text}</pre>
    return <Empty description="此格式不支持内嵌预览" />
  }
  const selectable = preview && ['markdown', 'html'].includes(preview.artifact.kind)
  return <aside className="bm-artifact-preview-panel" aria-label="产物预览">
    <header className="bm-artifact-preview-header"><div><Typography.Text strong ellipsis>{selected.name}</Typography.Text><span>{selected.kind.toUpperCase()} · {formatArtifactSize(selected.size)} · 第 {selected.version + 1} 版</span></div><Tooltip title="关闭预览"><Button type="text" aria-label="关闭预览" icon={<CloseOutlined />} onClick={close} /></Tooltip></header>
    <div className="bm-artifact-preview-toolbar">{selectable && <Segmented size="small" options={['预览', '源文件']} value={mode} onChange={(value) => setMode(String(value))} />}{versions.length > 1 && <Select size="small" aria-label="产物版本" value={selected.version} options={versions.map((item) => ({ value: item.version, label: `第 ${item.version + 1} 版` }))} onChange={(version) => { const target = versions.find((item) => item.version === version); if (target) useArtifactPreviewStore.getState().open(target) }} />}<span />{!!preview?.text && <Tooltip title="复制内容"><Button type="text" aria-label="复制内容" icon={<CopyOutlined />} onClick={() => void systemClipboardRepository.setText(preview.text).then(() => message.success('已复制')).catch((error) => message.error(String(error)))} /></Tooltip>}<Tooltip title="系统打开"><Button type="text" aria-label="系统打开" icon={<FolderOpenOutlined />} onClick={() => void artifactRepository.open(selected.id, selected.version).catch((error) => message.error(String(error)))} /></Tooltip><Tooltip title="另存为"><Button type="text" aria-label="另存为" icon={<DownloadOutlined />} onClick={() => void artifactRepository.exportFile(selected.id, selected.version).catch((error) => message.error(String(error)))} /></Tooltip></div>
    <div className="bm-artifact-preview-body">{preview?.truncated && <Typography.Text type="secondary" className="bm-artifact-truncated">文件较大，仅加载前 1 MB</Typography.Text>}{renderBody()}</div>
  </aside>
}
