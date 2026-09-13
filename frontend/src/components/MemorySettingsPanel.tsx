import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  App as AntApp,
  Button,
  Empty,
  Flex,
  Input,
  Modal,
  Pagination,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Spin,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import {
  DeleteOutlined,
  DownloadOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
  SaveOutlined,
} from '@ant-design/icons'
import { memoryRepository } from '../shared/repositories'
import { useWailsEvent } from '../shared/wails/events'
import type {
  MemoryConfigLite,
  MemoryInputLite,
  MemoryItemLite,
  MemorySettingsLite,
  MemoryStatusLite,
} from '../api'

const { Text, Title, Paragraph } = Typography
const PAGE_SIZE = 8

function blankMemory(): MemoryInputLite {
  return { content: '', topics: [], kind: 'fact', eventTime: '', participants: [], location: '' }
}

function displayTime(value: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

function statusLabel(status: MemoryStatusLite) {
  if (status.state === 'extracting') return `正在提取${status.pendingJobs ? ` · 等待 ${status.pendingJobs}` : ''}`
  if (status.state === 'error') return '提取异常'
  return status.pendingJobs ? `等待提取 · ${status.pendingJobs}` : '空闲'
}

export default function MemorySettingsPanel() {
  const { message } = AntApp.useApp()
  const [settings, setSettings] = useState<MemorySettingsLite | null>(null)
  const [draft, setDraft] = useState<MemoryConfigLite | null>(null)
  const [memories, setMemories] = useState<MemoryItemLite[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [query, setQuery] = useState('')
  const [kind, setKind] = useState<'all' | 'fact' | 'episode'>('all')
  const [page, setPage] = useState(1)
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<MemoryItemLite | null>(null)
  const [memoryDraft, setMemoryDraft] = useState<MemoryInputLite>(blankMemory)
  const [editorSaving, setEditorSaving] = useState(false)

  const loadSettings = useCallback(async () => {
    const next = await memoryRepository.getSettings()
    setSettings(next)
    setDraft(next.config)
  }, [])

  const loadMemories = useCallback(async () => {
    setMemories(await memoryRepository.list())
  }, [])

  const loadAll = useCallback(async () => {
    setLoading(true)
    try {
      await Promise.all([loadSettings(), loadMemories()])
    } catch (err) {
      message.error(`加载记忆失败：${String(err)}`)
    } finally {
      setLoading(false)
    }
  }, [loadMemories, loadSettings, message])

  useEffect(() => {
    void loadAll()
  }, [loadAll])

  useWailsEvent<string>('memory.changed', () => void loadMemories(), [loadMemories])
  useWailsEvent<string>('models.changed', () => void loadSettings(), [loadSettings])
  useWailsEvent<MemoryStatusLite>(
    'memory.status',
    (status) => setSettings((current) => (current ? { ...current, status } : current)),
    [],
  )

  const filtered = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase()
    return memories.filter((item) => {
      if (kind !== 'all' && item.kind !== kind) return false
      if (!needle) return true
      return [item.content, item.location, ...(item.topics ?? []), ...(item.participants ?? [])]
        .join(' ')
        .toLocaleLowerCase()
        .includes(needle)
    })
  }, [kind, memories, query])

  useEffect(() => {
    setPage(1)
  }, [kind, query])

  const paged = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)
  const activeStrategy = settings?.strategies.find((item) => item.id === draft?.strategy)
  const preview = draft?.strategy === 'custom' ? draft.customPrompt : activeStrategy?.prompt ?? ''
  const dirty = settings && draft ? JSON.stringify(settings.config) !== JSON.stringify(draft) : false

  const saveSettings = async () => {
    if (!draft) return
    if (draft.strategy === 'custom' && !draft.customPrompt.trim()) {
      message.warning('自定义提示词不能为空')
      return
    }
    setSaving(true)
    try {
      const next = await memoryRepository.saveSettings({
        ...draft,
        customPrompt: draft.customPrompt.trim(),
      })
      setSettings(next)
      setDraft(next.config)
      message.success('记忆设置已保存')
    } catch (err) {
      message.error(String(err))
    } finally {
      setSaving(false)
    }
  }

  const openCreate = () => {
    setEditing(null)
    setMemoryDraft(blankMemory())
    setEditorOpen(true)
  }

  const openEdit = (item: MemoryItemLite) => {
    setEditing(item)
    setMemoryDraft({
      content: item.content,
      topics: item.topics ?? [],
      kind: item.kind === 'episode' ? 'episode' : 'fact',
      eventTime: item.eventTime ? item.eventTime.slice(0, 10) : '',
      participants: item.participants ?? [],
      location: item.location,
    })
    setEditorOpen(true)
  }

  const saveMemory = async () => {
    if (!memoryDraft.content.trim()) {
      message.warning('记忆内容不能为空')
      return
    }
    if (memoryDraft.kind === 'episode' && !memoryDraft.eventTime) {
      message.warning('情景记忆需要事件日期')
      return
    }
    setEditorSaving(true)
    try {
      const input = { ...memoryDraft, content: memoryDraft.content.trim() }
      if (editing) await memoryRepository.update({ id: editing.id, ...input })
      else await memoryRepository.add(input)
      setEditorOpen(false)
      await loadMemories()
      message.success(editing ? '记忆已更新' : '记忆已添加')
    } catch (err) {
      message.error(String(err))
    } finally {
      setEditorSaving(false)
    }
  }

  const removeMemory = async (id: string) => {
    try {
      await memoryRepository.delete(id)
      await loadMemories()
      message.success('记忆已删除')
    } catch (err) {
      message.error(String(err))
    }
  }

  const clearMemories = async () => {
    try {
      await memoryRepository.clear()
      await loadMemories()
      message.success('全部记忆已清空')
    } catch (err) {
      message.error(String(err))
    }
  }

  const exportMemories = async () => {
    try {
      const path = await memoryRepository.export()
      message.success(`已导出到 ${path}`)
    } catch (err) {
      message.error(String(err))
    }
  }

  if (loading || !settings || !draft) {
    return <Flex justify="center" style={{ padding: 48 }}><Spin /></Flex>
  }

  return (
    <div className="bm-memory-settings">
      <header className="bm-settings-heading bm-settings-page-heading">
        <div className="bm-memory-heading-title">
          <span className="bm-settings-section-number">02</span>
          <div>
            <Title level={3}>记忆</Title>
            <Text type="secondary">跨会话保留稳定信息，并在后续对话中召回。</Text>
          </div>
        </div>
        <Button type="primary" icon={<SaveOutlined />} loading={saving} disabled={!dirty} onClick={() => void saveSettings()}>
          保存
        </Button>
      </header>

      <section className="bm-memory-band" aria-labelledby="memory-runtime-title">
        <div className="bm-memory-index">01</div>
        <div className="bm-memory-band-body">
          <Title level={5} id="memory-runtime-title">运行方式</Title>
          <div className="bm-memory-setting-row">
            <div><Text strong>启用长期记忆</Text><Text type="secondary">预加载、搜索和写入全部由此控制</Text></div>
            <Switch checked={draft.enabled} onChange={(enabled) => setDraft({ ...draft, enabled })} />
          </div>
          <div className="bm-memory-setting-row">
            <div><Text strong>自动提取</Text><Text type="secondary">对话完成后后台分析本轮内容</Text></div>
            <Switch disabled={!draft.enabled} checked={draft.autoExtract} onChange={(autoExtract) => setDraft({ ...draft, autoExtract })} />
          </div>
          <div className="bm-memory-runtime-grid">
            <div><Text type="secondary">提取模型</Text><Text>{settings.currentModel}</Text></div>
            <div><Text type="secondary">运行状态</Text><Text className={`bm-memory-status is-${settings.status.state}`}>{statusLabel(settings.status)}</Text></div>
            <div><Text type="secondary">最近成功</Text><Text>{settings.status.lastSuccessAt ? displayTime(settings.status.lastSuccessAt) : '尚无记录'}</Text></div>
          </div>
          {settings.status.lastError && <Alert type="error" showIcon message="最近一次提取失败" description={settings.status.lastError} />}
        </div>
      </section>

      <section className="bm-memory-band" aria-labelledby="memory-strategy-title">
        <div className="bm-memory-index">02</div>
        <div className="bm-memory-band-body">
          <Title level={5} id="memory-strategy-title">提取策略</Title>
          <Segmented
            block
            value={draft.strategy}
            options={[...settings.strategies.map((item) => ({ label: item.name, value: item.id })), { label: '自定义', value: 'custom' }]}
            onChange={(strategy) => setDraft({ ...draft, strategy: String(strategy) })}
          />
          <Text type="secondary" className="bm-memory-strategy-description">
            {draft.strategy === 'custom' ? '使用完整的自定义提示词；系统仅追加允许动作与相关旧记忆。' : activeStrategy?.description}
          </Text>
          {activeStrategy?.risk === 'high' && <Alert type="warning" showIcon message="全面策略会保留更多上下文，请定期检查记忆列表。" />}
          {draft.strategy === 'custom' ? (
            <div className="bm-memory-prompt-editor">
              <Input.TextArea
                value={draft.customPrompt}
                maxLength={20000}
                showCount
                autoSize={{ minRows: 8, maxRows: 16 }}
                placeholder="输入完整的记忆提取规则，可使用 {current_date}"
                onChange={(event) => setDraft({ ...draft, customPrompt: event.target.value })}
              />
            </div>
          ) : (
            <div className="bm-memory-prompt-preview">
              <Flex justify="space-between" align="center">
                <Text strong>有效提示词</Text>
                <Button size="small" onClick={() => setDraft({ ...draft, strategy: 'custom', customPrompt: preview })}>基于此自定义</Button>
              </Flex>
              <Paragraph ellipsis={{ rows: 6, expandable: true, symbol: '展开' }}>{preview}</Paragraph>
            </div>
          )}
        </div>
      </section>

      <section className="bm-memory-band" aria-labelledby="memory-list-title">
        <div className="bm-memory-index">03</div>
        <div className="bm-memory-band-body">
          <Flex className="bm-memory-list-heading" justify="space-between" align="center" gap={12} wrap>
            <div><Title level={5} id="memory-list-title">已保存的记忆</Title><Text type="secondary">{filtered.length} 条</Text></div>
            <Space size={6} wrap>
              <Tooltip title="刷新"><Button aria-label="刷新" icon={<ReloadOutlined />} onClick={() => void loadMemories()} /></Tooltip>
              <Tooltip title="导出 JSON"><Button aria-label="导出 JSON" icon={<DownloadOutlined />} onClick={() => void exportMemories()} /></Tooltip>
              <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新增</Button>
            </Space>
          </Flex>
          <Flex gap={8} className="bm-memory-filters">
            <Input.Search allowClear placeholder="搜索内容、主题、人物或地点" value={query} onChange={(event) => setQuery(event.target.value)} />
            <Select
              value={kind}
              onChange={setKind}
              options={[{ value: 'all', label: '全部类型' }, { value: 'fact', label: '事实' }, { value: 'episode', label: '情景' }]}
            />
          </Flex>

          {paged.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={memories.length ? '没有匹配的记忆' : '还没有保存记忆'} /> : (
            <div className="bm-memory-list">
              {paged.map((item) => (
                <article className="bm-memory-item" key={item.id}>
                  <div className="bm-memory-item-main">
                    <Flex gap={6} wrap align="center">
                      <Tag color={item.kind === 'episode' ? 'blue' : 'default'}>{item.kind === 'episode' ? '情景' : '事实'}</Tag>
                      {(item.topics ?? []).map((topic) => <Tag key={topic}>{topic}</Tag>)}
                    </Flex>
                    <Paragraph>{item.content}</Paragraph>
                    {item.kind === 'episode' && (
                      <div className="bm-memory-metadata">
                        {item.eventTime && <span>事件：{displayTime(item.eventTime)}</span>}
                        {!!item.participants?.length && <span>人物：{item.participants.join('、')}</span>}
                        {item.location && <span>地点：{item.location}</span>}
                      </div>
                    )}
                    <Text type="secondary" className="bm-memory-updated">更新于 {displayTime(item.updatedAt)}</Text>
                  </div>
                  <Space size={2} className="bm-memory-item-actions">
                    <Tooltip title="编辑"><Button type="text" aria-label="编辑" icon={<EditOutlined />} onClick={() => openEdit(item)} /></Tooltip>
                    <Popconfirm title="删除这条记忆？" onConfirm={() => void removeMemory(item.id)}>
                      <Tooltip title="删除"><Button type="text" danger aria-label="删除" icon={<DeleteOutlined />} /></Tooltip>
                    </Popconfirm>
                  </Space>
                </article>
              ))}
            </div>
          )}
          {filtered.length > PAGE_SIZE && <Pagination current={page} pageSize={PAGE_SIZE} total={filtered.length} showSizeChanger={false} onChange={setPage} />}
          <div className="bm-memory-danger-row">
            <div><Text strong>清空全部记忆</Text><Text type="secondary">软删除当前用户保存的所有记忆</Text></div>
            <Popconfirm title="确认清空全部记忆？" description="此操作会让后续对话无法召回这些内容。" okButtonProps={{ danger: true }} onConfirm={() => void clearMemories()}>
              <Button danger disabled={memories.length === 0}>清空</Button>
            </Popconfirm>
          </div>
        </div>
      </section>

      <Modal title={editing ? '编辑记忆' : '新增记忆'} open={editorOpen} confirmLoading={editorSaving} onOk={() => void saveMemory()} onCancel={() => setEditorOpen(false)} okText="保存">
        <div className="bm-memory-editor">
          <label><Text strong>类型</Text><Segmented block value={memoryDraft.kind} options={[{ label: '事实', value: 'fact' }, { label: '情景', value: 'episode' }]} onChange={(value) => setMemoryDraft({ ...memoryDraft, kind: value as 'fact' | 'episode' })} /></label>
          <label><Text strong>内容</Text><Input.TextArea autoSize={{ minRows: 4, maxRows: 9 }} value={memoryDraft.content} maxLength={4000} showCount onChange={(event) => setMemoryDraft({ ...memoryDraft, content: event.target.value })} /></label>
          <label><Text strong>主题</Text><Select mode="tags" value={memoryDraft.topics} tokenSeparators={[',', '，']} placeholder="输入后回车" onChange={(topics) => setMemoryDraft({ ...memoryDraft, topics })} /></label>
          {memoryDraft.kind === 'episode' && <>
            <label><Text strong>事件日期</Text><Input type="date" value={memoryDraft.eventTime} onChange={(event) => setMemoryDraft({ ...memoryDraft, eventTime: event.target.value })} /></label>
            <label><Text strong>参与人</Text><Select mode="tags" value={memoryDraft.participants} tokenSeparators={[',', '，']} placeholder="输入后回车" onChange={(participants) => setMemoryDraft({ ...memoryDraft, participants })} /></label>
            <label><Text strong>地点</Text><Input value={memoryDraft.location} onChange={(event) => setMemoryDraft({ ...memoryDraft, location: event.target.value })} /></label>
          </>}
        </div>
      </Modal>
    </div>
  )
}
