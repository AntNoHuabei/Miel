import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { SettingsNavigation } from './SettingsNavigation'

describe('SettingsNavigation', () => {
  it('switches sections without mounting hidden section content', async () => {
    const onChange = vi.fn()
    render(<SettingsNavigation active="models" onChange={onChange} />)
    await userEvent.click(screen.getByRole('button', { name: /技能管理/ }))
    expect(onChange).toHaveBeenCalledWith('skills')
    expect(screen.getByRole('button', { name: /模型服务商/ })).toHaveAttribute('aria-current', 'page')
  })
})
