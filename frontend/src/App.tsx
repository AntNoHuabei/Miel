import { useEffect, useState } from 'react'
import { Flex, Spin, Typography } from 'antd'
import { settingsRepository } from './shared/repositories'
import SetupWizard from './views/SetupWizard'
import AppShell from './views/MainLayout'
import QuickAssistantWindow from './views/QuickAssistantWindow'
import { Window as WailsWindow } from '@wailsio/runtime'

// App 负责“免登录 + 首启引导”:没有任何模型服务商配置时,全屏配置向导拦住入口。
function App() {
  const quickWindow = new URLSearchParams(window.location.search).get('window') === 'quick'
  const [configured, setConfigured] = useState<boolean | null>(null)
  const [error, setError] = useState('')

  const check = async () => {
    try {
      const has = await settingsRepository.hasProviders()
      setConfigured(has)
      setError('')
    } catch (err) {
      setError(String(err))
      setConfigured(false)
    }
  }

  useEffect(() => {
    void check()
  }, [])

  if (quickWindow) {
    return <QuickAssistantWindow configured={configured === true} checking={configured === null} />
  }

  if (configured === null) {
    return (
      <Flex align="center" justify="center" style={{ height: '100vh' }}>
        <Spin size="large" tip="Miel 启动中…">
          <div style={{ padding: 24 }} />
        </Spin>
      </Flex>
    )
  }

  if (error) {
    return (
      <Flex align="center" justify="center" style={{ height: '100vh' }}>
        <Typography.Text type="danger">初始化失败:{error}</Typography.Text>
      </Flex>
    )
  }

  if (!configured) {
    return <SetupWizard onDone={() => void check()} onExit={() => runWindowAction(() => WailsWindow.Close())} />
  }

  return <AppShell />
}

function runWindowAction(action: () => Promise<void>) {
  const runtime = (window as typeof window & { _wails?: { environment?: unknown } })._wails
  if (runtime?.environment) void action()
}

export default App
