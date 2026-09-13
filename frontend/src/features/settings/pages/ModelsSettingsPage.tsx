import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { App as AntApp, Button, Empty, Popconfirm, Spin, Switch, Tooltip, Typography } from 'antd'
import { CheckOutlined, DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import type { ProviderLite, ProviderTemplateLite } from '../../../api'
import ProviderFormModal from '../../../components/ProviderFormModal'
import { settingsRepository } from '../../../shared/repositories'
import { useProviderController } from '../providerController'

const { Title, Text } = Typography

export function ModelsSettingsPage() {
  const { message } = AntApp.useApp()
  const controller = useProviderController()
  const [open, setOpen] = useState(false)
  const [template, setTemplate] = useState<ProviderTemplateLite | null>(null)
  const [editing, setEditing] = useState<ProviderLite | null>(null)

  useEffect(() => {
    if (controller.error) message.error(`加载失败:${String(controller.error)}`)
  }, [controller.error, message])

  const openAdd = async () => {
    try {
      const templates = await settingsRepository.providerTemplates()
      setTemplate(templates.length > 0 ? templates[templates.length - 1] : null)
    } catch {
      setTemplate(null)
    }
    setEditing(null)
    setOpen(true)
  }

  const run = async (action: () => Promise<void>, success?: string) => {
    try {
      await action()
      if (success) message.success(success)
    } catch (error) {
      message.error(String(error))
    }
  }

  const enabledOf = (providerId: number, model: string) =>
    (controller.modelsByProvider[providerId] ?? []).some((item) => item.model === model)

  const renderModelRow = (provider: ProviderLite, key: string, label: string, custom: boolean, multimodal: boolean) => {
    const enabled = enabledOf(provider.id, key)
    const current = enabled && provider.model === key
    return (
      <div className="bm-settings-model-row" key={key}>
        <div className="bm-settings-model-main">
          <Switch
            size="small"
            checked={enabled}
            aria-label={`${enabled ? '停用' : '启用'} ${label}`}
            onChange={(value) => void run(() => custom
              ? controller.toggleCustom(provider, { model: key, label, custom, multimodal }, value)
              : controller.toggleBuiltin(provider, { id: key, label, reasoning: { type: 'none' }, multimodal }, value))}
          />
          <div className="bm-settings-model-label">
            <Text ellipsis={{ tooltip: label }}>{label}</Text>
            {custom && <span className="bm-settings-model-kind">自定义</span>}
            {multimodal && <span className="bm-settings-model-kind">支持图片</span>}
          </div>
        </div>
        <div className="bm-settings-model-state">
          {enabled && (current ? <span className="bm-settings-current-model"><CheckOutlined /> 当前</span> : (
            <Button type="text" size="small" onClick={() => void run(() => controller.setCurrent(provider.id, key), `当前模型:${key}`)}>设为当前</Button>
          ))}
        </div>
      </div>
    )
  }

  const renderProviderModels = (provider: ProviderLite) => {
    const providerCatalog = controller.catalog.find((item) => item.kind === provider.kind)
    const rows: ReactNode[] = []
    for (const model of providerCatalog?.models ?? []) rows.push(renderModelRow(provider, model.id, model.label, false, model.multimodal))
    for (const model of controller.modelsByProvider[provider.id] ?? []) {
      if (model.custom) rows.push(renderModelRow(provider, model.model, model.label || model.model, true, model.multimodal))
    }
    if (rows.length === 0) rows.push(<Text type="secondary" key="empty" className="bm-settings-model-empty">尚未启用模型,点击“编辑”添加</Text>)
    return <div className="bm-settings-model-list"><div className="bm-settings-model-list-head"><span>可用模型</span><span>使用状态</span></div>{rows}</div>
  }

  return (
    <section className="bm-settings-panel" aria-labelledby="settings-models-title">
      <header className="bm-settings-page-heading">
        <div><span className="bm-settings-section-number">01</span><Title id="settings-models-title" level={3}>模型服务商</Title></div>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => void openAdd()}>添加服务商</Button>
      </header>
      {controller.loading ? <div className="bm-settings-loading"><Spin /></div> : controller.providers.length === 0 ? (
        <div className="bm-settings-empty"><Empty description="还没有配置模型服务商"><Button type="primary" onClick={() => void openAdd()}>立即配置</Button></Empty></div>
      ) : (
        <div className="bm-settings-provider-list">
          {controller.providers.map((provider, index) => (
            <article className="bm-settings-provider" key={provider.id}>
              <div className="bm-settings-provider-index">{String(index + 1).padStart(2, '0')}</div>
              <div className="bm-settings-provider-body">
                <header className="bm-settings-provider-head">
                  <div className="bm-settings-provider-identity"><Text strong ellipsis={{ tooltip: provider.name }}>{provider.name}</Text>{provider.isDefault && <span className="bm-settings-provider-default">默认</span>}</div>
                  <dl className="bm-settings-provider-meta"><div><dt>当前模型</dt><dd title={provider.model}>{provider.model || '未设置'}</dd></div><div><dt>服务地址</dt><dd title={provider.baseUrl}>{provider.baseUrl}</dd></div></dl>
                  <div className="bm-settings-provider-actions">
                    <Tooltip title="编辑服务商"><Button type="text" icon={<EditOutlined />} aria-label={`编辑 ${provider.name}`} onClick={() => { setEditing(provider); setTemplate(null); setOpen(true) }} /></Tooltip>
                    <Popconfirm title={`删除 ${provider.name}?`} onConfirm={() => void run(() => controller.deleteProvider(provider.id), '已删除')}><Tooltip title="删除服务商"><Button type="text" danger icon={<DeleteOutlined />} aria-label={`删除 ${provider.name}`} /></Tooltip></Popconfirm>
                  </div>
                </header>
                {renderProviderModels(provider)}
              </div>
            </article>
          ))}
        </div>
      )}
      <ProviderFormModal open={open} template={template} existing={editing} onCancel={() => setOpen(false)} onSaved={() => { setOpen(false); void controller.reload().catch((error) => message.error(String(error))) }} />
    </section>
  )
}
