export interface AgentRunError {
  code: string
  message: string
}

export interface FriendlyChatError {
  title: string
  description: string
}

const HTTP_STATUS = /\b(400|401|403|404|408|413|422|429|5\d{2})\b/

function inferredCode(message: string) {
  const status = message.match(HTTP_STATUS)?.[1]
  if (status) return status
  const lower = message.toLowerCase()
  if (lower.includes('deadline exceeded') || lower.includes('timed out') || lower.includes('timeout')) return 'timeout'
  if (lower.includes('context canceled') || lower.includes('context cancelled') || lower.includes('request canceled')) return 'cancelled'
  if (['connection refused', 'connection reset', 'no such host', 'dial tcp', 'tls handshake', 'unexpected eof'].some((text) => lower.includes(text))) return 'network_error'
  return 'unknown_error'
}

export function normalizeAgentRunError(input: string | Partial<AgentRunError>): AgentRunError {
  const message = typeof input === 'string' ? input : String(input.message ?? '')
  const code = typeof input === 'string' ? '' : String(input.code ?? '').trim()
  return {
    code: code || inferredCode(message),
    message: message.trim() || '模型未返回错误详情',
  }
}

export function friendlyChatError(error: AgentRunError): FriendlyChatError {
  const raw = `${error.code} ${error.message}`.toLowerCase()
  const status = raw.match(HTTP_STATUS)?.[1] ?? ''
  if (status === '400' || status === '422' || raw.includes('invalid_request')) {
    return { title: '请求与当前模型不兼容', description: '请检查模型能力、思考级别或请求参数后重试。' }
  }
  if (status === '401' || raw.includes('invalid_api_key') || raw.includes('authentication')) {
    return { title: 'API Key 无效', description: '请在服务商设置中检查 API Key 后重试。' }
  }
  if (status === '403' || raw.includes('permission_denied') || raw.includes('forbidden')) {
    return { title: '无权访问当前模型', description: '当前账号或服务商不允许使用该模型，请更换模型或检查访问权限。' }
  }
  if (status === '404' || raw.includes('not_found') || raw.includes('no endpoints found')) {
    return { title: '模型暂不可用', description: '模型不存在或当前没有可用端点，请更换模型后重试。' }
  }
  if (status === '408' || error.code === 'timeout') {
    return { title: '模型响应超时', description: '模型未在限定时间内响应，请稍后重试。' }
  }
  if (status === '413') {
    return { title: '消息内容过大', description: '请缩短消息、减少上下文或移除部分附件后重试。' }
  }
  if (raw.includes('insufficient_quota')) {
    return { title: '服务商额度不足', description: '请检查账户余额或用量限制后重试。' }
  }
  if (status === '429' || raw.includes('rate_limit') || raw.includes('rate limit')) {
    return { title: '请求过于频繁', description: 'Miel 已自动重试，但服务商仍在限流，请稍后再试。' }
  }
  if (/^5\d{2}$/.test(status)) {
    return { title: '模型服务暂时不可用', description: '服务商当前异常，请稍后重试。' }
  }
  if (error.code === 'network_error') {
    return { title: '无法连接模型服务', description: '请检查网络连接和服务商地址后重试。' }
  }
  if (error.code === 'cancelled') {
    return { title: '请求已取消', description: '本次对话请求未完成。' }
  }
  return { title: '对话处理失败', description: '模型服务未能完成本次请求，请查看技术详情。' }
}
