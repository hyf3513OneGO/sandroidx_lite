import client from './client'

// 创建 Agent 分享 token（需登录）
export const createAgentShare = (id, ttlHours = 168) =>
  client.post(`/agents/share/${id}`, { ttl_hours: ttlHours })

// 获取对外分享的 Agent 信息（无需登录）
export const getSharedAgent = (token) => client.get(`/share/agents/${token}`)

// 通过分享 token 执行 Agent 的 running_commands（HTTP 版本）
export const executeSharedRunning = (token, variables) =>
  client.post(`/share/agents/${token}/execute-running`, { variables })

// 分享页撤销：持有链接即可撤销该 token
export const revokeSharedToken = (token) => client.delete(`/share/agents/${token}`)

// 分享管理（需登录）
export const getAgentShareSummary = () => client.get('/agents/share/summary')
export const listAllShares = () => client.get('/agents/share/list')
export const listSharesByAgent = (agentId) => client.get(`/agents/${agentId}/shares`)
export const revokeShare = (token) => client.delete(`/agents/share/${token}`)
