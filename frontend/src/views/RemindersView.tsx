import { Button, Empty, Flex, List, Space, Tag, Typography } from 'antd'
import dayjs from 'dayjs'
import type { ReminderLite } from './MainLayout'

interface Props {
  items: ReminderLite[]
  onClear: () => void
}

// 提醒中心:展示来自 Go 提醒引擎的到期/逾期提醒(应用内触达通道)。
export default function RemindersView({ items, onClear }: Props) {
  return (
    <div style={{ padding: 24, maxWidth: 760 }}>
      <Flex justify="space-between" align="center" style={{ marginBottom: 16 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          提醒中心
        </Typography.Title>
        <Button size="small" disabled={items.length === 0} onClick={onClear}>
          全部清除
        </Button>
      </Flex>
      {items.length === 0 ? (
        <Empty description="暂无提醒。待办/里程碑临近截止或逾期时,这里与系统通知会同时提醒你。" />
      ) : (
        <List
          bordered
          dataSource={items}
          renderItem={(r) => (
            <List.Item>
              <Space direction="vertical" size={2} style={{ width: '100%' }}>
                <Space>
                  {r.type === 'overdue' ? (
                    <Tag color="red">已逾期</Tag>
                  ) : (
                    <Tag color="orange">即将到期</Tag>
                  )}
                  <Typography.Text strong>{r.title}</Typography.Text>
                </Space>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  截止 {dayjs(r.deadline * 1000).format('YYYY-MM-DD HH:mm')} · 提醒于{' '}
                  {dayjs(r.ts * 1000).format('MM-DD HH:mm')}
                </Typography.Text>
              </Space>
            </List.Item>
          )}
        />
      )}
    </div>
  )
}
