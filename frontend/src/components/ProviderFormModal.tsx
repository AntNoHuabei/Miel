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
  Typography,
} from 'antd'
import { DeleteOutlined, ReloadOutlined } from '@ant-design/icons'
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
  const autoFetchedHerdsmanRef = useRef('')

  // 启用模型集合(含内置与自定义)
  const [enabled, setEnabled] = useState<ProviderModelInputLite[]>([])
  const [customName, setCustomName] = useState('')

  const kind = Form.useWatch('kind', form) ?? template?.kind ?? existing?.kind ?? 'custom'
  const currentModel = Form.useWatch('model', form) ?? ''

  useEffect(() => {
    if (!open) return
    providerEditorController.loadCatalog()
      .then(setCatalog)
      .catch((err) => message.warning(`模型目录加载失败:${String(err)}`))
  }, [open, message])

  const catProvider = catalog.find((c) => c.kind === kind)
  const builtinModels = catProvider?.models ?? []

  const enabledMap = useMemo(() => {
    const m = new Map<string, ProviderModelInputLite>()
    for (const e of enabled) m.set(e.model, e)
    return m
  }, [enabled])

  // 添加模型下拉的候选:本服务商内置(未启用)优先,再补其它目录模型
  const extraModelOptions = useMemo(() => {
    const seen = new Set(enabled.map((e) => e.model))
    const builtin = new Set(builtinModels.map((m) => m.id))
    const list: { value: string; label: string; modelLabel: string; multimodal: boolean }[] = []
    for (const model of remoteModels) {
      if (!seen.has(model.id) && !builtin.has(model.id)) {
        list.push({
          value: model.id,
          label: model.multimodal ? `${model.id} · 支持图片` : model.id,
          modelLabel: model.id,
          multimodal: model.multimodal,
        })
        seen.add(model.id)
      }
    }
    if (builtinModels.length === 0) {
      for (const c of catalog) {
        if (c.kind === kind) continue
        for (const m of c.models) {
          if (!seen.has(m.id)) {
            list.push({
              value: m.id,
              label: `${c.name} · ${m.label}`,
              modelLabel: m.label,
              multimodal: m.multimodal,
            })
            seen.add(m.id)
          }
        }
      }
    }
    return list
  }, [builtinModels, catalog, enabled, kind, remoteModels])

  // 打开时初始化:编辑回填启用集;模板默认启用首个内置模型
  useEffect(() => {
    if (!open) return
    setEnabled([])
    setCustomName('')
    setRemoteModels([])
    if (existing) {
      form.setFieldsValue({
        name: existing.name,
        kind: existing.kind,
        baseUrl: existing.baseUrl,
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
      const models = catalog.find((c) => c.kind === template.kind)?.models ?? []
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
    form.setFieldsValue(
      cat
        ? { kind: k, name: cat.name, baseUrl: cat.baseUrl, model: first }
        : { kind: k, name: '自定义服务商', baseUrl: '', model: '' },
    )
    const selected = models.find((model) => model.id === first)
    setEnabled(
      first
        ? [{ model: first, label: selected?.label ?? first, custom: false, multimodal: selected?.multimodal }]
        : [],
    )
    setRemoteModels([])
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
    if (enabled.some((e) => e.model === n)) {
      message.warning('该模型已启用')
      return
    }
    setEnabled((prev) => [
      ...prev,
      { model: n, label: label ?? n, custom: isCustom, multimodal },
    ])
    if (!currentModel) form.setFieldsValue({ model: n })
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
    const providerKind = (v.kind ?? '').trim().toLowerCase()
    const selectedModel = (v.model ?? '').trim()
    const discovered = remoteModels.find(
      (model) => model.id.toLowerCase() === selectedModel.toLowerCase(),
    )
    const selected = enabled.find(
      (model) => model.model.toLowerCase() === selectedModel.toLowerCase(),
    )
    const existingCapability =
      existing?.kind.toLowerCase() === 'herdsman' &&
      existing.model.toLowerCase() === selectedModel.toLowerCase()
        ? existing.multimodal
        : false
    return {
      id: existing?.id ?? 0,
      name: (v.name ?? '').trim(),
      kind: (v.kind ?? '').trim(),
      baseUrl: (v.baseUrl ?? '').trim(),
      apiKey: (v.apiKey ?? '').trim(),
      model: selectedModel,
      multimodal:
        providerKind === 'herdsman'
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
      if ((values.kind ?? '').trim().toLowerCase() === 'herdsman') {
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
    if (!open || kind.toLowerCase() !== 'herdsman') {
      autoFetchedHerdsmanRef.current = ''
      return
    }
    if (!catalog.some((provider) => provider.kind.toLowerCase() === 'herdsman')) return

    const baseUrl = String(form.getFieldValue('baseUrl') ?? '').trim()
    if (!baseUrl) return
    const requestKey = `${existing?.id ?? 0}:${baseUrl}`
    if (autoFetchedHerdsmanRef.current === requestKey) return

    autoFetchedHerdsmanRef.current = requestKey
    void handleFetchModels()
    // 修改 Base URL 后保留手动获取语义,不把输入过程加入自动请求依赖。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [catalog, existing?.id, form, kind, open])

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

  const kindOptions = [
    ...catalog.map((c) => ({ value: c.kind, label: c.name })),
    { value: 'custom', label: '自定义(OpenAI 兼容)' },
  ]

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

        <Form.Item label="API Key" name="apiKey">
          <Input.Password placeholder="sk-…(留空则沿用已有配置)" autoComplete="off" />
        </Form.Item>

        <Form.Item
          label={
            <Space size={8}>
              <span>启用的模型</span>
              <Button
                type="link"
                size="small"
                icon={<ReloadOutlined />}
                loading={fetchingModels}
                onClick={() => void handleFetchModels()}
              >
                获取模型列表
              </Button>
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
                .filter((e) => e.custom)
                .map((e) => {
                  const discovered = remoteModels.find(
                    (model) => model.id.toLowerCase() === e.model.toLowerCase(),
                  )
                  const modelMultimodal = discovered?.multimodal ?? !!e.multimodal
                  const isHerdsmanModel = kind.toLowerCase() === 'herdsman'
                  return (
                    <Flex key={e.model} align="center" justify="space-between" gap={8}>
                      <Space size={6} wrap>
                        <Tag color="orange" style={{ fontSize: 11, marginInlineEnd: 0 }}>
                          自定义
                        </Tag>
                        <Typography.Text style={{ fontSize: 13 }}>
                          {e.label && e.label !== e.model ? `${e.label}` : e.model}
                        </Typography.Text>
                        {isHerdsmanModel && modelMultimodal && (
                          <Tag color="blue" style={{ fontSize: 11, marginInlineEnd: 0 }}>
                            支持图片
                          </Tag>
                        )}
                        {!isHerdsmanModel && (
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

              {/* 无内置列表的服务商(自定义等):用下拉添加模型 */}
              {(builtinModels.length === 0 || remoteModels.length > 0) && (
                <>
                  <Select
                    value={customName || undefined}
                    onChange={(v) => {
                      if (!v) {
                        setCustomName('')
                        return
                      }
                      const opt = extraModelOptions.find((o) => o.value === v)
                      addCustom(String(v), opt?.modelLabel, true, opt?.multimodal)
                    }}
                    options={extraModelOptions}
                    placeholder={remoteModels.length > 0 ? '选择服务商返回的模型…' : '选择要启用的模型…'}
                    showSearch
                    allowClear
                    popupMatchSelectWidth={false}
                    style={{ width: '100%' }}
                    notFoundContent={
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        {fetchingModels
                          ? '正在获取模型列表…'
                          : catalog.length === 0
                          ? '模型目录加载中或加载失败,请稍候重试'
                          : '没有更多可启用的模型'}
                      </Typography.Text>
                    }
                  />
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {remoteModels.length > 0
                      ? '选择一个服务商返回的模型并启用'
                      : '该服务商无内置模型;可从上方选择其它目录模型'}
                  </Typography.Text>
                </>
              )}
            </Flex>
          </Radio.Group>
        </Form.Item>

      </Form>
    </Modal>
  )
}
