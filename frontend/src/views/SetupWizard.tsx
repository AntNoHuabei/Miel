import { useEffect, useState } from 'react'
import { App as AntApp, Card, Col, Flex, Row, Spin, Tag, Typography } from 'antd'
import { settingsRepository } from '../shared/repositories'
import type { ProviderTemplateLite } from '../api'
import ProviderFormModal from '../components/ProviderFormModal'

const { Title, Paragraph, Text } = Typography

interface Props {
  onDone: () => void
}

// 首次启动:无任何模型服务商时以全屏向导“拦住”入口,配置完成即可进入主界面。
export default function SetupWizard({ onDone }: Props) {
  const { message } = AntApp.useApp()
  const [templates, setTemplates] = useState<ProviderTemplateLite[]>([])
  const [loading, setLoading] = useState(true)
  const [open, setOpen] = useState(false)
  const [picked, setPicked] = useState<ProviderTemplateLite | null>(null)

  useEffect(() => {
    settingsRepository.providerTemplates()
      .then(setTemplates)
      .catch((err) => message.error(String(err)))
      .finally(() => setLoading(false))
  }, [message])

  return (
    <Flex
      align="center"
      justify="center"
      style={{ minHeight: '100vh', padding: 24, background: 'linear-gradient(135deg,#f5f7fa,#e6ecf5)' }}
    >
      <div style={{ width: 720 }}>
        <Title level={2} style={{ textAlign: 'center', marginBottom: 4 }}>
          👋 欢迎使用 BlankMind
        </Title>
        <Paragraph style={{ textAlign: 'center' }}>
          <Text type="secondary">
            你的本地办公 Agent。无需登录,数据保存在本机。开始前请先配置一个模型服务商:
          </Text>
        </Paragraph>
        {loading ? (
          <Flex justify="center" style={{ padding: 48 }}>
            <Spin />
          </Flex>
        ) : (
          <Row gutter={[16, 16]}>
            {templates.map((t) => (
              <Col span={8} key={t.name}>
                <Card
                  hoverable
                  onClick={() => {
                    setPicked(t)
                    setOpen(true)
                  }}
                >
                  <Title level={5} style={{ marginTop: 0 }}>
                    {t.name}
                  </Title>
                  <div>
                    <Tag>{t.model || '自定义模型'}</Tag>
                    {t.multimodal && <Tag color="purple">支持图片</Tag>}
                  </div>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {t.baseUrl}
                  </Text>
                </Card>
              </Col>
            ))}
          </Row>
        )}
        <Paragraph style={{ textAlign: 'center', marginTop: 24, marginBottom: 0 }}>
          <Text type="secondary" style={{ fontSize: 12 }}>
            想接自建网关 / 其它厂商?请选「自定义服务商」,填写任意 OpenAI 兼容的接口地址。
          </Text>
        </Paragraph>
        <ProviderFormModal
          open={open}
          template={picked}
          existing={null}
          onCancel={() => setOpen(false)}
          onSaved={() => {
            setOpen(false)
            message.success('配置完成,开始使用 BlankMind')
            onDone()
          }}
        />
      </div>
    </Flex>
  )
}
