import { describe, expect, it } from 'vitest'
import { friendlyChatError, normalizeAgentRunError } from './chatError'

describe('chat errors', () => {
  it.each([
    ['401', 'API Key 无效'],
    ['403', '无权访问当前模型'],
    ['404', '模型暂不可用'],
    ['413', '消息内容过大'],
    ['429', '请求过于频繁'],
    ['503', '模型服务暂时不可用'],
    ['network_error', '无法连接模型服务'],
  ])('maps %s to a friendly title', (code, title) => {
    expect(friendlyChatError({ code, message: 'raw detail' }).title).toBe(title)
  })

  it('keeps raw details while inferring an HTTP status', () => {
    const error = normalizeAgentRunError('POST request failed: 422 invalid parameter')
    expect(error).toEqual({ code: '422', message: 'POST request failed: 422 invalid parameter' })
  })
})
