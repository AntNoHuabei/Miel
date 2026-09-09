import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Checkbox,
  Empty,
  Flex,
  Input,
  Modal,
  Space,
  Spin,
  Tooltip,
  Typography,
  message,
} from 'antd'
import { MessageOutlined, SaveOutlined, ScanOutlined } from '@ant-design/icons'
import { ScreenshotService, SettingsService, useWailsEvent } from '../api'

const { Text } = Typography

export interface ScreenshotCaptured {
  id: number
  path: string
  dataUri: string
  width: number
  height: number
  createdAt: number
}

export interface ExtractedTodoLite {
  title: string
  description: string
  milestone: boolean
  dueDate: string
}

type Stage = 'menu' | 'busy' | 'extracted' | 'answer'

// 全局截图处理弹层。
// 触发方式:① 系统全局热键(默认 Ctrl+Alt+S,Go 广播 screenshot.captured)
//          ② 应用内“截图”按钮(window 派发 blankmind:capture 事件)
// 提供三种处理:转待办(AI 提取 → 勾选确认 → 入库)/ 问答(AI 看图回答)/ 仅保存。
export default function ScreenshotModal() {
  const [shot, setShot] = useState<ScreenshotCaptured | null>(null)
  const [open, setOpen] = useState(false)
  const [stage, setStage] = useState<Stage>('menu')
  const [visionOK, setVisionOK] = useState(true)
  const [askMode, setAskMode] = useState(false)

  // 转待办
  const [extracted, setExtracted] = useState<ExtractedTodoLite[]>([])
  const [checked, setChecked] = useState<number[]>([])
  const [busy, setBusy] = useState(false)

  // 问答
  const [question, setQuestion] = useState('')
  const [answer, setAnswer] = useState('')

  // 保存备注
  const [note, setNote] = useState('')

  // 判断是否配置了支持图片输入的模型(决定“转待办/问答”可用性)
  const checkVision = useCallback(async () => {
    try {
      const list = (await SettingsService.ListProviders()) ?? []
      setVisionOK(list.some((p) => p.multimodal))
    } catch {
      setVisionOK(false)
    }
  }, [])

  useEffect(() => {
    void checkVision()
  }, [checkVision])

  const reset = useCallback(() => {
    setStage('menu')
    setAskMode(false)
    setExtracted([])
    setChecked([])
    setQuestion('')
    setAnswer('')
    setNote('')
    setBusy(false)
  }, [])

  const openWith = useCallback(
    (s: ScreenshotCaptured) => {
      setShot(s)
      setOpen(true)
      reset()
      void checkVision()
    },
    [reset, checkVision],
  )

  // Go 侧热键截屏后广播
  useWailsEvent<ScreenshotCaptured>(
    'screenshot.captured',
    useCallback((s) => openWith(s), [openWith]),
  )

  // 应用内“截图”按钮(窗口内自触发,不依赖系统热键)
  useEffect(() => {
    const h = () => {
      ScreenshotService.Capture()
        .then((res) => openWith(res as unknown as ScreenshotCaptured))
        .catch((err) => message.error(`截屏失败:${String(err)}`))
    }
    window.addEventListener('blankmind:capture', h)
    return () => window.removeEventListener('blankmind:capture', h)
  }, [openWith])

  const close = useCallback(() => {
    setOpen(false)
    reset()
  }, [reset])

  const extract = async () => {
    if (!shot) return
    setBusy(true)
    setStage('busy')
    try {
      const items = (await ScreenshotService.ExtractTodos(shot.id)) ?? []
      setExtracted(items)
      setChecked(items.map((_, i) => i))
      setStage('extracted')
    } catch (err) {
      message.error(`提取失败:${String(err)}`)
      setStage('menu')
    } finally {
      setBusy(false)
    }
  }

  const confirmTodos = async () => {
    if (!shot) return
    setBusy(true)
    try {
      const picked = extracted.filter((_, i) => checked.includes(i))
      const created =
        (await ScreenshotService.ConfirmExtracted({
          shotId: shot.id,
          items: picked,
        })) ?? []
      message.success(`已添加 ${created.length} 条待办`)
      close()
    } catch (err) {
      message.error(`入库失败:${String(err)}`)
    } finally {
      setBusy(false)
    }
  }

  const ask = async () => {
    if (!shot || !question.trim()) return
    setBusy(true)
    setStage('busy')
    try {
      const ans = await ScreenshotService.AskAboutShot({
        shotId: shot.id,
        question: question.trim(),
      })
      setAnswer(ans)
      setStage('answer')
    } catch (err) {
      message.error(`问答失败:${String(err)}`)
      setStage('menu')
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    if (!shot) return
    setBusy(true)
    try {
      await ScreenshotService.SaveShot({ id: shot.id, note })
      message.success('截图已保存')
      close()
    } catch (err) {
      message.error(`保存失败:${String(err)}`)
    } finally {
      setBusy(false)
    }
  }

  const visionTip = visionOK
    ? ''
    : '当前没有支持图片输入的模型,无法“转待办/问答”;可“仅保存”,或在设置中启用多模态模型并设为默认'

  const renderBody = () => {
    if (!shot) {
      return <Empty description="暂无截图" style={{ padding: '24px 0' }} />
    }

    if (stage === 'busy') {
      return (
        <Flex align="center" justify="center" style={{ minHeight: 200 }}>
          <Spin tip="AI 正在处理截图…">
            <div style={{ padding: 24 }} />
          </Spin>
        </Flex>
      )
    }

    if (stage === 'extracted') {
      return (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Text strong>请勾选要从截图中添加的待办:</Text>
          {extracted.length === 0 ? (
            <Empty description="未识别到任务类内容,可返回重试或改用问答" />
          ) : (
            extracted.map((it, i) => (
              <Checkbox
                key={i}
                checked={checked.includes(i)}
                onChange={(e) =>
                  setChecked((prev) =>
                    e.target.checked
                      ? [...prev, i]
                      : prev.filter((x) => x !== i),
                  )
                }
              >
                <Space direction="vertical" size={0}>
                  <Text>
                    {it.milestone ? '🚩 ' : ''}
                    {it.title}
                  </Text>
                  {it.dueDate && (
                    <Text type="secondary" style={{ fontSize: 12 }}>
                      截止:{it.dueDate}
                    </Text>
                  )}
                </Space>
              </Checkbox>
            ))
          )}
          <Flex justify="space-between">
            <Button onClick={() => setStage('menu')}>返回</Button>
            <Button
              type="primary"
              loading={busy}
              disabled={checked.length === 0}
              onClick={confirmTodos}
            >
              添加 {checked.length} 条
            </Button>
          </Flex>
        </Space>
      )
    }

    if (stage === 'answer') {
      return (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Text strong>回答:</Text>
          <div
            style={{
              whiteSpace: 'pre-wrap',
              background: 'var(--bm-inset-bg)',
              padding: 12,
              borderRadius: 8,
              maxHeight: 260,
              overflow: 'auto',
            }}
          >
            {answer}
          </div>
          <Flex justify="space-between">
            <Button onClick={() => setStage('menu')}>返回</Button>
            <Button type="primary" onClick={close}>
              完成
            </Button>
          </Flex>
        </Space>
      )
    }

    // menu
    return (
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Flex justify="center">
          <img
            src={shot.dataUri}
            alt="截图预览"
            style={{
              maxWidth: '100%',
              maxHeight: 240,
              borderRadius: 8,
              border: '1px solid var(--bm-border)',
            }}
          />
        </Flex>
        {!visionOK && <Alert type="warning" showIcon message={visionTip} />}
        <Flex gap={8} wrap>
          <Tooltip title={visionOK ? 'AI 提取为待办,勾选后入库' : visionTip}>
            <Button
              icon={<ScanOutlined />}
              disabled={!visionOK}
              loading={busy}
              onClick={extract}
            >
              转待办
            </Button>
          </Tooltip>
          <Tooltip title={visionOK ? '针对截图内容提问' : visionTip}>
            <Button
              icon={<MessageOutlined />}
              disabled={!visionOK}
              onClick={() => setAskMode((v) => !v)}
            >
              问答
            </Button>
          </Tooltip>
          <Button icon={<SaveOutlined />} loading={busy} onClick={save}>
            仅保存
          </Button>
        </Flex>
        {askMode && (
          <Space.Compact style={{ width: '100%' }}>
            <Input
              placeholder="问这张截图,例如:总结里面的任务"
              value={question}
              onChange={(e) => setQuestion(e.target.value)}
              onPressEnter={ask}
            />
            <Button type="primary" loading={busy} onClick={ask}>
              提问
            </Button>
          </Space.Compact>
        )}
        {!askMode && (
          <Input
            placeholder="备注(可选,随截图一并保存)"
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
        )}
      </Space>
    )
  }

  return (
    <Modal
      title="截图处理"
      open={open}
      onCancel={close}
      footer={null}
      width={560}
    >
      {renderBody()}
    </Modal>
  )
}
