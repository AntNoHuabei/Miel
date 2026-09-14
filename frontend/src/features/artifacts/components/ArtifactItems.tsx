import { useEffect, useState } from 'react'
import { Button, Tooltip, Typography } from 'antd'
import { AudioOutlined, CodeOutlined, FileOutlined, FilePdfOutlined, FileTextOutlined, PictureOutlined, VideoCameraOutlined } from '@ant-design/icons'
import type { ReactNode } from 'react'
import type { ArtifactKind, ArtifactRefLite } from '../../../shared/types/artifacts'
import { useArtifactPreviewStore } from '../artifactStore'
import { artifactRepository } from '../../../shared/repositories'

const iconByKind: Record<ArtifactKind, ReactNode> = {
  text: <FileTextOutlined />, markdown: <FileTextOutlined />, code: <CodeOutlined />, html: <CodeOutlined />,
  image: <PictureOutlined />, audio: <AudioOutlined />, video: <VideoCameraOutlined />, csv: <FileTextOutlined />, pdf: <FilePdfOutlined />, file: <FileOutlined />,
}

export function formatArtifactSize(size: number) {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(size < 10 * 1024 ? 1 : 0)} KB`
  return `${(size / 1024 / 1024).toFixed(size < 10 * 1024 * 1024 ? 1 : 0)} MB`
}

export function ArtifactItems({ artifacts }: { artifacts?: ArtifactRefLite[] }) {
  const open = useArtifactPreviewStore((state) => state.open)
  if (!artifacts?.length) return null
  const unique = [...new Map(artifacts.map((item) => [`${item.id}:${item.version}`, item])).values()]
  return <div className="bm-artifact-items" aria-label="对话产物">{unique.map((item) => (
    <Tooltip key={`${item.id}:${item.version}`} title={item.availability === 'available' ? '预览产物' : item.availability === 'deleted' ? '产物已删除' : '产物文件不可用'}>
      <Button type="text" className={`bm-artifact-item is-${item.kind}`} disabled={item.availability !== 'available'} onClick={() => open(item)}>
        <span className="bm-artifact-kind-icon">{item.kind === 'image' ? <ArtifactThumbnail artifact={item} /> : iconByKind[item.kind] ?? <FileOutlined />}</span>
        <span className="bm-artifact-item-copy"><Typography.Text ellipsis>{item.name}</Typography.Text><span>{item.kind.toUpperCase()} · {formatArtifactSize(item.size)}{item.kind === 'image' && item.width > 0 && item.height > 0 ? ` · ${item.width} x ${item.height}` : ''} · 第 {item.version + 1} 版</span></span>
      </Button>
    </Tooltip>
  ))}</div>
}

function ArtifactThumbnail({ artifact }: { artifact: ArtifactRefLite }) {
  const [url, setURL] = useState('')
  useEffect(() => { let active = true; void artifactRepository.preview(artifact.id, artifact.version).then((value) => { if (active) setURL(value.url) }).catch(() => undefined); return () => { active = false } }, [artifact.id, artifact.version])
  return url ? <img className="bm-artifact-thumb" src={url} alt="" /> : <PictureOutlined />
}
