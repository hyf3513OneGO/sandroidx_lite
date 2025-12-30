import client from './client'

export const listSandboxes = () => client.get('/sandboxes/list')

export const getSandbox = (id) => client.get(`/sandboxes/detail/${id}`)

export const createSandbox = (payload) => client.post('/sandboxes/create', payload)

export const startSandbox = (id) => client.post(`/sandboxes/start/${id}`)

export const stopSandbox = (id) => client.post(`/sandboxes/stop/${id}`)

export const deleteSandbox = (id, volumeIds = []) =>
  client.post(`/sandboxes/delete/${id}`, null, {
    params: { volume_ids: volumeIds.join(',') },
  })

export const adbExec = (id, body) => client.post(`/sandboxes/exec/${id}`, body)

export const startScrcpy = (id) => client.post(`/sandboxes/scrcpy/${id}/start`)
export const stopScrcpy = (id) => client.post(`/sandboxes/scrcpy/${id}/stop`)
export const getScrcpyStatus = (id) =>
  client.get(`/sandboxes/scrcpy/${id}/status`)
export const getScrcpyResolution = (id) =>
  client.get(`/sandboxes/scrcpy/${id}/resolution`)

export const installApk = (id, apkId) => client.post(`/sandboxes/install-apk/${id}`, { apk_id: apkId })

// 查询 ADB 命令日志
export const getAdbCommandLogs = (mappingId, params) =>
  client.get(`/adb-gateway/command-logs/mapping/${mappingId}`, { params })

// 删除 ADB 命令日志
export const deleteAdbCommandLogs = (ids) =>
  client.post('/adb-gateway/command-logs/delete', { ids })

// 清空指定映射的所有命令日志
export const clearAdbCommandLogsByMapping = (mappingId) =>
  client.post(`/adb-gateway/command-logs/mapping/${mappingId}/clear`)

