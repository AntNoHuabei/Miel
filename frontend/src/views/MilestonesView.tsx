import { useCallback, useEffect, useState } from 'react'
import {
  App as AntApp,
  Button,
  Empty,
  Flex,
  Progress,
  Space,
  Spin,
  Tag,
  Typography,
} from 'antd'
import dayjs from 'dayjs'
import { TodoService, useWailsEvent } from '../api'
import type { TodoLite } from '../api'

const { Text, Title } = Typography

interface MilestoneCard {
  todo: TodoLite
  state: 'overdue' | 'today' | 'soon' | 'normal'
  days: number // 距截止天数(负=已逾期)
}

// 里程碑页:未完成的里程碑以倒计时卡片展示(逾期红色预警 / 当日 / 临近)。
export default function MilestonesView({ onGoTodos }: { onGoTodos?: () => void }) {
  const { message } = AntApp.useApp()
  const [items, setItems] = useState<TodoLite[]>([])
  const [loading, setLoading] = useState(true)

  const reload = useCallback(async () => {
    try {
      const list = (await TodoService.ListTodos()) as unknown as TodoLite[]
      setItems(list.filter((t) => t.isMilestone && t.status !== 'done'))
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

  const cards: MilestoneCard[] = items.map((t) => {
    const ms = t.deadline > 0 ? t.deadline * 1000 - Date.now() : null
    if (ms === null) {
      return { todo: t, state: 'normal', days: Infinity }
    }
    const days = Math.ceil(ms / 86400000)
    let state: MilestoneCard['state'] = 'normal'
    if (ms < 0) state = 'overdue'
    else if (days <= 0) state = 'today'
    else if (days <= 3) state = 'soon'
    return { todo: t, state, days }
  })

  const colorOf = (c: MilestoneCard): string => {
    switch (c.state) {
      case 'overdue':
        return '#ff4d4f'
      case 'today':
        return '#fa8c16'
      case 'soon':
        return '#faad14'
      default:
        return '#1677ff'
    }
  }

  const textOf = (c: MilestoneCard): string => {
    if (!c.todo.deadline) return '未设截止日期'
    switch (c.state) {
      case 'overdue':
        return `已逾期 ${-c.days} 天`
      case 'today':
        return '今天到期'
      case 'soon':
        return `还剩 ${c.days} 天`
      default:
        return `还剩 ${c.days} 天`
    }
  }

  const percentOf = (c: MilestoneCard): number => {
    if (!c.todo.deadline) return 0
    const total = Math.max(c.todo.deadline * 1000 - c.todo.createdAt * 1000, 1)
    const left = c.todo.deadline * 1000 - Date.now()
    const used = total - left
    return Math.max(0, Math.min(100, Math.round((used / total) * 100)))
  }

  return (
    <Flex vertical style={{ height: '100%', padding: 16 }} gap={12}>
      <Flex justify="space-between" align="center">
        <Title level={5} style={{ margin: 0 }}>
          里程碑倒计时
        </Title>
        {onGoTodos && (
          <Button type="link" onClick={onGoTodos}>
            管理待办 →
          </Button>
        )}
      </Flex>

      {loading ? (
        <Flex justify="center" align="center" style={{ flex: 1 }}>
          <Spin />
        </Flex>
      ) : cards.length === 0 ? (
        <Flex justify="center" align="center" style={{ flex: 1 }}>
          <Empty description="暂无进行中的里程碑">
            {onGoTodos && (
              <Button type="primary" onClick={onGoTodos}>
                去创建里程碑
              </Button>
            )}
          </Empty>
        </Flex>
      ) : (
        <Flex wrap gap={12} style={{ flex: 1, overflow: 'auto', alignContent: 'flex-start' }}>
          {cards
            .sort((a, b) => a.days - b.days)
            .map((c) => {
              const color = colorOf(c)
              return (
                <Flex
                  key={c.todo.id}
                  vertical
                  style={{
                    width: 260,
                    background: 'var(--bm-header-bg)',
                    border: `1px solid ${c.state === 'overdue' ? '#ff4d4f55' : 'var(--bm-border)'}`,
                    borderRadius: 12,
                    padding: 14,
                    gap: 8,
                  }}
                >
                  <Space>
                    <Text strong style={{ fontSize: 15 }}>
                      🚩 {c.todo.title}
                    </Text>
                    {c.state === 'overdue' && <Tag color="red">逾期</Tag>}
                    {c.state === 'today' && <Tag color="orange">今日到期</Tag>}
                  </Space>
                  {c.todo.description && (
                    <Text type="secondary" style={{ fontSize: 12 }}>
                      {c.todo.description}
                    </Text>
                  )}
                  {c.todo.deadline > 0 && (
                    <>
                      <Text type="secondary" style={{ fontSize: 12 }}>
                        截止:{dayjs.unix(c.todo.deadline).format('YYYY-MM-DD HH:mm')}
                      </Text>
                      <Progress
                        percent={percentOf(c)}
                        size="small"
                        strokeColor={color}
                        format={() => textOf(c)}
                      />
                    </>
                  )}
                  <Text style={{ color, fontWeight: 600, fontSize: 14 }}>{textOf(c)}</Text>
                </Flex>
              )
            })}
        </Flex>
      )}
    </Flex>
  )
}
