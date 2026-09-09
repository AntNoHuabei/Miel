// 皮肤(主题)系统定义。
// 每套皮肤 = antd token(交给 ConfigProvider 的算法/令牌)+ CSS 变量
// (供自定义样式的背景/边框使用,变量以 --bm- 前缀注入 <html>)。
// 新增皮肤:在此追加一项即可,运行时热切换、设置持久化(key: theme)。

export interface BMTheme {
  /** 皮肤 id,同时作为持久化设置值 */
  id: string
  /** 显示名 */
  name: string
  /** 说明 */
  desc: string
  /** 是否暗色(决定 antd 算法) */
  dark: boolean
  /** antd 主题令牌(colorPrimary 等) */
  token: Record<string, string | number>
  /** CSS 变量映射(--bm-{key}) */
  vars: Record<string, string>
}

export const BM_THEMES: BMTheme[] = [
  {
    id: 'light',
    name: '明亮',
    desc: '经典亮色 · 默认',
    dark: false,
    token: {},
    vars: {
      'app-bg': '#f5f6f8',
      'content-bg': '#f5f6f8',
      'header-bg': '#ffffff',
      'inset-bg': '#f6f8fa',
      'border': '#f0f0f0',
      'text': '#1f2329',
      'muted': '#667085',
      'signal': '#1677ff',
      'control-radius': '8px',
    },
  },
  {
    id: 'dark',
    name: '暗夜',
    desc: '深色护眼 · 适合夜间',
    dark: true,
    token: {},
    vars: {
      'app-bg': '#0e1116',
      'content-bg': '#141821',
      'header-bg': '#1a1f2a',
      'inset-bg': '#1e2430',
      'border': '#2a3140',
      'text': '#e8edf5',
      'muted': '#98a3b5',
      'signal': '#61a8ff',
      'control-radius': '6px',
    },
  },
  {
    id: 'forest',
    name: '护眼绿',
    desc: '柔和的绿色亮色主题',
    dark: false,
    token: { colorPrimary: '#3a7d44' },
    vars: {
      'app-bg': '#eef4ee',
      'content-bg': '#eef4ee',
      'header-bg': '#ffffff',
      'inset-bg': '#f2f7f1',
      'border': '#e2ecdf',
      'text': '#203126',
      'muted': '#607264',
      'signal': '#3a7d44',
      'control-radius': '8px',
    },
  },
  {
    id: 'nebula',
    name: '极客紫',
    desc: '深色 · 紫色高亮',
    dark: true,
    token: { colorPrimary: '#8b5cf6' },
    vars: {
      'app-bg': '#0d0b16',
      'content-bg': '#151222',
      'header-bg': '#1b1730',
      'inset-bg': '#201b38',
      'border': '#2c2547',
      'text': '#f0ebff',
      'muted': '#a69fc1',
      'signal': '#a78bfa',
      'control-radius': '6px',
    },
  },
]

export const DEFAULT_THEME_ID = 'light'

export function themeById(id: string | null | undefined): BMTheme {
  return BM_THEMES.find((t) => t.id === id) ?? BM_THEMES[0]
}
