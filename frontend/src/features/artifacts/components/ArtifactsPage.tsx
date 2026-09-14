import { App as AntApp, Button, Empty, Input, Popconfirm, Segmented, Select, Spin, Tooltip, Typography } from 'antd'
import { DeleteOutlined, DownloadOutlined, FolderOpenOutlined, ImportOutlined, UndoOutlined } from '@ant-design/icons'
import { artifactRepository } from '../../../shared/repositories'
import { useArtifactPreviewStore } from '../artifactStore'
import { useArtifacts } from '../controllers/useArtifacts'
import { formatArtifactSize } from './ArtifactItems'

const kindOptions = [{ value: '', label: '全部类型' }, ...['text', 'markdown', 'code', 'html', 'image', 'audio', 'video', 'csv', 'pdf', 'file'].map((value) => ({ value, label: value.toUpperCase() }))]

export function ArtifactsPage() {
  const controller = useArtifacts(); const { message } = AntApp.useApp(); const open = useArtifactPreviewStore((state) => state.open)
  const action = async (fn: () => Promise<unknown>) => { try { await fn(); await controller.reload() } catch (error) { message.error(String(error)) } }
  return <section className="bm-artifacts-page">
    <div className="bm-artifacts-controls"><Input.Search allowClear placeholder="搜索产物名称" value={controller.search} onChange={(event) => controller.setSearch(event.target.value)} /><Select aria-label="产物类型" value={controller.kind} options={kindOptions} onChange={controller.setKind} /><Segmented options={[{ label: '产物', value: false }, { label: '已删除', value: true }]} value={controller.deleted} onChange={(value) => controller.setDeleted(Boolean(value))} /><Button icon={<ImportOutlined />} onClick={() => void controller.importFile()}>导入</Button></div>
    {controller.loading ? <Spin /> : controller.items.length === 0 ? <Empty description={controller.deleted ? '没有已删除的产物' : '暂无产物'} /> : <div className="bm-artifacts-list">{controller.items.map((item) => <article key={`${item.id}:${item.version}`} className={`bm-artifacts-row is-${item.kind}`}><button className="bm-artifacts-open" onClick={() => open(item)}><Typography.Text ellipsis>{item.name}</Typography.Text><span>{item.kind.toUpperCase()} · {formatArtifactSize(item.size)} · 第 {item.version + 1} 版{item.availability === 'missing' ? ' · 文件不可用' : ''}</span></button><div className="bm-artifacts-actions">{!controller.deleted && <><Tooltip title="系统打开"><Button type="text" icon={<FolderOpenOutlined />} onClick={() => void action(() => artifactRepository.open(item.id, item.version))} /></Tooltip><Tooltip title="另存为"><Button type="text" icon={<DownloadOutlined />} onClick={() => void action(() => artifactRepository.exportFile(item.id, item.version))} /></Tooltip><Popconfirm title="删除产物？" description="删除后可在已删除列表恢复。" onConfirm={() => void action(() => artifactRepository.delete(item.id))}><Tooltip title="删除"><Button danger type="text" icon={<DeleteOutlined />} /></Tooltip></Popconfirm></>}{controller.deleted && <Tooltip title="恢复"><Button type="text" icon={<UndoOutlined />} onClick={() => void action(() => artifactRepository.restore(item.id))} /></Tooltip>}</div></article>)}</div>}
  </section>
}
