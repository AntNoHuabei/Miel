import { useEffect, useState } from 'react'
import { Typography } from 'antd'
import { appRepository } from '../../../shared/repositories'

const { Title, Text } = Typography

export function AboutSettingsPage() {
  const [version, setVersion] = useState('读取中...')

  useEffect(() => {
    appRepository.version().then(setVersion).catch(() => setVersion('未知'))
  }, [])

  return (
    <section className="bm-settings-panel" aria-labelledby="settings-about-title">
      <header className="bm-settings-page-heading">
        <div><span className="bm-settings-section-number">06</span><Title id="settings-about-title" level={3}>关于</Title></div>
      </header>
      <div className="bm-settings-about-band">
        <img className="bm-settings-about-icon" src="/appicon.png" alt="" aria-hidden="true" />
        <div className="bm-settings-about-copy">
          <Title level={4}>Miel</Title>
          <Text type="secondary">本地办公 Agent</Text>
        </div>
        <div className="bm-settings-about-version">
          <Text type="secondary">版本</Text>
          <code>{version}</code>
        </div>
      </div>
    </section>
  )
}
