import { useEffect, useReducer, useRef } from 'react'
import { App as AntApp, Button, Checkbox, DatePicker, Empty, Flex, Input, Spin, Tooltip, Typography } from 'antd'
import { DeleteOutlined, FileImageOutlined, FileTextOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { clipboardRepository } from '../../../shared/repositories'
import { clipboardTodoReducer, initialClipboardTodoState } from '../clipboardTodoState'

interface ClipboardTodoPanelProps {
  request: number
  onDraftChange: (id: string) => void
  onCancel: () => void
  onCreated: () => void
}

export function ClipboardTodoPanel({ request, onDraftChange, onCancel, onCreated }: ClipboardTodoPanelProps) {
  const { message } = AntApp.useApp()
  const [state, dispatch] = useReducer(clipboardTodoReducer, initialClipboardTodoState)
  const handledRequest = useRef(0)

  useEffect(() => {
    if (!request || handledRequest.current === request) return
    handledRequest.current = request
    dispatch({ type: 'loading' })
    void clipboardRepository.extractTodos()
      .then((result) => { dispatch({ type: 'loaded', draft: result }); onDraftChange(result.draftId) })
      .catch((reason) => dispatch({ type: 'failed', error: String(reason) }))
  }, [request, onDraftChange])

  const confirm = async () => {
    if (!state.draft) return
    const chosen = state.items.filter((_, index) => state.selected.includes(index))
    dispatch({ type: 'confirming' })
    try {
      const created = await clipboardRepository.confirmTodos({ draftId: state.draft.draftId, items: chosen })
      message.success(`已创建 ${created?.length ?? 0} 条待办`)
      onCreated()
    } catch (reason) {
      dispatch({ type: 'confirmation-failed', error: String(reason) })
      message.error(`保存失败：${String(reason)}`)
    }
  }

  return (
    <Flex vertical className="bm-clipboard-panel">
      <header className="bm-clipboard-heading"><div><Typography.Text strong>从粘贴板生成待办</Typography.Text><Typography.Text type="secondary">确认前可以修改提取结果。</Typography.Text></div><Button type="text" onClick={onCancel}>返回对话</Button></header>
      {state.stage === 'loading' ? (
        <Flex flex={1} align="center" justify="center" vertical gap={8}><Spin /><Typography.Text type="secondary">正在提取待办</Typography.Text></Flex>
      ) : state.stage === 'error' ? (
        <Flex flex={1} align="center" justify="center" vertical gap={10} className="bm-clipboard-error"><Typography.Text type="danger">{state.error}</Typography.Text><Button onClick={onCancel}>返回对话</Button></Flex>
      ) : state.draft ? (
        <>
          <div className="bm-clipboard-source-preview">{state.draft.kind === 'clipboard_image' ? <><FileImageOutlined /><img src={state.draft.dataUri} alt="粘贴板来源" /></> : <><FileTextOutlined /><div>{state.draft.text}</div></>}</div>
          <div className="bm-clipboard-items">
            {state.items.length === 0 ? <Empty description="没有识别到待办" /> : state.items.map((item, index) => (
              <div className="bm-clipboard-item" key={index}>
                <Checkbox checked={state.selected.includes(index)} onChange={(event) => dispatch({ type: 'toggle', index, selected: event.target.checked })} />
                <div className="bm-clipboard-item-fields">
                  <Input value={item.title} placeholder="标题" onChange={(event) => dispatch({ type: 'update', index, patch: { title: event.target.value } })} />
                  <Input.TextArea value={item.description} placeholder="描述（可选）" autoSize={{ minRows: 1, maxRows: 3 }} onChange={(event) => dispatch({ type: 'update', index, patch: { description: event.target.value } })} />
                  <Flex gap={10} align="center" wrap><DatePicker showTime value={item.dueDate ? dayjs(item.dueDate) : null} placeholder="截止时间" onChange={(value) => dispatch({ type: 'update', index, patch: { dueDate: value ? value.format('YYYY-MM-DD HH:mm') : '' } })} /><Checkbox checked={item.milestone} onChange={(event) => dispatch({ type: 'update', index, patch: { milestone: event.target.checked } })}>里程碑</Checkbox></Flex>
                </div>
                <Tooltip title="不保存此项"><Button type="text" aria-label="不保存此项" icon={<DeleteOutlined />} onClick={() => dispatch({ type: 'remove', index })} /></Tooltip>
              </div>
            ))}
          </div>
          <footer className="bm-clipboard-footer"><Typography.Text type="secondary">已选择 {state.selected.length} 项</Typography.Text><Button type="primary" loading={state.stage === 'confirming'} disabled={state.selected.length === 0} onClick={() => void confirm()}>创建待办</Button></footer>
        </>
      ) : null}
    </Flex>
  )
}
