import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import type { ReactNode } from 'react'
import { App as AntApp, ConfigProvider, theme as antdTheme } from 'antd'
import type { ThemeConfig } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn'
import { SettingsService, WindowThemeService } from '../api'
import { BM_THEMES, DEFAULT_THEME_ID, themeById } from './themes'
import type { BMTheme } from './themes'

dayjs.locale('zh-cn')

const THEME_STORAGE_KEY = 'blankmind.theme'

function readCachedThemeId(): string {
  try {
    return themeById(window.localStorage.getItem(THEME_STORAGE_KEY)).id
  } catch {
    return DEFAULT_THEME_ID
  }
}

function cacheThemeId(id: string) {
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, id)
  } catch {
    // WebView storage can be unavailable in restricted environments.
  }
}

interface ThemeCtxValue {
  themeId: string
  theme: BMTheme
  setTheme: (id: string) => Promise<void>
}

const ThemeCtx = createContext<ThemeCtxValue>({
  themeId: DEFAULT_THEME_ID,
  theme: themeById(DEFAULT_THEME_ID),
  setTheme: async () => undefined,
})

export function useBMTheme(): ThemeCtxValue {
  return useContext(ThemeCtx)
}

// 全局主题 Provider:
//  1. 启动时读取设置项 theme(默认 light)持久化选择;
//  2. 把皮肤 CSS 变量以 --bm-* 注入 <html>,驱动自定义样式的背景/边框;
//  3. 通过 ConfigProvider 把 antd token 应用到全组件树;
//  4. 提供 setTheme(id) 热切换并落盘。
export default function ThemeProvider({ children }: { children: ReactNode }) {
  const [themeId, setThemeId] = useState<string>(readCachedThemeId)
  const selectionVersion = useRef(0)
  const theme = useMemo(() => themeById(themeId), [themeId])

  // 启动恢复
  useEffect(() => {
    const cachedId = readCachedThemeId()
    const initialSelectionVersion = selectionVersion.current
    void WindowThemeService.SetTheme(cachedId).catch(() => undefined)

    SettingsService.GetSetting('theme')
      .then((v: string) => {
        if (selectionVersion.current !== initialSelectionVersion) return
        const id = themeById(v || cachedId).id
        setThemeId(id)
        cacheThemeId(id)
        void WindowThemeService.SetTheme(id).catch(() => undefined)
        if (!v) void SettingsService.SetSetting('theme', id).catch(() => undefined)
      })
      .catch(() => {
        void WindowThemeService.SetTheme(cachedId).catch(() => undefined)
      })
  }, [])

  // 注入 CSS 变量
  useEffect(() => {
    const root = document.documentElement.style
    for (const [k, v] of Object.entries(theme.vars)) {
      root.setProperty(`--bm-${k}`, v)
    }
    root.colorScheme = theme.dark ? 'dark' : 'light'
  }, [theme])

  const setTheme = useCallback(async (id: string) => {
    const t = themeById(id)
    selectionVersion.current += 1
    setThemeId(t.id)
    cacheThemeId(t.id)
    try {
      await SettingsService.SetSetting('theme', t.id)
    } catch {
      // 持久化失败不阻塞切换
    }
    try {
      await WindowThemeService.SetTheme(t.id)
    } catch {
      // 原生标题栏不可用时不阻塞 WebView 皮肤切换
    }
  }, [])

  const config = useMemo<ThemeConfig>(() => {
    const algorithm = theme.dark
      ? antdTheme.darkAlgorithm
      : antdTheme.defaultAlgorithm
    // 透传自定义种子令牌(colorPrimary 等);多余键由 antd 忽略
    const token = theme.token as unknown as ThemeConfig['token']
    return { algorithm, token }
  }, [theme])

  return (
    <ThemeCtx.Provider value={{ themeId, theme, setTheme }}>
      <ConfigProvider locale={zhCN} theme={config}>
        <AntApp>{children}</AntApp>
      </ConfigProvider>
    </ThemeCtx.Provider>
  )
}

// 导出内置主题列表供皮肤选择器使用。
export { BM_THEMES }
