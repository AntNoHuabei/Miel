import { useEffect, useState } from 'react'
import { Button, Typography } from 'antd'
import { FolderOpenOutlined } from '@ant-design/icons'
import { directoryRepository } from '../../../shared/repositories'

const { Title, Text } = Typography

export function DataSettingsPage() {
  const [dataDir, setDataDir] = useState('')
  useEffect(() => { directoryRepository.paths().then((paths) => setDataDir(paths.root)).catch(() => undefined) }, [])
  return (
    <section className="bm-settings-panel" aria-labelledby="settings-data-title">
      <header className="bm-settings-page-heading"><div><span className="bm-settings-section-number">05</span><Title id="settings-data-title" level={3}>数据与导出</Title></div></header>
      <div className="bm-settings-data-band"><span className="bm-settings-data-index">01</span><div className="bm-settings-data-copy"><Text strong>应用数据目录</Text><Text type="secondary">截图、周报、文档、表格和技能文件保存在此目录</Text><code>{dataDir || '读取中...'}</code></div><Button icon={<FolderOpenOutlined />} onClick={() => void directoryRepository.openDataDir()}>打开目录</Button></div>
    </section>
  )
}
