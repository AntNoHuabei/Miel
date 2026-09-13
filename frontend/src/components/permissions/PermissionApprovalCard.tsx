import { SafetyCertificateOutlined } from '@ant-design/icons'
import { Button, Flex, Typography } from 'antd'
import type { ApprovalDecision, ApprovalRequest } from './permissionTypes'
import {
  formatApprovalOperation,
  formatApprovalRisk,
  formatApprovalTarget,
  getPermissionToolLabel,
} from './permissionUtils'

const { Text } = Typography

export interface PermissionApprovalCardProps {
  request: ApprovalRequest
  onResolve: (decision: ApprovalDecision) => void | Promise<void>
  resolving?: boolean
}

export function PermissionApprovalCard({
  request,
  onResolve,
  resolving = false,
}: PermissionApprovalCardProps) {
  return (
    <section
      className={`bm-permission-card risk-${request.riskLevel || 'medium'}`}
      aria-label="等待权限批准"
    >
      <Flex align="flex-start" gap={10}>
        <SafetyCertificateOutlined className="bm-permission-card-icon" />
        <div className="bm-permission-card-copy">
          <Text strong>需要批准：{getPermissionToolLabel(request.tool)}</Text>
          <Text type="secondary" className="bm-permission-card-operation">
            {formatApprovalOperation(request)} · {formatApprovalRisk(request.riskLevel)}
          </Text>
          <code className="bm-permission-card-target" title={formatApprovalTarget(request)}>
            {formatApprovalTarget(request)}
          </code>
        </div>
      </Flex>
      <Flex wrap gap={6} className="bm-permission-card-actions">
        <Button
          size="small"
          type="primary"
          loading={resolving}
          onClick={() => void onResolve('once')}
        >
          仅这一次
        </Button>
        <Button size="small" disabled={resolving} onClick={() => void onResolve('session')}>
          本会话允许此类操作
        </Button>
        <Button size="small" danger disabled={resolving} onClick={() => void onResolve('deny')}>
          拒绝
        </Button>
      </Flex>
    </section>
  )
}
