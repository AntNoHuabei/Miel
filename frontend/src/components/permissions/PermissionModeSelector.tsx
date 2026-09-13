import { SafetyCertificateOutlined } from '@ant-design/icons'
import { Button, Popover, Tooltip, Typography } from 'antd'
import type { PermissionMode } from './permissionTypes'
import { PERMISSION_LABELS, PERMISSION_MODES, PERMISSION_NOTES } from './permissionUtils'

const { Text } = Typography

export interface PermissionModeSelectorProps {
  mode: PermissionMode
  onChange: (mode: PermissionMode) => void | Promise<void>
  disabled?: boolean
}

export function PermissionModeSelector({ mode, onChange, disabled }: PermissionModeSelectorProps) {
  return (
    <Popover
      trigger="click"
      placement="topLeft"
      rootClassName="bm-permission-popover"
      content={
        <div className="bm-permission-panel">
          <Text strong>运行权限</Text>
          {PERMISSION_MODES.map((option) => (
            <button
              type="button"
              key={option}
              className={`bm-permission-option ${mode === option ? 'is-active' : ''}`}
              aria-pressed={mode === option}
              disabled={disabled}
              onClick={() => void onChange(option)}
            >
              <span className="bm-permission-option-title">{PERMISSION_LABELS[option]}</span>
              <span className="bm-permission-option-note">{PERMISSION_NOTES[option]}</span>
            </button>
          ))}
        </div>
      }
    >
      <Tooltip title={PERMISSION_NOTES[mode]}>
        <Button
          type="text"
          className={`bm-permission-trigger mode-${mode}`}
          icon={<SafetyCertificateOutlined />}
          disabled={disabled}
        >
          <span>{PERMISSION_LABELS[mode]}</span>
        </Button>
      </Tooltip>
    </Popover>
  )
}
