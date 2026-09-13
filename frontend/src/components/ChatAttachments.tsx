import { useCallback, useEffect, useRef, useState } from 'react'
import type { ClipboardEvent as ReactClipboardEvent } from 'react'
import { App as AntApp, Button, Image, Tooltip } from 'antd'
import { CloseOutlined } from '@ant-design/icons'
import { attachmentRepository } from '../shared/repositories'
import type { ChatAttachmentDraftLite, MessageAttachmentLite } from '../api'

const MAX_IMAGES = 8
const MAX_TOTAL_BYTES = 50 * 1024 * 1024
const THUMBNAIL_SIZE = 80

export type ChatImageAttachment = ChatAttachmentDraftLite | MessageAttachmentLite

export function useChatAttachments() {
  const { message } = AntApp.useApp()
  const [attachments, setAttachments] = useState<ChatAttachmentDraftLite[]>([])
  const attachmentsRef = useRef<ChatAttachmentDraftLite[]>([])

  useEffect(() => {
    attachmentsRef.current = attachments
  }, [attachments])

  useEffect(() => () => {
    const ids = attachmentsRef.current.map((item) => item.id)
    if (ids.length > 0) void attachmentRepository.discardDrafts(ids)
  }, [])

  const addDrafts = useCallback((incoming: ChatAttachmentDraftLite[]) => {
    if (incoming.length === 0) return
    const current = attachmentsRef.current
    let count = current.length
    let total = current.reduce((sum, item) => sum + item.size, 0)
    const accepted: ChatAttachmentDraftLite[] = []
    const rejected: ChatAttachmentDraftLite[] = []
    for (const item of incoming) {
      if (count >= MAX_IMAGES || total + item.size > MAX_TOTAL_BYTES) {
        rejected.push(item)
        continue
      }
      accepted.push(item)
      count += 1
      total += item.size
    }
    if (accepted.length > 0) {
      const next = [...current, ...accepted]
      attachmentsRef.current = next
      setAttachments(next)
    }
    if (rejected.length > 0) {
      void attachmentRepository.discardDrafts(rejected.map((item) => item.id))
      message.warning('最多添加 8 张图片，单次总大小不能超过 50 MB')
    }
  }, [message])

  const pickImages = useCallback(async () => {
    try {
      const picked = await attachmentRepository.pickImages()
      addDrafts(picked ?? [])
    } catch (error) {
      message.error(`添加图片失败：${String(error)}`)
    }
  }, [addDrafts, message])

  const pasteImage = useCallback(async () => {
    try {
      const pasted = await attachmentRepository.pasteImage()
      if (pasted) addDrafts([pasted])
    } catch (error) {
      message.error(`粘贴图片失败：${String(error)}`)
    }
  }, [addDrafts, message])

  const onPaste = useCallback((event: ReactClipboardEvent<HTMLTextAreaElement>) => {
    const hasImage = Array.from(event.clipboardData?.items ?? []).some((item) =>
      item.type.toLowerCase().startsWith('image/'),
    )
    if (hasImage) void pasteImage()
  }, [pasteImage])

  const removeAttachment = useCallback((id: string) => {
    const next = attachmentsRef.current.filter((item) => item.id !== id)
    attachmentsRef.current = next
    setAttachments(next)
    void attachmentRepository.discardDrafts([id])
  }, [])

  const discardAttachments = useCallback(() => {
    const ids = attachmentsRef.current.map((item) => item.id)
    attachmentsRef.current = []
    setAttachments([])
    if (ids.length > 0) void attachmentRepository.discardDrafts(ids)
  }, [])

  const consumeAttachments = useCallback(() => {
    attachmentsRef.current = []
    setAttachments([])
  }, [])

  return {
    attachments,
    pickImages,
    onPaste,
    removeAttachment,
    discardAttachments,
    consumeAttachments,
  }
}

export function ChatAttachmentStrip({
  attachments,
  onRemove,
}: {
  attachments: ChatImageAttachment[]
  onRemove?: (id: string) => void
}) {
  if (attachments.length === 0) return null
  return (
    <Image.PreviewGroup>
      <div className="bm-chat-attachment-strip">
        {attachments.map((attachment) => (
          <div className="bm-chat-attachment" key={attachment.id}>
            <LazyAttachmentImage attachment={attachment} />
            {onRemove && (
              <Tooltip title="移除图片">
                <Button
                  type="text"
                  className="bm-chat-attachment-remove"
                  aria-label={`移除图片 ${attachment.name}`}
                  icon={<CloseOutlined />}
                  onClick={() => onRemove(attachment.id)}
                />
              </Tooltip>
            )}
          </div>
        ))}
      </div>
    </Image.PreviewGroup>
  )
}

function LazyAttachmentImage({ attachment }: { attachment: ChatImageAttachment }) {
  const { message } = AntApp.useApp()
  const [previewSource, setPreviewSource] = useState('')
  const loadingRef = useRef(false)

  const loadOriginal = async () => {
    if (previewSource || loadingRef.current) return
    loadingRef.current = true
    try {
      setPreviewSource(await attachmentRepository.imageDataUri(attachment.id))
    } catch (error) {
      message.error(`加载原图失败：${String(error)}`)
    } finally {
      loadingRef.current = false
    }
  }

  return (
    <Image
      src={attachment.thumbnailDataUri}
      alt={attachment.name || '对话图片'}
      width={THUMBNAIL_SIZE}
      height={THUMBNAIL_SIZE}
      preview={{
        src: previewSource || attachment.thumbnailDataUri,
        onVisibleChange: (visible) => {
          if (visible) void loadOriginal()
        },
      }}
    />
  )
}
