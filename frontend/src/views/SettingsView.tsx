import { useCallback, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import {
  App as AntApp,
  Button,
  Empty,
  Popconfirm,
  Spin,
  Switch,
  Tooltip,
  Typography,
} from 'antd'
import {
  BgColorsOutlined,
  BulbOutlined,
  CheckOutlined,
  CloudServerOutlined,
  DeleteOutlined,
  EditOutlined,
  FolderOpenOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import { DirectoryService, SettingsService } from '../api'
import type {
  CatalogModelLite,
  CatalogProviderLite,
  ProviderLite,
  ProviderModelLite,
  ProviderTemplateLite,
} from '../api'
import ProviderFormModal from '../components/ProviderFormModal'
import MemorySettingsPanel from '../components/MemorySettingsPanel'
import { BM_THEMES, useBMTheme } from '../theme/ThemeContext'

const { Title, Text } = Typography

type SectionKey = 'models' | 'memory' | 'appearance' | 'data'

// 设置页:左侧大分类导航,右侧对应详细设置。
export default function SettingsView() {
  const { message } = AntApp.useApp()
  const { themeId, setTheme } = useBMTheme()
  const [section, setSection] = useState<SectionKey>('models')
  const [dataDir, setDataDir] = useState('')
  const [providers, setProviders] = useState<ProviderLite[]>([])
  const [catalog, setCatalog] = useState<CatalogProviderLite[]>([])
  const [modelsMap, setModelsMap] = useState<Record<number, ProviderModelLite[]>>({})
  const [loading, setLoading] = useState(true)
  const [open, setOpen] = useState(false)
  const [template, setTemplate] = useState<ProviderTemplateLite | null>(null)
  const [editing, setEditing] = useState<ProviderLite | null>(null)

  const loadAll = useCallback(async () => {
    try {
      const [ps, cat] = await Promise.all([
        SettingsService.ListProviders() as unknown as Promise<ProviderLite[]>,
        SettingsService.ModelCatalog() as unknown as Promise<CatalogProviderLite[]>,
      ])
      setProviders(ps ?? [])
      setCatalog(cat ?? [])
      const map: Record<number, ProviderModelLite[]> = {}
      for (const p of ps ?? []) {
        try {
          const rows = (await SettingsService.ProviderModels(p.id)) as unknown as ProviderModelLite[]
          map[p.id] = rows ?? []
        } catch {
          map[p.id] = []
        }
      }
      setModelsMap(map)
    } catch (err) {
      message.error(`加载失败:${String(err)}`)
    } finally {
      setLoading(false)
    }
  }, [message])

  useEffect(() => {
    void loadAll()
  }, [loadAll])

  useEffect(() => {
    DirectoryService.Paths()
      .then((paths) => setDataDir(paths.root))
      .catch(() => undefined)
  }, [])

  const openAdd = async () => {
    try {
      const tpl = (await SettingsService.ProviderTemplates()) as unknown as ProviderTemplateLite[]
      setTemplate(tpl.length > 0 ? tpl[tpl.length - 1] : null)
    } catch {
      setTemplate(null)
    }
    setEditing(null)
    setOpen(true)
  }

  const handleDelete = async (id: number) => {
    try {
      await SettingsService.DeleteProvider(id)
      message.success('已删除')
      void loadAll()
    } catch (err) {
      message.error(String(err))
    }
  }

  const toggleBuiltin = async (p: ProviderLite, m: CatalogModelLite, on: boolean) => {
    try {
      if (on) await SettingsService.EnableModel(p.id, m.id, m.label, false)
      else await SettingsService.DisableModel(p.id, m.id)
      await loadAll()
    } catch (err) {
      message.error(String(err))
    }
  }

  const toggleCustom = async (p: ProviderLite, m: ProviderModelLite, on: boolean) => {
    try {
      if (on) await SettingsService.EnableModel(p.id, m.model, m.label, true)
      else await SettingsService.DisableModel(p.id, m.model)
      await loadAll()
    } catch (err) {
      message.error(String(err))
    }
  }

  const setCurrent = async (pid: number, model: string) => {
    try {
      await SettingsService.SetProviderModel(pid, model)
      message.success(`当前模型:${model}`)
      await loadAll()
    } catch (err) {
      message.error(String(err))
    }
  }

  const enabledOf = (pid: number, model: string) =>
    (modelsMap[pid] ?? []).some((m) => m.model === model)

  const renderModelRow = (
    p: ProviderLite,
    key: string,
    label: string,
    custom: boolean,
    multimodal: boolean,
  ) => {
    const on = enabledOf(p.id, key)
    const isCurrent = on && p.model === key
    return (
      <div className="bm-settings-model-row" key={key}>
        <div className="bm-settings-model-main">
          <Switch
            size="small"
            checked={on}
            aria-label={`${on ? '停用' : '启用'} ${label}`}
            onChange={(v) => {
              if (custom) void toggleCustom(p, { model: key, label, custom, multimodal }, v)
              else {
                const cm: CatalogModelLite = {
                  id: key,
                  label,
                  reasoning: { type: 'none' },
                  multimodal: false,
                }
                void toggleBuiltin(p, cm, v)
              }
            }}
          />
          <div className="bm-settings-model-label">
            <Text ellipsis={{ tooltip: label }}>{label}</Text>
            {custom && <span className="bm-settings-model-kind">自定义</span>}
            {multimodal && <span className="bm-settings-model-kind">支持图片</span>}
          </div>
        </div>
        <div className="bm-settings-model-state">
          {on && (isCurrent ? (
            <span className="bm-settings-current-model"><CheckOutlined /> 当前</span>
          ) : (
            <Button type="text" size="small" onClick={() => void setCurrent(p.id, key)}>
              设为当前
            </Button>
          ))}
        </div>
      </div>
    )
  }

  const renderProviderModels = (p: ProviderLite) => {
    const cat = catalog.find((c) => c.kind === p.kind)
    const rows: ReactNode[] = []
    if (cat && cat.models.length > 0) {
      for (const m of cat.models) {
        rows.push(renderModelRow(p, m.id, m.label, false, m.multimodal))
      }
    }
    for (const m of modelsMap[p.id] ?? []) {
      if (m.custom) rows.push(renderModelRow(p, m.model, m.label || m.model, true, m.multimodal))
    }
    if (rows.length === 0) {
      rows.push(
        <Text type="secondary" key="empty" className="bm-settings-model-empty">
          尚未启用模型,点击“编辑”添加
        </Text>,
      )
    }
    return (
      <div className="bm-settings-model-list">
        <div className="bm-settings-model-list-head">
          <span>可用模型</span>
          <span>使用状态</span>
        </div>
        {rows}
      </div>
    )
  }

  const menuItems: Array<{ key: SectionKey; icon: ReactNode; label: string }> = [
    { key: 'models', icon: <CloudServerOutlined />, label: '模型服务商' },
    { key: 'memory', icon: <BulbOutlined />, label: '记忆' },
    { key: 'appearance', icon: <BgColorsOutlined />, label: '外观与皮肤' },
    { key: 'data', icon: <FolderOpenOutlined />, label: '数据与导出' },
  ]

  return (
    <div className="bm-settings-layout">
      <aside className="bm-settings-nav" aria-label="设置分类">
        <nav className="bm-settings-nav-list">
          {menuItems.map((item, index) => (
            <Button
              type="text"
              key={item.key}
              className={`bm-settings-nav-item ${section === item.key ? 'is-active' : ''}`}
              aria-current={section === item.key ? 'page' : undefined}
              onClick={() => setSection(item.key)}
            >
              <span className="bm-settings-nav-index">{String(index + 1).padStart(2, '0')}</span>
              <span className="bm-settings-nav-icon">{item.icon}</span>
              <span className="bm-settings-nav-label">{item.label}</span>
            </Button>
          ))}
        </nav>
      </aside>

      <main className="bm-settings-main">
        {section === 'models' && (
          <section className="bm-settings-panel" aria-labelledby="settings-models-title">
            <header className="bm-settings-page-heading">
              <div>
                <span className="bm-settings-section-number">01</span>
                <Title id="settings-models-title" level={3}>模型服务商</Title>
              </div>
              <Button type="primary" icon={<PlusOutlined />} onClick={() => void openAdd()}>
                添加服务商
              </Button>
            </header>

            {loading ? (
              <div className="bm-settings-loading"><Spin /></div>
            ) : providers.length === 0 ? (
              <div className="bm-settings-empty">
                <Empty description="还没有配置模型服务商">
                  <Button type="primary" onClick={() => void openAdd()}>立即配置</Button>
                </Empty>
              </div>
            ) : (
              <div className="bm-settings-provider-list">
                {providers.map((p, index) => (
                  <article className="bm-settings-provider" key={p.id}>
                    <div className="bm-settings-provider-index">
                      {String(index + 1).padStart(2, '0')}
                    </div>
                    <div className="bm-settings-provider-body">
                      <header className="bm-settings-provider-head">
                        <div className="bm-settings-provider-identity">
                          <Text strong ellipsis={{ tooltip: p.name }}>{p.name}</Text>
                          {p.isDefault && <span className="bm-settings-provider-default">默认</span>}
                        </div>
                        <dl className="bm-settings-provider-meta">
                          <div>
                            <dt>当前模型</dt>
                            <dd title={p.model}>{p.model || '未设置'}</dd>
                          </div>
                          <div>
                            <dt>服务地址</dt>
                            <dd title={p.baseUrl}>{p.baseUrl}</dd>
                          </div>
                        </dl>
                        <div className="bm-settings-provider-actions">
                          <Tooltip title="编辑服务商">
                            <Button
                              type="text"
                              icon={<EditOutlined />}
                              aria-label={`编辑 ${p.name}`}
                              onClick={() => {
                                setEditing(p)
                                setTemplate(null)
                                setOpen(true)
                              }}
                            />
                          </Tooltip>
                          <Popconfirm
                            title={`删除 ${p.name}?`}
                            onConfirm={() => void handleDelete(p.id)}
                          >
                            <Tooltip title="删除服务商">
                              <Button
                                type="text"
                                danger
                                icon={<DeleteOutlined />}
                                aria-label={`删除 ${p.name}`}
                              />
                            </Tooltip>
                          </Popconfirm>
                        </div>
                      </header>
                      {renderProviderModels(p)}
                    </div>
                  </article>
                ))}
              </div>
            )}
          </section>
        )}

        {section === 'appearance' && (
          <section className="bm-settings-panel" aria-labelledby="settings-appearance-title">
            <header className="bm-settings-page-heading">
              <div>
                <span className="bm-settings-section-number">03</span>
                <Title id="settings-appearance-title" level={3}>外观与皮肤</Title>
              </div>
            </header>
            <div className="bm-settings-appearance-list">
              {BM_THEMES.map((theme, index) => {
                const selected = themeId === theme.id
                return (
                  <button
                    type="button"
                    className={`bm-settings-appearance-row ${selected ? 'is-active' : ''}`}
                    aria-pressed={selected}
                    key={theme.id}
                    onClick={() => void setTheme(theme.id)}
                  >
                    <span className="bm-settings-theme-index">{String(index + 1).padStart(2, '0')}</span>
                    <span
                      className="bm-settings-theme-swatch"
                      style={{
                        backgroundColor: theme.vars['sidebar-bg'],
                        borderColor: theme.vars.divider,
                      }}
                    >
                      <span style={{ backgroundColor: theme.vars.signal }} />
                    </span>
                    <span className="bm-settings-theme-copy">
                      <strong>{theme.name}</strong>
                      <small>{theme.desc}</small>
                    </span>
                    <span className="bm-settings-theme-mode">{theme.dark ? '深色' : '浅色'}</span>
                    <span className="bm-settings-theme-check" aria-hidden="true">
                      {selected && <CheckOutlined />}
                    </span>
                  </button>
                )
              })}
            </div>
          </section>
        )}

        {section === 'memory' && <MemorySettingsPanel />}

        {section === 'data' && (
          <section className="bm-settings-panel" aria-labelledby="settings-data-title">
            <header className="bm-settings-page-heading">
              <div>
                <span className="bm-settings-section-number">04</span>
                <Title id="settings-data-title" level={3}>数据与导出</Title>
              </div>
            </header>
            <div className="bm-settings-data-band">
              <span className="bm-settings-data-index">01</span>
              <div className="bm-settings-data-copy">
                <Text strong>应用数据目录</Text>
                <Text type="secondary">截图、周报、文档、表格和技能文件保存在此目录</Text>
                <code>{dataDir || '读取中...'}</code>
              </div>
              <Button icon={<FolderOpenOutlined />} onClick={() => void DirectoryService.OpenDataDir()}>
                打开目录
              </Button>
            </div>
          </section>
        )}

        <ProviderFormModal
          open={open}
          template={template}
          existing={editing}
          onCancel={() => setOpen(false)}
          onSaved={() => {
            setOpen(false)
            void loadAll()
          }}
        />
      </main>
    </div>
  )
}
