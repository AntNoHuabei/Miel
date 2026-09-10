import { useCallback, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import {
  App as AntApp,
  Button,
  Card,
  Empty,
  Flex,
  List,
  Menu,
  Popconfirm,
  Space,
  Spin,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { MenuProps } from 'antd'
import {
  BgColorsOutlined,
  BulbOutlined,
  CloudServerOutlined,
  FolderOpenOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import { SettingsService } from '../api'
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
    SettingsService.DataDir()
      .then((d) => setDataDir(d))
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

  const renderModelRow = (p: ProviderLite, key: string, label: string, custom: boolean) => {
    const on = enabledOf(p.id, key)
    const isCurrent = on && p.model === key
    return (
      <Flex key={key} align="center" justify="space-between" style={{ padding: '2px 0' }}>
        <Space size={6} style={{ minWidth: 0 }}>
          <Switch
            size="small"
            checked={on}
            onChange={(v) => {
              if (custom) void toggleCustom(p, { model: key, label, custom }, v)
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
          {custom && (
            <Tag color="orange" style={{ fontSize: 11, marginInlineEnd: 0 }}>
              自定义
            </Tag>
          )}
          <Typography.Text style={{ fontSize: 12 }} ellipsis>
            {label}
          </Typography.Text>
        </Space>
        {on &&
          (isCurrent ? (
            <Tag color="blue" style={{ fontSize: 11, marginInlineEnd: 0 }}>
              当前
            </Tag>
          ) : (
            <Button type="link" size="small" onClick={() => void setCurrent(p.id, key)}>
              设为当前
            </Button>
          ))}
      </Flex>
    )
  }

  const renderProviderModels = (p: ProviderLite) => {
    const cat = catalog.find((c) => c.kind === p.kind)
    const rows: ReactNode[] = []
    if (cat && cat.models.length > 0) {
      for (const m of cat.models) {
        rows.push(renderModelRow(p, m.id, m.label, false))
      }
    }
    for (const m of modelsMap[p.id] ?? []) {
      if (m.custom) rows.push(renderModelRow(p, m.model, m.label || m.model, true))
    }
    if (rows.length === 0) {
      rows.push(
        <Text type="secondary" key="empty" style={{ fontSize: 12 }}>
          尚未启用模型,点击“编辑”添加
        </Text>,
      )
    }
    return (
      <Flex vertical style={{ marginTop: 6, paddingLeft: 2 }}>
        {rows}
      </Flex>
    )
  }

  const menuItems: MenuProps['items'] = [
    { key: 'models', icon: <CloudServerOutlined />, label: '模型服务商' },
    { key: 'memory', icon: <BulbOutlined />, label: '记忆' },
    { key: 'appearance', icon: <BgColorsOutlined />, label: '外观与皮肤' },
    { key: 'data', icon: <FolderOpenOutlined />, label: '数据与导出' },
  ]

  return (
    <Flex style={{ height: '100%' }}>
      {/* 左侧分类 */}
      <div
        style={{
          width: 190,
          borderRight: '1px solid var(--bm-border)',
          paddingTop: 12,
          flexShrink: 0,
          background: 'var(--bm-header-bg)',
        }}
      >
        <Menu
          mode="inline"
          selectedKeys={[section]}
          items={menuItems}
          onClick={(e) => setSection(e.key as SectionKey)}
          style={{ borderInlineEnd: 'none' }}
        />
      </div>

      {/* 右侧详情 */}
      <div
        style={{
          flex: 1,
          minWidth: 0,
          overflowY: 'auto',
          padding: 24,
          background: 'var(--bm-content-bg)',
        }}
      >
        {section === 'models' && (
          <>
            <Flex justify="space-between" align="center" style={{ marginBottom: 12 }}>
              <Title level={4} style={{ margin: 0 }}>
                模型服务商
              </Title>
              <Button type="primary" icon={<PlusOutlined />} onClick={() => void openAdd()}>
                添加服务商
              </Button>
            </Flex>
            {loading ? (
              <Flex justify="center" style={{ padding: 48 }}>
                <Spin />
              </Flex>
            ) : providers.length === 0 ? (
              <Empty description="还没有配置模型服务商">
                <Button type="primary" onClick={() => void openAdd()}>
                  立即配置
                </Button>
              </Empty>
            ) : (
              <List
                bordered
                dataSource={providers}
                renderItem={(p) => (
                  <List.Item
                    actions={[
                      <Button
                        key="edit"
                        size="small"
                        onClick={() => {
                          setEditing(p)
                          setTemplate(null)
                          setOpen(true)
                        }}
                      >
                        编辑
                      </Button>,
                      <Popconfirm
                        key="del"
                        title={`删除 ${p.name}?`}
                        onConfirm={() => void handleDelete(p.id)}
                      >
                        <Button size="small" danger>
                          删除
                        </Button>
                      </Popconfirm>,
                    ]}
                  >
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <Space>
                        <Typography.Text strong>{p.name}</Typography.Text>
                        {p.isDefault && <Tag color="blue">默认</Tag>}
                        <Typography.Text type="secondary" style={{ fontSize: 11 }} ellipsis>
                          {p.model} · {p.baseUrl}
                        </Typography.Text>
                      </Space>
                      {renderProviderModels(p)}
                    </div>
                  </List.Item>
                )}
              />
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
          </>
        )}

        {section === 'appearance' && (
          <>
            <Title level={4} style={{ marginTop: 0 }}>
              外观与皮肤
            </Title>
            <Card>
              <Text type="secondary" style={{ display: 'block', marginBottom: 12 }}>
                选择界面主题,即时生效并自动保存;所有颜色跟随皮肤切换。
              </Text>
              <Flex gap={8} wrap className="bm-settings-theme-list">
                {BM_THEMES.map((t) => (
                  <Tooltip key={t.id} title={`${t.desc}(${t.dark ? '深色' : '浅色'})`}>
                    <Button
                      type="default"
                      className={`bm-settings-theme-button ${themeId === t.id ? 'is-active' : ''}`}
                      aria-pressed={themeId === t.id}
                      icon={(
                        <span
                          className="bm-theme-swatch"
                          style={{
                            backgroundColor: t.vars['sidebar-bg'],
                            borderColor: t.vars.divider,
                          }}
                        >
                          <span style={{ backgroundColor: t.vars.signal }} />
                        </span>
                      )}
                      onClick={() => void setTheme(t.id)}
                    >
                      {t.name}
                    </Button>
                  </Tooltip>
                ))}
              </Flex>
            </Card>
          </>
        )}

        {section === 'memory' && <MemorySettingsPanel />}

        {section === 'data' && (
          <>
            <Title level={4} style={{ marginTop: 0 }}>
              数据与导出
            </Title>
            <Card>
              <Space direction="vertical" size={8} style={{ width: '100%' }}>
                <Text type="secondary" style={{ fontSize: 12, wordBreak: 'break-all' }}>
                  应用数据目录(截图、周报、文档、表格、skills 均在此):
                  <br />
                  {dataDir || '读取中…'}
                </Text>
                <Button
                  icon={<FolderOpenOutlined />}
                  onClick={() => void SettingsService.OpenDataDir()}
                >
                  打开数据目录
                </Button>
              </Space>
            </Card>
          </>
        )}
      </div>
    </Flex>
  )
}
