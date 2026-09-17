import { Button } from 'antd'
import type { ReactNode } from 'react'
import { AppstoreOutlined, BgColorsOutlined, BulbOutlined, CloudServerOutlined, FolderOpenOutlined, InfoCircleOutlined } from '@ant-design/icons'

export type SettingsSection = 'models' | 'skills' | 'memory' | 'appearance' | 'data' | 'about'
const items = [
  { key: 'models', icon: <CloudServerOutlined />, label: '模型服务商' },
  { key: 'skills', icon: <AppstoreOutlined />, label: '技能管理' },
  { key: 'memory', icon: <BulbOutlined />, label: '记忆' },
  { key: 'appearance', icon: <BgColorsOutlined />, label: '外观与皮肤' },
  { key: 'data', icon: <FolderOpenOutlined />, label: '数据与导出' },
  { key: 'about', icon: <InfoCircleOutlined />, label: '关于' },
] satisfies Array<{ key: SettingsSection; icon: ReactNode; label: string }>

export function SettingsNavigation({ active, onChange }: { active: SettingsSection; onChange: (section: SettingsSection) => void }) {
  return (
    <aside className="bm-settings-nav" aria-label="设置分类">
      <nav className="bm-settings-nav-list">
        {items.map((item, index) => (
          <Button type="text" key={item.key} className={`bm-settings-nav-item ${active === item.key ? 'is-active' : ''}`} aria-current={active === item.key ? 'page' : undefined} onClick={() => onChange(item.key)}>
            <span className="bm-settings-nav-index">{String(index + 1).padStart(2, '0')}</span>
            <span className="bm-settings-nav-icon">{item.icon}</span>
            <span className="bm-settings-nav-label">{item.label}</span>
          </Button>
        ))}
      </nav>
    </aside>
  )
}
