import { ClockCircleOutlined } from '@ant-design/icons'
import { useEffect, useState } from 'react'

export function formatApprovalExpiry(expiresAt: string, now: number) {
  const expiry = Date.parse(expiresAt)
  if (!Number.isFinite(expiry)) return '超时后自动拒绝'
  const remainingSeconds = Math.max(0, Math.ceil((expiry - now) / 1000))
  if (remainingSeconds === 0) return '即将自动拒绝'
  const minutes = Math.floor(remainingSeconds / 60)
  const seconds = remainingSeconds % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')} 后自动拒绝`
}

export function PermissionApprovalExpiry({ expiresAt }: { expiresAt: string }) {
  const [now, setNow] = useState(Date.now)

  useEffect(() => {
    setNow(Date.now())
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [expiresAt])

  return (
    <div className="bm-permission-dialog-expiry">
      <ClockCircleOutlined aria-hidden="true" />
      <span>{formatApprovalExpiry(expiresAt, now)}</span>
    </div>
  )
}
