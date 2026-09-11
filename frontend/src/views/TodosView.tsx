import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  App as AntApp,
  Button,
  Checkbox,
  DatePicker,
  Drawer,
  Empty,
  Flex,
  Input,
  Image,
  List,
  Modal,
  Popconfirm,
  Segmented,
  Space,
  Spin,
  Switch,
  Tag,
  Typography,
} from 'antd'
import { EditOutlined, LinkOutlined, PlusOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { TodoService, useWailsEvent } from '../api'
import type { TodoLite, TodoSourceLite, TodoStatsLite } from '../api'

const { Text } = Typography

type Filter = 'all' | 'pending' | 'done'

// 待办页:统计卡片 + 增删改查 + 勾选完成 + 里程碑标记,数据变更经 todos.changed 自动刷新。
export default function TodosView({ onOpenConversation }: { onOpenConversation?: (id: number) => void }) {
  const { message } = AntApp.useApp()
  const [items, setItems] = useState<TodoLite[]>([])
  const [stats, setStats] = useState<TodoStatsLite | null>(null)
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<Filter>('all')

  // 编辑弹窗状态
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<TodoLite | null>(null)
  const [title, setTitle] = useState('')
  const [desc, setDesc] = useState('')
  const [deadline, setDeadline] = useState<dayjs.Dayjs | null>(null)
  const [milestone, setMilestone] = useState(false)
  const [saving, setSaving] = useState(false)
  const [sourceOpen, setSourceOpen] = useState(false)
  const [sourceLoading, setSourceLoading] = useState(false)
  const [sourceDetail, setSourceDetail] = useState<TodoSourceLite | null>(null)
  const [sourceError, setSourceError] = useState('')

  const reload = useCallback(async () => {
    try {
      const [list, st] = await Promise.all([
        TodoService.ListTodos() as unknown as Promise<TodoLite[]>,
        TodoService.TodoStats() as unknown as Promise<TodoStatsLite>,
      ])
      setItems(list)
      setStats(st)
    } catch (err) {
      message.error(`加载失败:${String(err)}`)
    } finally {
      setLoading(false)
    }
  }, [message])

  useEffect(() => {
    void reload()
  }, [reload])

  useWailsEvent<string>(
    'todos.changed',
    useCallback(() => void reload(), [reload]),
  )

  const visible = useMemo(() => {
    switch (filter) {
      case 'done':
        return items.filter((t) => t.status === 'done')
      case 'pending':
        return items.filter((t) => t.status !== 'done')
      default:
        return items
    }
  }, [items, filter])

  const openNew = () => {
    setEditing(null)
    setTitle('')
    setDesc('')
    setDeadline(null)
    setMilestone(false)
    setOpen(true)
  }

  const openEdit = (t: TodoLite) => {
    setEditing(t)
    setTitle(t.title)
    setDesc(t.description)
    setDeadline(t.deadline ? dayjs.unix(t.deadline) : null)
    setMilestone(t.isMilestone)
    setOpen(true)
  }

  const save = async () => {
    if (!title.trim()) {
      message.warning('标题不能为空')
      return
    }
    setSaving(true)
    try {
      const payload = {
        id: editing?.id ?? 0,
        title: title.trim(),
        description: desc.trim(),
        deadline: deadline ? Math.floor(deadline.valueOf() / 1000) : 0,
        isMilestone: milestone,
        status: editing?.status ?? 'pending',
        source: editing?.source ?? 'manual',
        sourceId: editing?.sourceId ?? 0,
      }
      if (editing) {
        await TodoService.UpdateTodo(payload)
      } else {
        await TodoService.CreateTodo(payload)
      }
      message.success(editing ? '已更新' : '已添加')
      setOpen(false)
      void reload()
    } catch (err) {
      message.error(`保存失败:${String(err)}`)
    } finally {
      setSaving(false)
    }
  }

  const toggleDone = async (t: TodoLite) => {
    const next = t.status === 'done' ? 'pending' : 'done'
    try {
      await TodoService.SetTodoStatus(t.id, next)
      void reload()
    } catch (err) {
      message.error(String(err))
    }
  }

  const remove = async (id: number) => {
    try {
      await TodoService.DeleteTodo(id)
      void reload()
    } catch (err) {
      message.error(String(err))
    }
  }

  const openSource = async (sourceId: number) => {
    setSourceOpen(true)
    setSourceLoading(true)
    setSourceError('')
    setSourceDetail(null)
    try {
      const detail = await TodoService.GetTodoSource(sourceId) as unknown as TodoSourceLite
      setSourceDetail(detail)
    } catch (err) {
      setSourceError(String(err))
    } finally {
      setSourceLoading(false)
    }
  }

  const sourceLabel = (source: string) => {
    if (source === 'chat' || source === 'conversation') return '对话'
    if (source === 'screenshot') return '截图'
    if (source === 'clipboard') return '粘贴板'
    return '手动'
  }

  const statCards = stats
    ? [
        { label: '总待办', value: stats.total, color: '#1677ff' },
        { label: '进行中', value: stats.pending, color: '#faad14' },
        { label: '里程碑', value: stats.milestones, color: '#722ed1' },
        { label: '逾期', value: stats.overdue, color: '#ff4d4f' },
        { label: '24h内到期', value: stats.dueSoon, color: '#fa8c16' },
      ]
    : []

  return (
    <Flex vertical style={{ height: '100%', padding: 16 }} gap={12}>
      <Flex justify="space-between" align="center" wrap gap={8}>
        <Segmented
          value={filter}
          onChange={(v) => setFilter(v as Filter)}
          options={[
            { label: '全部', value: 'all' },
            { label: '未完成', value: 'pending' },
            { label: '已完成', value: 'done' },
          ]}
        />
        <Button type="primary" icon={<PlusOutlined />} onClick={openNew}>
          新建待办
        </Button>
      </Flex>

      {statCards.length > 0 && (
        <Flex gap={12} wrap>
          {statCards.map((s) => (
            <Flex
              key={s.label}
              vertical
              align="center"
              style={{
                background: 'var(--bm-header-bg)',
                borderRadius: 10,
                padding: '8px 18px',
                border: '1px solid var(--bm-border)',
                minWidth: 90,
              }}
            >
              <Text strong style={{ fontSize: 20, color: s.color }}>
                {s.value}
              </Text>
              <Text type="secondary" style={{ fontSize: 12 }}>
                {s.label}
              </Text>
            </Flex>
          ))}
        </Flex>
      )}

      <Flex style={{ flex: 1, overflow: 'auto' }}>
        {loading ? (
          <Flex justify="center" align="center" style={{ width: '100%' }}>
            <Spin />
          </Flex>
        ) : visible.length === 0 ? (
          <Flex justify="center" align="center" style={{ width: '100%' }}>
            <Empty description={filter === 'all' ? '还没有待办' : '该分类下暂无待办'}>
              <Button type="primary" onClick={openNew}>
                新建待办
              </Button>
            </Empty>
          </Flex>
        ) : (
          <List
            style={{ width: '100%' }}
            dataSource={visible}
            renderItem={(t) => {
              const overdue = t.status !== 'done' && t.deadline > 0 && t.deadline * 1000 < Date.now()
              return (
                <List.Item
                  style={{ background: 'var(--bm-header-bg)', marginBottom: 8, borderRadius: 8, padding: '8px 12px' }}
                  actions={[
                    <Button
                      key="edit"
                      type="text"
                      size="small"
                      icon={<EditOutlined />}
                      onClick={() => openEdit(t)}
                    />,
                    <Popconfirm key="del" title="删除这条待办?" onConfirm={() => void remove(t.id)}>
                      <Button type="text" size="small" danger>
                        删除
                      </Button>
                    </Popconfirm>,
                  ]}
                >
                  <Flex vertical gap={2} style={{ width: '100%' }}>
                    <Flex align="center" gap={8}>
                      <Checkbox
                        checked={t.status === 'done'}
                        onChange={() => void toggleDone(t)}
                      />
                      <Text
                        strong
                        delete={t.status === 'done'}
                        style={{ textDecoration: t.status === 'done' ? 'line-through' : undefined }}
                      >
                        {t.isMilestone ? '🚩 ' : ''}
                        {t.title}
                      </Text>
                      {t.status === 'doing' && <Tag color="gold">进行中</Tag>}
                      {t.status === 'done' && <Tag color="green">完成</Tag>}
                    </Flex>
                    {t.description && (
                      <Text type="secondary" style={{ fontSize: 12, marginLeft: 32 }}>
                        {t.description}
                      </Text>
                    )}
                    <Space size={6} style={{ marginLeft: 32 }}>
                      {t.deadline > 0 && (
                        <Text
                          type={overdue ? 'danger' : 'secondary'}
                          style={{ fontSize: 12 }}
                        >
                          截止 {dayjs.unix(t.deadline).format('YYYY-MM-DD HH:mm')}
                          {overdue ? '(已逾期)' : ''}
                        </Text>
                      )}
                      {t.sourceId > 0 ? (
                        <Button
                          type="text"
                          size="small"
                          className="bm-todo-source-trigger"
                          icon={<LinkOutlined />}
                          onClick={() => void openSource(t.sourceId)}
                        >
                          {sourceLabel(t.source)}
                        </Button>
                      ) : (
                        <Tag color="default" style={{ fontSize: 11 }}>{sourceLabel(t.source)}</Tag>
                      )}
                    </Space>
                  </Flex>
                </List.Item>
              )
            }}
          />
        )}
      </Flex>

      <Modal
        title={editing ? '编辑待办' : '新建待办'}
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => void save()}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
      >
        <Space direction="vertical" style={{ width: '100%' }} size={10}>
          <Input
            placeholder="标题"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            maxLength={100}
          />
          <Input.TextArea
            placeholder="描述(可选)"
            value={desc}
            onChange={(e) => setDesc(e.target.value)}
            rows={2}
          />
          <Space wrap>
            <DatePicker
              showTime
              placeholder="截止时间(可选)"
              value={deadline}
              onChange={setDeadline}
              style={{ width: 220 }}
            />
            <span>
              <Switch checked={milestone} onChange={setMilestone} /> 里程碑
            </span>
          </Space>
        </Space>
      </Modal>

      <Drawer
        title="待办来源"
        placement="right"
        width={420}
        open={sourceOpen}
        onClose={() => setSourceOpen(false)}
        rootClassName="bm-todo-source-drawer"
      >
        {sourceLoading ? (
          <Flex justify="center" align="center" className="bm-todo-source-loading"><Spin /></Flex>
        ) : sourceError ? (
          <div className="bm-todo-source-error">{sourceError}</div>
        ) : sourceDetail ? (
          <Flex vertical className="bm-todo-source-detail">
            <div className="bm-todo-source-meta">
              <Text type="secondary">来源</Text>
              <Text>{sourceLabel(sourceDetail.kind.startsWith('clipboard') ? 'clipboard' : sourceDetail.kind)}</Text>
              <Text type="secondary">采集时间</Text>
              <Text>{dayjs.unix(sourceDetail.createdAt).format('YYYY-MM-DD HH:mm:ss')}</Text>
            </div>
            {!sourceDetail.available && (
              <div className="bm-todo-source-error">{sourceDetail.error || '来源内容不可用'}</div>
            )}
            {sourceDetail.dataUri && (
              <div className="bm-todo-source-image">
                <Image src={sourceDetail.dataUri} alt="待办来源图片" />
              </div>
            )}
            {sourceDetail.textContent && (
              <div className="bm-todo-source-text">{sourceDetail.textContent}</div>
            )}
            {sourceDetail.screenshotNote && (
              <div className="bm-todo-source-note">
                <Text type="secondary">截图备注</Text>
                <div>{sourceDetail.screenshotNote}</div>
              </div>
            )}
            {sourceDetail.kind === 'conversation' && (
              <div className="bm-todo-source-conversation">
                <Text type="secondary">{sourceDetail.conversationTitle || '原对话'}</Text>
                <Button
                  type="link"
                  disabled={!sourceDetail.conversationAvailable}
                  onClick={() => {
                    setSourceOpen(false)
                    onOpenConversation?.(sourceDetail.conversationId)
                  }}
                >
                  打开对话
                </Button>
              </div>
            )}
          </Flex>
        ) : null}
      </Drawer>
    </Flex>
  )
}
