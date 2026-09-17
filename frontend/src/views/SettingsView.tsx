import { useState } from 'react'
import MemorySettingsPanel from '../components/MemorySettingsPanel'
import { SettingsNavigation } from '../features/settings/components/SettingsNavigation'
import type { SettingsSection } from '../features/settings/components/SettingsNavigation'
import { AppearanceSettingsPage } from '../features/settings/pages/AppearanceSettingsPage'
import { AboutSettingsPage } from '../features/settings/pages/AboutSettingsPage'
import { DataSettingsPage } from '../features/settings/pages/DataSettingsPage'
import { ModelsSettingsPage } from '../features/settings/pages/ModelsSettingsPage'
import { SkillsSettingsPage } from '../features/settings/pages/SkillsSettingsPage'

export default function SettingsView() {
  const [section, setSection] = useState<SettingsSection>('models')

  return (
    <div className="bm-settings-layout">
      <SettingsNavigation active={section} onChange={setSection} />
      <main className="bm-settings-main">
        {section === 'models' && <ModelsSettingsPage />}
        {section === 'skills' && <SkillsSettingsPage />}
        {section === 'memory' && <MemorySettingsPanel />}
        {section === 'appearance' && <AppearanceSettingsPage />}
        {section === 'data' && <DataSettingsPage />}
        {section === 'about' && <AboutSettingsPage />}
      </main>
    </div>
  )
}
