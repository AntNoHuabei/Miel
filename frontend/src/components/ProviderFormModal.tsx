import { useEffect, useMemo, useRef, useState } from 'react'
import {
  App as AntApp,
  Button,
  Checkbox,
  Flex,
  Form,
  Input,
  Modal,
  Radio,
  Select,
  Space,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { DeleteOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { providerEditorController } from '../features/settings/providerEditorController'
import type {
  CatalogModelLite,
  CatalogProviderLite,
  DiscoveredModelLite,
  ProviderLite,
  ProviderModelInputLite,
  ProviderTemplateLite,
} from '../api'

interface Props {
  open: boolean
  template: ProviderTemplateLite | null
  existing: ProviderLite | null
  onCancel: () => void
  onSaved: () => void
}

interface FormValues {
  name: string
  kind: string
  baseUrl: string
  apiKey: string
  model: string
  multimodal: boolean
  isDefault: boolean
}

const reasoningTag: Record<string, { text: string; color: string }> = {
  toggle: { text: '可开关思考', color: 'blue' },
  effort: { text: '思考可调档位', color: 'purple' },
  always: { text: '思考常开', color: 'green' },
  none: { text: '无思考开关', color: 'default' },
}

const normalizeProviderKind = (kind: string) =>
  kind.trim().toLowerCase() === 'volcengine-coding' ? 'volcengine-plan' : kind.trim().toLowerCase()

const normalizeProviderBaseURL = (kind: string, baseURL: string) => {
  const value = baseURL.trim()
  if (
    kind.trim().toLowerCase() === 'volcengine-coding' &&
    /^https:\/\/ark\.cn-beijing\.volces\.com\/api\/coding\/v3\/?$/i.test(value)
  ) {
    return 'https://ark.cn-beijing.volces.com/api/plan/v3'
  }
  return value
}

// 模型启用编辑:内置目录模型用开关启用/停用,自定义模型手输加入(可删除);
// 启用的模型中以 Radio 指定“当前使用模型”。
export default function ProviderFormModal({
  open,
  template,
  existing,
  onCancel,
  onSaved,
}: Props) {
  const [form] = Form.useForm<FormValues>()
  const { message } = AntApp.useApp()
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [fetchingModels, setFetchingModels] = useState(false)
  const [catalog, setCatalog] = useState<CatalogProviderLite[]>([])
  const [remoteModels, setRemoteModels] = useState<DiscoveredModelLite[]>([])
  const autoFetchedProviderRef = useRef('')

  // 启用模型集合(含内置与自定义)
  const [enabled, setEnabled] = useState<ProviderModelInputLite[]>([])
  const [customName, setCustomName] = useState('')

  const kind = normalizeProviderKind(
    Form.useWatch('kind', form) ?? template?.kind ?? existing?.kind ?? 'custom',
  )
  const currentModel = Form.useWatch('model', form) ?? ''
  const normalizedKind = kind
  const usesDiscoveredCapabilities = normalizedKind === 'herdsman' || normalizedKind === 'openrouter'
  const canDiscoverModels = usesDiscoveredCapabilities || normalizedKind === 'custom'
  const requiresAPIKey = normalizedKind === 'openrouter' || normalizedKind === 'volcengine-plan'

  useEffect(() => {
    if (!open) return
    providerEditorController.loadCatalog()
      .then(setCatalog)
      .catch((err) => message.warning(`模型目录加载失败:${String(err)}`))
  }, [open, message])

  const catProvider = catalog.find((c) => normalizeProviderKind(c.kind) === normalizedKind)
  const builtinModels = catProvider?.models ?? []

  const enabledMap = useMemo(() => {
    const m = new Map<string, ProviderModelInputLite>()
    for (const e of enabled) m.set(e.model, e)
    return m
  }, [enabled])

  // 远程发现只补充当前服务商返回且尚未启用的模型，不混入其它服务商目录。
  const extraModelOptions = useMemo(() => {
    const seen = new Set(enabled.map((e) => e.model))
    const builtin = new Set(builtinModels.map((m) => m.id))
    const list: { value: string; label: string; modelLabel: string; multimodal: boolean; supportsTools: boolean }[] = []
    for (const model of remoteModels) {
      if (!seen.has(model.id) && !builtin.has(model.id)) {
        const labels = [model.id]
        if (model.multimodal) labels.push('支持图片')
        if (normalizedKind === 'openrouter' && !model.supportsTools) labels.push('纯聊天')
        list.push({
          value: model.id,
          label: labels.join(' · '),
          modelLabel: model.id,
          multimodal: model.multimodal,
          supportsTools: model.supportsTools,
        })
        seen.add(model.id)
      }
    }
    return list
  }, [builtinModels, enabled, normalizedKind, remoteModels])

  // 打开时初始化:编辑回填启用集;模板默认启用首个内置模型
  useEffect(() => {
    if (!open) return
    setEnabled([])
    setCustomName('')
    setRemoteModels([])
    if (existing) {
      form.setFieldsValue({
        name: existing.name,
        kind: normalizeProviderKind(existing.kind),
        baseUrl: normalizeProviderBaseURL(existing.kind, existing.baseUrl),
        apiKey: existing.apiKey,
        model: existing.model,
        multimodal: existing.multimodal,
        isDefault: existing.isDefault,
      })
      providerEditorController.loadEnabledModels(existing.id)
        .then((rows) => {
          const list = rows.map((r) => ({
            model: r.model,
            label: r.label,
            custom: r.custom,
            multimodal: r.multimodal,
          }))
          setEnabled(
            list.length > 0
              ? list
              : [{ model: existing.model, custom: false, multimodal: existing.multimodal }],
          )
        })
        .catch(() =>
          setEnabled([{ model: existing.model, custom: false, multimodal: existing.multimodal }]),
        )
    } else if (template) {
      const models = catalog.find(
        (c) => normalizeProviderKind(c.kind) === normalizeProviderKind(template.kind),
      )?.models ?? []
      const first = template.model || models[0]?.id || ''
      form.setFieldsValue({
        name: template.name,
        kind: template.kind,
        baseUrl: template.baseUrl,
        model: first,
        apiKey: '',
        multimodal: template.multimodal,
        isDefault: true,
      })
      const selected = models.find((model) => model.id === first)
      setEnabled(
        first
          ? [{ model: first, label: selected?.label ?? first, custom: false, multimodal: selected?.multimodal }]
          : [],
      )
    } else {
      form.setFieldsValue({
        name: '',
        kind: 'custom',
        baseUrl: '',
        model: '',
        apiKey: '',
        multimodal: false,
        isDefault: false,
      })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, template, existing, form, catalog])

  // 切换服务商类型:自动带默认接口与首个内置模型
  const onKindChange = (k: string) => {
    const cat = catalog.find((c) => c.kind === k)
    const models = cat?.models ?? []
    const first = models[0]?.id ?? ''
    const selected = models.find((model) => model.id === first)
    form.setFieldsValue(
      cat
        ? { kind: k, name: cat.name, baseUrl: cat.baseUrl, apiKey: '', model: first, multimodal: selected?.multimodal ?? false }
        : { kind: k, name: '自定义服务商', baseUrl: '', apiKey: '', model: '', multimodal: false },
    )
    setEnabled(
      first
        ? [{ model: first, label: selected?.label ?? first, custom: false, multimodal: selected?.multimodal }]
        : [],
    )
    setRemoteModels([])
    setCustomName('')
  }

  // 内置模型开关
  const toggleBuiltin = (m: CatalogModelLite, on: boolean) => {
    if (!on) {
      if (enabled.length <= 1) {
        message.warning('至少保留一个可用模型')
        return
      }
      const rest = enabled.filter((e) => e.model !== m.id)
      setEnabled(rest)
      if (currentModel === m.id) form.setFieldsValue({ model: rest[0].model })
      return
    }
    setEnabled((prev) =>
      prev.some((e) => e.model === m.id)
        ? prev
        : [...prev, { model: m.id, label: m.label, custom: false, multimodal: m.multimodal }],
    )
    if (!currentModel) form.setFieldsValue({ model: m.id })
  }

  // 添加“其它模型”:可从目录下拉选择,也可输入未收录的模型名
  const addCustom = (name: string, label?: string, isCustom = true, multimodal = false) => {
    const n = (name || '').trim()
    if (!n) {
      message.warning('请选择或输入模型名')
      return
    }
    if (enabled.some((e) => e.model.toLowerCase() === n.toLowerCase())) {
      message.warning('该模型已启用')
      return
    }
    const builtin = builtinModels.find((model) => model.id.toLowerCase() === n.toLowerCase())
    setEnabled((prev) => [
      ...prev,
      {
        model: builtin?.id ?? n,
        label: builtin?.label ?? label ?? n,
        custom: builtin ? false : isCustom,
        multimodal: builtin?.multimodal ?? multimodal,
      },
    ])
    if (!currentModel) form.setFieldsValue({ model: builtin?.id ?? n })
    setCustomName('')
  }

  const removeModel = (model: string) => {
    if (enabled.length <= 1) {
      message.warning('至少保留一个可用模型')
      return
    }
    const rest = enabled.filter((e) => e.model !== model)
    setEnabled(rest)
    if (currentModel === model) form.setFieldsValue({ model: rest[0].model })
  }

  const setModelMultimodal = (model: string, multimodal: boolean) => {
    setEnabled((prev) =>
      prev.map((item) => (item.model === model ? { ...item, multimodal } : item)),
    )
  }

  const toInput = (v: FormValues) => {
    const providerKind = normalizeProviderKind(v.kind ?? '')
    const selectedModel = (v.model ?? '').trim()
    const discovered = remoteModels.find(
      (model) => model.id.toLowerCase() === selectedModel.toLowerCase(),
    )
    const selected = enabled.find(
      (model) => model.model.toLowerCase() === selectedModel.toLowerCase(),
    )
    const existingCapability =
      usesDiscoveredCapabilities &&
      !!existing &&
      normalizeProviderKind(existing?.kind ?? '') === normalizedKind &&
      existing.model.toLowerCase() === selectedModel.toLowerCase()
        ? existing.multimodal
        : false
    return {
      id: existing?.id ?? 0,
      name: (v.name ?? '').trim(),
      kind: providerKind,
      baseUrl: normalizeProviderBaseURL(existing?.kind ?? providerKind, v.baseUrl ?? ''),
      apiKey: (v.apiKey ?? '').trim(),
      model: selectedModel,
      multimodal:
        providerKind === 'herdsman' || providerKind === 'openrouter'
          ? (discovered?.multimodal ?? selected?.multimodal ?? existingCapability)
          : (selected?.multimodal ?? !!v.multimodal),
      // 默认服务商由“当前模型切换”产生;后端在无默认时自动为第一个服务商设默认
      isDefault: false,
      // 完整化每个启用模型条目(bindings 类型要求 label/custom 必填)
      models: enabled.map((e) => ({
        model: e.model,
        label: e.label ?? '',
        custom: !!e.custom,
        multimodal: !!e.multimodal,
      })),
    }
  }

  const handleFetchModels = async () => {
    try {
      await form.validateFields(['baseUrl'])
      setFetchingModels(true)
      const values = form.getFieldsValue(true) as FormValues
      const list = await providerEditorController.discoverModels(toInput(values))
      setRemoteModels(list)
      if (['herdsman', 'openrouter'].includes((values.kind ?? '').trim().toLowerCase())) {
        setEnabled((prev) =>
          prev.map((item) => {
            const capability = list.find(
              (model) => model.id.toLowerCase() === item.model.toLowerCase(),
            )
            return capability ? { ...item, multimodal: capability.multimodal } : item
          }),
        )
      }
      const current = (values.model ?? '').trim().toLowerCase()
      const currentCapability = list.find((model) => model.id.toLowerCase() === current)
      if (currentCapability) form.setFieldValue('multimodal', currentCapability.multimodal)
      message.success(`已获取 ${list.length} 个模型`)
    } catch (err) {
      message.error(`获取模型列表失败:${String(err)}`)
    } finally {
      setFetchingModels(false)
    }
  }

  useEffect(() => {
    if (!open || !usesDiscoveredCapabilities) {
      autoFetchedProviderRef.current = ''
      return
    }
    if (!catalog.some((provider) => provider.kind.toLowerCase() === normalizedKind)) return

    const baseUrl = String(form.getFieldValue('baseUrl') ?? '').trim()
    if (!baseUrl) return
    const requestKey = `${normalizedKind}:${existing?.id ?? 0}:${baseUrl}`
    if (autoFetchedProviderRef.current === requestKey) return

    autoFetchedProviderRef.current = requestKey
    void handleFetchModels()
    // 修改 Base URL 后保留手动获取语义,不把输入过程加入自动请求依赖。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [catalog, existing?.id, form, normalizedKind, open, usesDiscoveredCapabilities])

  const handleTest = async () => {
    try {
      const v = await form.validateFields()
      setTesting(true)
      const res = await providerEditorController.testConnection(toInput(v))
      if (res.ok) {
        message.success(`连接成功(${res.latencyMs}ms):${res.message}`)
      } else {
        message.error(`连接失败:${res.message}`)
      }
    } catch (err) {
      if (err instanceof Error) message.error('请先填写完整配置')
      else message.error(String(err))
    } finally {
      setTesting(false)
    }
  }

  const handleSave = async () => {
    try {
      const v = await form.validateFields()
      if (enabled.length === 0) {
        message.warning('请至少启用一个模型')
        return
      }
      let cur = (v.model ?? '').trim()
      if (!cur || !enabled.some((e) => e.model === cur)) {
        cur = enabled[0].model
        form.setFieldsValue({ model: cur })
      }
      setSaving(true)
      await providerEditorController.save(toInput({ ...v, model: cur }))
      message.success('已保存模型服务商配置')
      onSaved()
    } catch (err) {
      if (err instanceof Error) message.error(String(err.message ?? err))
      else if (typeof err === 'object' && err && 'errorFields' in err) {
        message.error('请填写完整配置')
      } else {
        message.error(String(err))
      }
    } finally {
      setSaving(false)
    }
  }

  const kindOptions = catalog.map((c) => ({ value: c.kind, label: c.name }))
  if (!kindOptions.some((option) => option.value === 'custom')) {
    kindOptions.push({ value: 'custom', label: '自定义(OpenAI 兼容)' })
  }

  return (
    <Modal
      title={existing ? '编辑模型服务商' : '配置模型服务商'}
      open={open}
      onCancel={onCancel}
      width={620}
      destroyOnHidden
      style={{ top: '4vh' }}
      styles={{
        body: { maxHeight: 'calc(92vh - 160px)', overflowY: 'auto' },
      }}
      footer={
        <Space>
          <Button onClick={onCancel}>取消</Button>
          <Button loading={testing} onClick={() => void handleTest()}>
            测试连接
          </Button>
          <Button type="primary" loading={saving} onClick={() => void handleSave()}>
            保存
          </Button>
        </Space>
      }
    >
      <Form<FormValues> form={form} layout="vertical" initialValues={{ kind: 'custom' }}>
        <Form.Item label="服务商类型" name="kind" rules={[{ required: true }]}>
          <Select
            options={kindOptions}
            onChange={(k) => onKindChange(k as string)}
            popupMatchSelectWidth={false}
          />
        </Form.Item>

        <Form.Item label="显示名称" name="name" rules={[{ required: true, message: '请输入名称' }]}>
          <Input placeholder="例如: DeepSeek 主账号" />
        </Form.Item>

        <Form.Item label="API Base URL" name="baseUrl" rules={[{ required: true, message: '请输入接口地址' }]}>
          <Input
            placeholder="接口地址,切换服务商自动填充"
            onChange={() => setRemoteModels([])}
          />
        </Form.Item>

        <Form.Item
          label="API Key"
          name="apiKey"
          dependencies={['kind']}
          rules={[
            {
              validator: (_, value) =>
                !requiresAPIKey ||
                (existing?.id && normalizeProviderKind(existing.kind) === normalizedKind) ||
                String(value ?? '').trim()
                  ? Promise.resolve()
                  : Promise.reject(new Error('请输入 API Key')),
            },
          ]}
        >
          <Input.Password placeholder="sk-…(留空则沿用已有配置)" autoComplete="off" />
        </Form.Item>

        <Form.Item
          label={
            <Space size={8}>
              <span>启用的模型</span>
              {!canDiscoverModels && catProvider ? (
                <Tag color="blue" style={{ marginInlineEnd: 0 }}>
                  预设 {builtinModels.length} 个
                </Tag>
              ) : (
                <Button
                  type="link"
                  size="small"
                  icon={<ReloadOutlined />}
                  loading={fetchingModels}
                  onClick={() => void handleFetchModels()}
                >
                  获取模型列表
                </Button>
              )}
            </Space>
          }
          name="model"
          tooltip="内置模型用开关启用;自定义模型输入后点“启用”加入。以“当前”指定对话默认使用哪个。"
          style={{ marginBottom: 8 }}
        >
          <Radio.Group style={{ width: '100%' }}>
            <Flex vertical gap={6}>
              {builtinModels.map((m) => {
                const on = !!enabledMap.get(m.id)
                const tag = reasoningTag[m.reasoning.type]
                const cur = on && m.id === currentModel
                return (
                  <Flex key={m.id} align="center" justify="space-between" gap={8}>
                    <Space size={6}>
                      <Switch size="small" checked={on} onChange={(v) => toggleBuiltin(m, v)} />
                      <Typography.Text style={{ fontSize: 13 }}>{m.label}</Typography.Text>
                      {tag && (
                        <Tag color={tag.color} style={{ fontSize: 11, marginInlineEnd: 0 }}>
                          {tag.text}
                        </Tag>
                      )}
                      {m.multimodal && (
                        <Tag color="blue" style={{ fontSize: 11, marginInlineEnd: 0 }}>
                          支持图片
                        </Tag>
                      )}
                    </Space>
                    {on && (
                      <Radio value={m.id}>
                        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                          {cur ? '当前' : '设为当前'}
                        </Typography.Text>
                      </Radio>
                    )}
                  </Flex>
                )
              })}

              {/* 其它/自定义模型 */}
              {enabled
                .filter(
                  (e) =>
                    e.custom ||
                    !builtinModels.some((model) => model.id.toLowerCase() === e.model.toLowerCase()),
                )
                .map((e) => {
                  const discovered = remoteModels.find(
                    (model) => model.id.toLowerCase() === e.model.toLowerCase(),
                  )
                  const modelMultimodal = discovered?.multimodal ?? !!e.multimodal
                  const usesAutomaticCapability = usesDiscoveredCapabilities && !!discovered
                  const pureChat = normalizedKind === 'openrouter' && discovered?.supportsTools === false
                  return (
                    <Flex key={e.model} align="center" justify="space-between" gap={8}>
                      <Space size={6} wrap>
                        <Tag color="orange" style={{ fontSize: 11, marginInlineEnd: 0 }}>
                          自定义
                        </Tag>
                        <Typography.Text style={{ fontSize: 13 }}>
                          {e.label && e.label !== e.model ? `${e.label}` : e.model}
                        </Typography.Text>
                        {usesAutomaticCapability && modelMultimodal && (
                          <Tag color="blue" style={{ fontSize: 11, marginInlineEnd: 0 }}>
                            支持图片
                          </Tag>
                        )}
                        {pureChat && (
                          <Tag color="default" style={{ fontSize: 11, marginInlineEnd: 0 }}>
                            纯聊天
                          </Tag>
                        )}
                        {!usesAutomaticCapability && (
                          <Checkbox
                            checked={modelMultimodal}
                            onChange={(event) => setModelMultimodal(e.model, event.target.checked)}
                          >
                            图片输入
                          </Checkbox>
                        )}
                      </Space>
                      <Space size={2}>
                        <Radio value={e.model}>
                          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                            {e.model === currentModel ? '当前' : '设为当前'}
                          </Typography.Text>
                        </Radio>
                        <Button
                          type="text"
                          size="small"
                          icon={<DeleteOutlined />}
                          onClick={() => removeModel(e.model)}
                        />
                      </Space>
                    </Flex>
                  )
                })}

              {remoteModels.length > 0 && (
                <Select
                  value={undefined}
                  onChange={(v) => {
                    const opt = extraModelOptions.find((o) => o.value === v)
                    addCustom(String(v), opt?.modelLabel, true, opt?.multimodal)
                  }}
                  options={extraModelOptions}
                  placeholder="选择服务商返回的模型…"
                  showSearch
                  popupMatchSelectWidth={false}
                  style={{ width: '100%' }}
                  notFoundContent="没有更多可启用的模型"
                />
              )}

              <Space.Compact block>
                <Input
                  value={customName}
                  placeholder="输入自定义模型 ID"
                  onChange={(event) => setCustomName(event.target.value)}
                  onPressEnter={() => addCustom(customName)}
                />
                <Tooltip title="添加自定义模型">
                  <Button
                    icon={<PlusOutlined />}
                    aria-label="添加自定义模型"
                    onClick={() => addCustom(customName)}
                  />
                </Tooltip>
              </Space.Compact>
            </Flex>
          </Radio.Group>
        </Form.Item>

      </Form>
    </Modal>
  )
}
