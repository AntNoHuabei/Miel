import { SafetyCertificateOutlined } from '@ant-design/icons'
import { Button, Modal } from 'antd'
import { PermissionApprovalExpiry } from './PermissionApprovalExpiry'
import type { ApprovalDecision, ApprovalRequest } from './permissionTypes'
import {
  formatApprovalOperation,
  formatApprovalRisk,
  formatApprovalScope,
  formatApprovalTarget,
  getPermissionToolLabel,
} from './permissionUtils'

export interface PermissionApprovalModalProps {
  request: ApprovalRequest
  onResolve: (decision: ApprovalDecision) => void | Promise<void>
  resolving?: boolean
}

export function PermissionApprovalModal({
  request,
  onResolve,
  resolving = false,
}: PermissionApprovalModalProps) {
  return (
    <Modal
      open
      centered
      closable={false}
      keyboard={false}
      mask={{ closable: false }}
      width={468}
      rootClassName="bm-permission-modal"
      title={(
        <div className="bm-permission-dialog-heading">
          <SafetyCertificateOutlined className="bm-permission-dialog-icon" />
          <div className="bm-permission-dialog-heading-copy">
            <span className="bm-permission-dialog-eyebrow">需要批准</span>
            <strong>{getPermissionToolLabel(request.tool)}</strong>
          </div>
          <span className="bm-permission-dialog-risk">
            {formatApprovalRisk(request.riskLevel)}
          </span>
        </div>
      )}
      footer={(
        <div className="bm-permission-dialog-actions">
          <Button disabled={resolving} onClick={() => void onResolve('deny')}>
            拒绝
          </Button>
          <Button disabled={resolving} onClick={() => void onResolve('session')}>
            本会话允许
          </Button>
          <Button
            type="primary"
            loading={resolving}
            onClick={() => void onResolve('once')}
          >
            允许一次
          </Button>
        </div>
      )}
    >
      <section
        className={`bm-permission-dialog risk-${request.riskLevel || 'medium'}`}
        aria-label="等待权限批准"
      >
        <dl className="bm-permission-dialog-details">
          <div>
            <dt>操作</dt>
            <dd>{formatApprovalOperation(request)}</dd>
          </div>
          <div>
            <dt>范围</dt>
            <dd className={request.outsideWorkspace ? 'is-outside-workspace' : undefined}>
              {formatApprovalScope(request)}
            </dd>
          </div>
          <div className="bm-permission-dialog-target-row">
            <dt>目标</dt>
            <dd>
              <code className="bm-permission-dialog-target" title={formatApprovalTarget(request)}>
                {formatApprovalTarget(request)}
              </code>
            </dd>
          </div>
        </dl>
        <PermissionApprovalExpiry expiresAt={request.expiresAt} />
      </section>
    </Modal>
  )
}
