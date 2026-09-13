import { CheckOutlined } from '@ant-design/icons'
import { Typography } from 'antd'
import { BM_THEMES, useBMTheme } from '../../../theme/ThemeContext'

const { Title } = Typography

export function AppearanceSettingsPage() {
  const { themeId, setTheme } = useBMTheme()
  return (
    <section className="bm-settings-panel" aria-labelledby="settings-appearance-title">
      <header className="bm-settings-page-heading"><div><span className="bm-settings-section-number">04</span><Title id="settings-appearance-title" level={3}>外观与皮肤</Title></div></header>
      <div className="bm-settings-appearance-list">{BM_THEMES.map((theme, index) => {
        const selected = themeId === theme.id
        return <button type="button" className={`bm-settings-appearance-row ${selected ? 'is-active' : ''}`} aria-pressed={selected} key={theme.id} onClick={() => void setTheme(theme.id)}><span className="bm-settings-theme-index">{String(index + 1).padStart(2, '0')}</span><span className="bm-settings-theme-swatch" style={{ backgroundColor: theme.vars['sidebar-bg'], borderColor: theme.vars.divider }}><span style={{ backgroundColor: theme.vars.signal }} /></span><span className="bm-settings-theme-copy"><strong>{theme.name}</strong><small>{theme.desc}</small></span><span className="bm-settings-theme-mode">{theme.dark ? '深色' : '浅色'}</span><span className="bm-settings-theme-check" aria-hidden="true">{selected && <CheckOutlined />}</span></button>
      })}</div>
    </section>
  )
}
