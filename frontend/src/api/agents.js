import client from './client'

export const listAgents = () => client.get('/agents/list')

export const getAgent = (id) => client.get(`/agents/detail/${id}`)

// 获取 Agent 性能指标
export const getAgentMetrics = (id) => client.get(`/agents/metrics/${id}`)

export const createAgent = (payload) => client.post('/agents/create', payload)

export const startAgent = (id) => client.post(`/agents/start/${id}`)

export const stopAgent = (id) => client.post(`/agents/stop/${id}`)

export const deleteAgent = (id, volumeIds = []) =>
  client.post(`/agents/delete/${id}`, null, {
    params: { volume_ids: volumeIds.join(',') },
  })

export const execInAgent = (id, body) => client.post(`/agents/exec/${id}`, body)

// 运行模板命令：后端增强接口
export const executeRunning = (id, variables) =>
  client.post(`/agents/execute-running/${id}`, { variables })

// 只插入执行开始标记，不执行命令
export const markExecutionStart = (id, data) =>
  client.post(`/agents/mark-execution-start/${id}`, data)

// 切换 Sandbox：后端增强接口（使用 sandbox 名称）
export const switchSandbox = (id, sandboxName) =>
  client.post(`/agents/switch-sandbox/${id}`, { sandbox_name: sandboxName })

// 获取 Agent 的 ADB 命令日志（使用 Agent 的 MappingID）
export const getAgentAdbCommandLogs = (mappingId, params) =>
  client.get(`/adb-gateway/command-logs/mapping/${mappingId}`, { params })

// 删除 ADB 命令日志
export const deleteAdbCommandLogs = (ids) =>
  client.post('/adb-gateway/command-logs/delete', { ids })

// 清空指定映射的所有命令日志
export const clearAdbCommandLogsByMapping = (mappingId) =>
  client.post(`/adb-gateway/command-logs/mapping/${mappingId}/clear`)

