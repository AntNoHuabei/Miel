import { useEffect, useState } from 'react'
import { App as AntApp, Button, Input, Modal, Tooltip } from 'antd'
import { SaveOutlined } from '@ant-design/icons'
import { artifactRepository } from '../../../shared/repositories'

export function SaveArtifactButton({ conversationId, messageId, suggestedName, codeBlock, onSaved }: { conversationId: number; messageId: string; suggestedName: string; codeBlock?: number; onSaved?: () => void }) {
  const { message } = AntApp.useApp()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState(suggestedName)
  const [saving, setSaving] = useState(false)
  useEffect(() => { if (open) setName(suggestedName) }, [open, suggestedName])
  const save = async () => {
    if (!name.trim()) return
    setSaving(true)
    try {
      await artifactRepository.saveMessage({ conversationId, messageId, name: name.trim(), ...(codeBlock === undefined ? {} : { codeBlock }) })
      setOpen(false); message.success('已保存到产物库'); onSaved?.()
    } catch (error) { message.error(`保存失败：${String(error)}`) }
    finally { setSaving(false) }
  }
  return <>
    <Tooltip title={codeBlock === undefined ? '将回复保存为产物' : '将代码块保存为产物'}><Button type="text" className="bm-chat-save-artifact" aria-label="保存为产物" icon={<SaveOutlined />} onClick={() => setOpen(true)} /></Tooltip>
    <Modal title="保存为产物" open={open} okText="保存" cancelText="取消" confirmLoading={saving} okButtonProps={{ disabled: !name.trim() }} onOk={() => void save()} onCancel={() => setOpen(false)} destroyOnHidden>
      <Input value={name} maxLength={240} aria-label="产物文件名" onChange={(event) => setName(event.target.value)} onPressEnter={() => void save()} />
    </Modal>
  </>
}
