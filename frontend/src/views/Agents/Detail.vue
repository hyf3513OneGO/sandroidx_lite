// WebSocket 辅助：附带 Authorization 头
function createWebSocketWithHeaders(url, headers = {}) {
  // 浏览器原生 WebSocket 不支持自定义 Header，这里使用 token query 兜底
  if (!headers.Authorization) {
    return new WebSocket(url)
  }
  const token = headers.Authorization.replace(/^Bearer\s+/i, '')
  // 优先用 query 传递，便于后端从 Query 读取
  const sep = url.includes('?') ? '&' : '?'
  const urlWithToken = `${url}${sep}token=${encodeURIComponent(token)}`
  return new WebSocket(urlWithToken)
}

<script setup>
import { onMounted, onBeforeUnmount, ref, computed, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { message, Modal } from 'ant-design-vue'
import { ArrowLeftOutlined } from '@ant-design/icons-vue'
import { getAgent, getAgentMetrics, switchSandbox, getAgentAdbCommandLogs, deleteAdbCommandLogs, markExecutionStart, clearAdbCommandLogsByMapping } from '../../api/agents'
import { listSandboxes, adbExec, getScrcpyStatus, startScrcpy, stopScrcpy, installApk, startSandbox, stopSandbox, getSandbox } from '../../api/sandboxes'
import { listApks } from '../../api/apks'
import { createAgentShare, listSharesByAgent, revokeShare } from '../../api/share'
import PerformanceMonitor from '../../components/PerformanceMonitor/PerformanceMonitor.vue'
import TerminalShell from '../../components/Terminal/TerminalShell.vue'
import FormBuilder from '../../components/FormBuilder/FormBuilder.vue'
import ScrcpyPlayer from '../../components/VideoPlayer/ScrcpyPlayer.vue'
import { PlayCircleOutlined, PlaySquareOutlined, DeleteOutlined, PoweroffOutlined, ReloadOutlined, ClearOutlined, DownloadOutlined } from '@ant-design/icons-vue'
import dayjs from 'dayjs'

// SSR/预渲染兼容：某些环境下没有 window
const origin = typeof window !== 'undefined' ? window?.location?.origin || '' : ''

const route = useRoute()
const router = useRouter()
const agentId = route.params.id

const loading = ref(false)
const execLoading = ref(false)
const metricsLoading = ref(false)
const agent = ref(null)
const sandboxes = ref([])

// 获取 Sandbox 名称的映射
const sandboxNameMap = computed(() => {
  const map = new Map()
  if (sandboxes.value && sandboxes.value.length > 0) {
    sandboxes.value.forEach(sandbox => {
      if (sandbox.id) {
        map.set(sandbox.id, sandbox.name || sandbox.id)
      }
    })
  }
  return map
})

// 从日志记录的 'to' 字段提取 sandbox ID（to 字段格式：sandbox-xxx:5555 或容器名:5555）
const extractSandboxIdFromTo = (toField) => {
  if (!toField) return null
  
  // to 字段格式可能是：sandbox-xxx:5555 或 容器名:5555
  // 提取容器名部分（去掉端口号）
  const parts = toField.split(':')
  if (parts.length >= 1) {
    const containerName = parts[0]
    // 如果容器名是 sandbox ID 格式，直接返回
    // 否则需要从 sandbox 列表中查找匹配的
    return containerName
  }
  
  return null
}

// 根据日志记录的 'to' 字段获取 Sandbox 名称
const getSandboxName = (log) => {
  if (!log || !log.to) return '-'
  
  // 从日志记录的 'to' 字段提取容器名/sandbox ID
  const containerNameOrId = extractSandboxIdFromTo(log.to)
  if (!containerNameOrId) return '-'
  
  // 尝试从 sandbox 列表中找到匹配的 sandbox
  // 先尝试直接匹配 ID
  if (sandboxNameMap.value.has(containerNameOrId)) {
    return sandboxNameMap.value.get(containerNameOrId)
  }
  
  // 如果直接匹配失败，尝试通过容器名匹配（sandbox 的容器名通常是 sandbox ID）
  // 或者直接显示容器名
  return containerNameOrId
}

// 根据 Sandbox ID 获取名称（用于筛选下拉框）
const getSandboxNameById = (sandboxId) => {
  if (!sandboxId) return '-'
  return sandboxNameMap.value.get(sandboxId) || sandboxId
}

// 获取所有唯一的 Sandbox ID（从日志记录的 'to' 字段）
const uniqueSandboxIds = computed(() => {
  if (!commandLogs.value || commandLogs.value.length === 0) return []
  const sandboxIds = new Set()
  commandLogs.value.forEach(log => {
    // 从日志记录的 'to' 字段提取 sandbox ID
    const sandboxId = extractSandboxIdFromTo(log.to)
    if (sandboxId) {
      sandboxIds.add(sandboxId)
    }
  })
  return Array.from(sandboxIds).sort()
})
const scrcpyStatus = ref('disconnected')

const terminalLogs = ref([])
const shellConnected = ref(false)
const shellLoading = ref(false)
const wsRef = ref(null)
const metrics = ref({ cpu: 0, memory: 0, networkIn: 0, networkOut: 0 })
let metricsTimer = null

const shareLoading = ref(false)
const sharesLoading = ref(false)
const shares = ref([])

// APK 应用商店相关
const apkLoading = ref(false)
const apkDataSource = ref([])
const selectedApkPackage = ref(null)
const installingApkId = ref(null) // 正在安装的 APK ID
const activeTab = ref('overview')
const currentSandbox = ref(null) // 当前关联的 Sandbox 信息（用于获取已安装的 APK）

// ADB 命令日志相关（使用 Agent 的 MappingID）
const commandLogsLoading = ref(false)
const commandLogs = ref([])
const commandLogsTotal = ref(0)
const commandLogsPagination = ref({
  current: 1,
  pageSize: 50,
})
const dateRange = ref([
  dayjs().subtract(1, 'day'),
  dayjs(),
])
const selectedLogIds = ref([])
const executingCommandId = ref(null)
const batchExecuting = ref(false)
const deletingLogs = ref(false)
const sandboxOperating = ref(false) // Sandbox 操作状态
const currentBatchExecutingId = ref(null) // 当前批量执行中的命令ID
const batchExecutedIds = ref(new Set()) // 已批量执行的命令ID集合

// 需要过滤掉的命令列表（支持多种格式）
const filteredCommandPatterns = [
  /^shell\s*$/,                    // 单独的 "shell"
  /^shell\s+v2/,                    // "shell v2" 或 "shell v2 ..."
  /^v2\s*$/,                        // 单独的 "v2"
  /TERM=xterm-256color/,            // 包含 TERM=xterm-256color
  /^raw:/,                          // 以 "raw:" 开头
  /^wm\s+size/,                     // "wm size" 或 "wm size ..."
]

// 过滤命令日志
const shouldFilterCommand = (command) => {
  if (!command) return true
  const cmd = command.trim()
  
  // 使用正则表达式匹配
  return filteredCommandPatterns.some(pattern => pattern.test(cmd))
}

// 计算属性确保响应式更新
const selectedLogIdsCount = computed(() => selectedLogIds.value?.length || 0)

// 受控 rowSelection（确保类型一致）
const rowSelection = computed(() => {
  return {
    selectedRowKeys: selectedLogIds.value || [],
    onChange: (keys) => {
      // 保证类型一致：全部转成字符串（与 row-key="id" 对齐）
      selectedLogIds.value = (keys || []).map(String)
    },
    preserveSelectedRowKeys: true, // 分页/刷新后保留勾选
    getCheckboxProps: (record) => {
      // 执行开始标记行不能被选中
      if (isAgentExecutionStart(record.adb_command)) {
        return {
          disabled: true,
        }
      }
      return {}
    },
  }
})
const autoRefreshEnabled = ref(false)
const autoRefreshInterval = ref(null)
const isAutoRefreshing = ref(false) // 区分自动刷新和手动查询
const selectedSandboxFilter = ref(null) // 筛选的 Sandbox ID（null 表示显示所有）

// 查询 Agent 的命令日志（使用 Agent 的 MappingID）
const fetchCommandLogs = async (silent = false, isAutoRefresh = false) => {
  if (!agentId) {
    if (!silent) message.warning('Agent ID 不存在')
    return
  }

  // 只有手动查询时才显示 loading，自动刷新时不显示（避免白屏）
  if (!isAutoRefresh) {
    commandLogsLoading.value = true
  } else {
    isAutoRefreshing.value = true
  }
  
  try {
    // 使用 Agent 的 MappingID（Agent 的操作走的是 Agent 的映射）
    const agentMappingId = agent.value?.mapping_id
    
    if (!agentMappingId) {
      if (!silent) message.warning('该 Agent 没有关联的 ADB 映射，可能尚未创建或启动')
      commandLogs.value = []
      commandLogsTotal.value = 0
      return
    }

    // 使用 ISO 8601 格式（RFC3339 兼容）
    // 如果启用了自动刷新，确保结束时间是最新的
    const endTime = autoRefreshEnabled.value ? dayjs() : (dateRange.value[1] || dayjs())
    const start = dateRange.value[0]?.toISOString() || dayjs().subtract(1, 'day').toISOString()
    const end = endTime.toISOString()
    
    const params = {
      start,
      end,
      limit: commandLogsPagination.value.pageSize,
      offset: (commandLogsPagination.value.current - 1) * commandLogsPagination.value.pageSize,
    }

    const response = await getAgentAdbCommandLogs(agentMappingId, params)
    // 处理响应数据，可能是 { data: [...], total: ... } 格式
    let allLogs = []
    if (response.data && Array.isArray(response.data)) {
      allLogs = response.data
      commandLogsTotal.value = response.data.length
    } else if (response.data?.data) {
      allLogs = response.data.data || []
      commandLogsTotal.value = response.data.total || 0
    } else {
      allLogs = []
      commandLogsTotal.value = 0
    }
    
    // 先过滤掉指定的命令
    let filteredLogs = allLogs.filter(log => !shouldFilterCommand(log.adb_command))
    
    // 再根据 Sandbox 筛选（从日志记录的 'to' 字段获取 sandbox ID）
    if (selectedSandboxFilter.value) {
      filteredLogs = filteredLogs.filter(log => {
        // 从日志记录的 'to' 字段提取 sandbox ID
        const sandboxId = extractSandboxIdFromTo(log.to)
        return sandboxId === selectedSandboxFilter.value
      })
    }
    commandLogs.value = filteredLogs.map(log => ({
      ...log,
      id: String(log.id || log.ID) // 确保 id 是字符串
    }))
    commandLogsTotal.value = filteredLogs.length
  } catch (err) {
    if (!silent) message.error(err.message || '查询命令日志失败')
  } finally {
    if (!isAutoRefresh) {
      commandLogsLoading.value = false
    } else {
      isAutoRefreshing.value = false
    }
  }
}

// 启动自动刷新
const startAutoRefresh = () => {
  if (autoRefreshInterval.value) return
  
  autoRefreshEnabled.value = true
  // 每 3 秒自动刷新一次
  autoRefreshInterval.value = setInterval(() => {
    if (activeTab.value === 'command-logs' && agent.value?.mapping_id) {
      fetchCommandLogs(true, true) // 静默刷新，不显示错误消息，标记为自动刷新
    }
  }, 3000)
}

// 停止自动刷新
const stopAutoRefresh = () => {
  autoRefreshEnabled.value = false
  if (autoRefreshInterval.value) {
    clearInterval(autoRefreshInterval.value)
    autoRefreshInterval.value = null
  }
}

// 执行单条命令
const executeCommand = async (log) => {
  if (!log?.adb_command || !agent.value?.sandbox_id) return
  
  executingCommandId.value = log.id
  try {
    // 确保命令格式正确（如果命令不包含 "shell " 前缀，则添加）
    let cmd = log.adb_command.trim()
    if (!cmd.startsWith('shell ') && !cmd.startsWith('adb ')) {
      cmd = `shell ${cmd}`
    }
    
    await adbExec(agent.value.sandbox_id, { command: cmd })
    message.success('命令执行成功')
  } catch (err) {
    message.error(err.message || '命令执行失败')
  } finally {
    executingCommandId.value = null
  }
}

// 批量执行命令（从某条到某条）
const executeBatchCommands = async () => {
  if (selectedLogIds.value.length < 1) {
    message.warning('请至少选择一条命令')
    return
  }

  if (!agent.value?.sandbox_id) {
    message.warning('Agent 未关联 Sandbox，无法执行命令')
    return
  }

  // 根据时间排序，确保按顺序执行
  const selectedLogs = commandLogs.value
    .filter(log => selectedLogIds.value.includes(String(log.id)))
    .sort((a, b) => new Date(a.time) - new Date(b.time))

  if (selectedLogs.length < 1) {
    message.warning('请选择至少一条命令')
    return
  }

  batchExecuting.value = true
  currentBatchExecutingId.value = null
  batchExecutedIds.value.clear()
  const hideLoading = message.loading('正在批量执行命令...', 0)

  try {
    let successCount = 0
    let failCount = 0
    
    for (const log of selectedLogs) {
      if (!log?.adb_command) continue
      
      const logId = String(log.id)
      
      // 设置当前执行的命令ID（在执行前设置，让用户看到高亮）
      currentBatchExecutingId.value = logId
      // 等待 Vue 更新 DOM 以显示高亮效果
      await nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))
      
      let cmd = log.adb_command.trim()
      if (!cmd.startsWith('shell ') && !cmd.startsWith('adb ')) {
        cmd = `shell ${cmd}`
      }
      
      try {
        // 执行命令 - 严格等待前一个命令完全执行完成（包括响应返回）后再执行下一个
        // await 会等待 HTTP 响应完全返回，确保前一个命令执行完成
        const response = await adbExec(agent.value.sandbox_id, { command: cmd })
        
        // 检查响应状态，确保命令执行成功
        // 后端直接返回 AdbExecResponse，格式为 { exit_code, output, error, results }
        const responseData = response?.data || {}
        
        // 如果后端返回错误信息，抛出异常以停止后续执行
        if (responseData.error) {
          throw new Error(responseData.error)
        }
        
        // 检查 exit code，非 0 表示命令执行失败
        if (responseData.exit_code !== undefined && responseData.exit_code !== 0) {
          const errorMsg = responseData.error || `命令执行失败，退出码: ${responseData.exit_code}`
          throw new Error(errorMsg)
        }
        
        successCount++
        
        // 标记为已执行
        batchExecutedIds.value.add(logId)
        // 等待 Vue 更新 DOM
        await nextTick()
        // 延迟一下以便看到已执行的高亮效果
        await new Promise(resolve => setTimeout(resolve, 300))
        currentBatchExecutingId.value = null
        await nextTick()
        
        // 每条命令之间稍微延迟，避免执行过快
        await new Promise(resolve => setTimeout(resolve, 100))
      } catch (cmdErr) {
        // 单个命令执行失败，停止后续执行
        failCount++
        currentBatchExecutingId.value = null
        await nextTick()
        
        const errorMsg = cmdErr?.message || cmdErr?.toString() || '命令执行失败'
        message.error(`命令执行失败: ${cmd} - ${errorMsg}`)
        
        // 停止后续执行
        throw new Error(`批量执行中断: ${errorMsg}`)
      }
    }
    
    hideLoading()
    if (failCount === 0) {
      message.success(`成功执行 ${successCount} 条命令`)
    } else {
      message.warning(`执行完成: 成功 ${successCount} 条，失败 ${failCount} 条`)
    }
    selectedLogIds.value = []
    // 清空执行状态
    currentBatchExecutingId.value = null
    batchExecutedIds.value.clear()
  } catch (err) {
    hideLoading()
    message.error(err.message || '批量执行失败')
    currentBatchExecutingId.value = null
  } finally {
    batchExecuting.value = false
  }
}

// 批量删除命令日志
const deleteBatchCommandLogs = async () => {
  if (selectedLogIds.value.length < 1) {
    message.warning('请至少选择一条命令')
    return
  }

  Modal.confirm({
    title: '确认删除',
    content: `确定要删除选中的 ${selectedLogIds.value.length} 条命令日志吗？`,
    okText: '确定',
    cancelText: '取消',
    onOk: async () => {
      deletingLogs.value = true
      const hideLoading = message.loading('正在删除...', 0)

      try {
        // 将字符串 ID 转换为数字
        const ids = selectedLogIds.value.map(id => parseInt(id)).filter(id => !isNaN(id))
        
        await deleteAdbCommandLogs(ids)
        hideLoading()
        message.success(`成功删除 ${ids.length} 条命令日志`)
        
        // 清空选择
        selectedLogIds.value = []
        
        // 刷新列表
        await fetchCommandLogs(false, false)
      } catch (err) {
        hideLoading()
        message.error(err.message || '删除失败')
      } finally {
        deletingLogs.value = false
      }
    },
  })
}

// 删除单条命令日志
const deleteCommandLog = async (log) => {
  Modal.confirm({
    title: '确认删除',
    content: '确定要删除这条命令日志吗？',
    okText: '确定',
    cancelText: '取消',
    onOk: async () => {
      deletingLogs.value = true
      const hideLoading = message.loading('正在删除...', 0)

      try {
        const id = parseInt(log.id)
        if (isNaN(id)) {
          throw new Error('无效的日志 ID')
        }
        
        await deleteAdbCommandLogs([id])
        hideLoading()
        message.success('删除成功')
        
        // 刷新列表
        await fetchCommandLogs(false, false)
      } catch (err) {
        hideLoading()
        message.error(err.message || '删除失败')
      } finally {
        deletingLogs.value = false
      }
    },
  })
}

// 处理分页变化
const handleCommandLogsTableChange = (pagination) => {
  commandLogsPagination.value = {
    current: pagination.current,
    pageSize: pagination.pageSize,
  }
  fetchCommandLogs(false, false)
}

// 判断是否是 Agent 执行开始标记
const isAgentExecutionStart = (command) => {
  if (!command || typeof command !== 'string') return false
  return command.trim().startsWith('=== Agent 执行开始 ===')
}

// 提取变量信息（简化显示）
const extractVariables = (command) => {
  if (!command || !command.includes('变量:')) return ''
  const lines = command.split('\n')
  const varLines = lines.filter(line => {
    const trimmed = line.trim()
    return trimmed.includes('=') && !trimmed.startsWith('===') && !trimmed.startsWith('变量:')
  })
  if (varLines.length === 0) return ''
  // 只显示变量部分，格式化为简洁形式
  return varLines.map(line => line.trim()).join('; ')
}

// 获取行的类名（用于高亮显示执行状态）
const getRowClassName = (record) => {
  try {
    if (!record || !record.id) return ''
    const id = String(record.id)
    
    // 当前正在执行的命令
    if (currentBatchExecutingId.value === id) {
      console.log('[DEBUG] getRowClassName: batch-executing-row', { id, currentBatchExecutingId: currentBatchExecutingId.value })
      return 'batch-executing-row'
    }
    
    // 已执行的命令
    if (batchExecutedIds.value && batchExecutedIds.value instanceof Set && batchExecutedIds.value.has(id)) {
      return 'batch-executed-row'
    }
    
    // Agent 执行开始标记行
    if (isAgentExecutionStart(record.adb_command)) {
      return 'agent-execution-start-row'
    }
    
    return ''
  } catch (e) {
    console.error('getRowClassName error:', e, record)
    return ''
  }
}

// 清空当前映射的所有命令日志
const clearAllCommandLogs = async () => {
  if (!agent.value?.mapping_id) {
    message.warning('当前 Agent 没有关联的映射，无法清空日志')
    return
  }

  Modal.confirm({
    title: '确认清空',
    content: '确定要清空当前映射的所有命令日志吗？此操作不可恢复！',
    okText: '确定',
    cancelText: '取消',
    okType: 'danger',
    onOk: async () => {
      deletingLogs.value = true
      const hideLoading = message.loading('正在清空...', 0)

      try {
        await clearAdbCommandLogsByMapping(agent.value.mapping_id)
        hideLoading()
        message.success('成功清空所有命令日志')
        
        // 清空选择
        selectedLogIds.value = []
        
        // 刷新列表
        await fetchCommandLogs(false, false)
      } catch (err) {
        hideLoading()
        message.error(err.message || '清空失败')
      } finally {
        deletingLogs.value = false
      }
    },
  })
}

// 导出选中的命令日志
const exportSelectedCommandLogs = () => {
  if (selectedLogIds.value.length < 1) {
    message.warning('请至少选择一条命令日志')
    return
  }

  // 获取选中的日志
  const selectedLogs = commandLogs.value.filter(log => 
    selectedLogIds.value.includes(String(log.id))
  )

  if (selectedLogs.length === 0) {
    message.warning('未找到选中的命令日志')
    return
  }

  // 按时间排序
  const sortedLogs = selectedLogs.sort((a, b) => 
    new Date(a.time) - new Date(b.time)
  )

  // 构造导出数据
  const exportData = {
    exportTime: new Date().toISOString(),
    agentId: agent.value?.id || '',
    agentName: agent.value?.name || '',
    mappingId: agent.value?.mapping_id || '',
    totalCount: sortedLogs.length,
    logs: sortedLogs.map(log => ({
      id: log.id,
      time: log.time,
      sandbox: getSandboxName(log),
      command: log.adb_command,
      connectionId: log.connection_id || '',
    }))
  }

  // 转换为 JSON 字符串
  const jsonStr = JSON.stringify(exportData, null, 2)
  
  // 创建 Blob 对象
  const blob = new Blob([jsonStr], { type: 'application/json;charset=utf-8' })
  
  // 创建下载链接
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  
  // 生成文件名（包含时间戳）
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, -5)
  link.download = `command-logs-${timestamp}.json`
  
  // 触发下载
  document.body.appendChild(link)
  link.click()
  
  // 清理
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
  
  message.success(`成功导出 ${sortedLogs.length} 条命令日志`)
}

const fetchShares = async () => {
  if (!agentId) return
  sharesLoading.value = true
  try {
    const { data } = await listSharesByAgent(agentId)
    shares.value = data?.shares || []
  } catch (_) {
    shares.value = []
  } finally {
    sharesLoading.value = false
  }
}

const handleCreateShare = async () => {
  if (!agentId) return
  shareLoading.value = true
  try {
    const { data } = await createAgentShare(agentId, 168)
    const path = data?.share_path || ''
    const url = path ? `${origin}${path}` : ''
    if (!url) {
      message.error('生成分享链接失败')
      return
    }
    try {
      await navigator.clipboard.writeText(url)
      message.success('分享链接已复制')
    } catch (_) {
      // fallback：仍提示用户手动复制
      message.success(`分享链接：${url}`)
    }
    await fetchShares()
  } catch (err) {
    message.error(err.message || '生成分享失败')
  } finally {
    shareLoading.value = false
  }
}

const copyShareUrl = async (sharePath) => {
  const url = sharePath ? `${origin}${sharePath}` : ''
  if (!url) return
  try {
    await navigator.clipboard.writeText(url)
    message.success('已复制')
  } catch (_) {
    message.success(url)
  }
}

const openShareUrl = (sharePath) => {
  const url = sharePath ? `${origin}${sharePath}` : ''
  if (!url) return
  if (typeof window === 'undefined') return
  window.open(url, '_blank', 'noopener,noreferrer')
}

const handleRevokeShare = async (token) => {
  if (!token) return
  try {
    await revokeShare(token)
    message.success('已撤销分享')
    await fetchShares()
  } catch (err) {
    message.error(err.message || '撤销失败')
  }
}

const fetchAgent = async () => {
  loading.value = true
  try {
    const { data } = await getAgent(agentId)
    agent.value = data
    // 如果 Agent 有关联的 Sandbox，获取 Sandbox 信息（用于显示已安装的 APK）
    if (data?.sandbox_id) {
      try {
        const { data: sandboxData } = await getSandbox(data.sandbox_id)
        currentSandbox.value = sandboxData
        // 如果 installed_apk_ids 是字符串，尝试解析为数组
        if (currentSandbox.value && typeof currentSandbox.value.installed_apk_ids === 'string') {
          try {
            currentSandbox.value.installed_apk_ids = JSON.parse(currentSandbox.value.installed_apk_ids)
          } catch (e) {
            console.error('Failed to parse installed_apk_ids:', e)
          }
        }
      } catch (err) {
        console.warn('获取 Sandbox 信息失败:', err)
        currentSandbox.value = null
      }
    } else {
      currentSandbox.value = null
    }
  } catch (err) {
    const errorMessage = err.message || '加载 Agent 失败'
    message.error(errorMessage)
    // 如果 agent 不存在（404 或相关错误），跳转到列表页
    if (err.response?.status === 404 || errorMessage.includes('不存在') || errorMessage.includes('not found')) {
      setTimeout(() => {
        router.push('/agents')
      }, 1500)
    }
  } finally {
    loading.value = false
  }
}

const fetchSandboxes = async () => {
  try {
    const { data } = await listSandboxes()
    sandboxes.value = data || []
  } catch (err) {
    // 静默
  }
}

const handleExecute = async (variables, commandPreviews) => {
  if (!commandPreviews || commandPreviews.length === 0) {
    message.warning('没有可执行的命令')
    return
  }
  
  if (!shellConnected.value) {
    message.error('Shell 未连接，无法执行命令')
    return
  }
  
  // 检查是否有 sandbox_id，如果没有则不允许执行命令
  if (!agent.value?.sandbox_id) {
    message.warning('请先选择一个 Sandbox 才能执行命令')
    return
  }
  
  execLoading.value = true
  try {
    // 先插入执行开始标记（如果有 mapping_id）
    if (agent.value?.mapping_id) {
      try {
        // 调用专门的 API 来只插入执行开始标记，不执行命令
        await markExecutionStart(agentId, {
          variables,
          sandbox_id: agent.value?.sandbox_id || ''
        })
      } catch (err) {
        // 忽略插入标记的错误，继续执行命令
        console.warn('插入执行开始标记失败:', err)
      }
    }
    
    // 等待一小段时间，确保标记已插入
    await new Promise(resolve => setTimeout(resolve, 200))
    
    // 将所有预览的命令发送到 Shell
    for (const cmd of commandPreviews) {
      sendShell(cmd)
      // 等待一小段时间，避免命令发送过快
      await new Promise(resolve => setTimeout(resolve, 100))
    }
    
    message.success(`已发送 ${commandPreviews.length} 条命令到 Shell`)
    
    // 刷新命令日志，以便显示新插入的执行开始标记
    await fetchCommandLogs(false, false)
  } catch (err) {
    message.error(err.message || '执行失败')
  } finally {
    execLoading.value = false
  }
}

const parseNumber = (val) => {
  const num = Number(val)
  if (Number.isNaN(num) || !Number.isFinite(num)) return 0
  return Math.round(num * 100) / 100
}

const fetchMetrics = async (showLoading = false) => {
  if (!agentId) return
  if (showLoading) metricsLoading.value = true
  try {
    const { data } = await getAgentMetrics(agentId)
    metrics.value = {
      cpu: parseNumber(data?.cpu),
      memory: parseNumber(data?.memory),
      networkIn: parseNumber(data?.network_in),
      networkOut: parseNumber(data?.network_out),
    }
  } catch (err) {
    // 静默处理，避免频繁弹出
    console.warn('获取性能指标失败', err)
  } finally {
    if (showLoading) metricsLoading.value = false
  }
}

const stopMetricsPolling = () => {
  if (metricsTimer) {
    clearInterval(metricsTimer)
    metricsTimer = null
  }
}

const startMetricsPolling = async () => {
  stopMetricsPolling()
  await fetchMetrics(true)
  metricsTimer = setInterval(fetchMetrics, 5000)
}

const handleSwitchSandbox = async (sandboxName) => {
  if (!sandboxName || !agent.value) return
  try {
    await switchSandbox(agent.value.id, sandboxName)
    // 本地更新，确保 Scrcpy/状态使用最新 sandbox
    agent.value.sandbox_id = sandboxName
    scrcpyStatus.value = 'disconnected'
    
    // 更新 Sandbox 信息（用于显示已安装的 APK）
    if (sandboxName) {
      try {
        const { data: sandboxData } = await getSandbox(sandboxName)
        currentSandbox.value = sandboxData
        // 如果 installed_apk_ids 是字符串，尝试解析为数组
        if (currentSandbox.value && typeof currentSandbox.value.installed_apk_ids === 'string') {
          try {
            currentSandbox.value.installed_apk_ids = JSON.parse(currentSandbox.value.installed_apk_ids)
          } catch (e) {
            console.error('Failed to parse installed_apk_ids:', e)
          }
        }
      } catch (err) {
        console.warn('获取 Sandbox 信息失败:', err)
        currentSandbox.value = null
      }
    } else {
      currentSandbox.value = null
    }
    
    message.success('已切换到目标 Sandbox')
    // 可选：切换后主动刷新 scrcpy 状态
    await refreshScrcpy()
  } catch (err) {
    message.error(err.message || '切换失败')
  }
}

// 启动 Sandbox
const handleStartSandbox = async () => {
  if (!agent.value?.sandbox_id) {
    message.warning('请先选择 Sandbox')
    return
  }
  
  sandboxOperating.value = true
  const hideLoading = message.loading('正在启动 Sandbox...', 0)
  
  try {
    await startSandbox(agent.value.sandbox_id)
    hideLoading()
    message.success('Sandbox 启动成功')
    // 刷新 Agent 信息以获取最新状态
    await fetchAgent()
  } catch (err) {
    hideLoading()
    message.error(err.message || '启动失败')
  } finally {
    sandboxOperating.value = false
  }
}

// 停止 Sandbox
const handleStopSandbox = async () => {
  if (!agent.value?.sandbox_id) {
    message.warning('请先选择 Sandbox')
    return
  }
  
  Modal.confirm({
    title: '确认停止',
    content: '确定要停止当前 Sandbox 吗？',
    okText: '确定',
    cancelText: '取消',
    onOk: async () => {
      sandboxOperating.value = true
      const hideLoading = message.loading('正在停止 Sandbox...', 0)
      
      try {
        await stopSandbox(agent.value.sandbox_id)
        hideLoading()
        message.success('Sandbox 已停止')
        // 停止后断开 Scrcpy 连接
        scrcpyStatus.value = 'disconnected'
        // 刷新 Agent 信息以获取最新状态
        await fetchAgent()
      } catch (err) {
        hideLoading()
        message.error(err.message || '停止失败')
      } finally {
        sandboxOperating.value = false
      }
    },
  })
}

// 内部重启函数（不显示确认对话框）
const restartSandboxInternal = async () => {
  if (!agent.value?.sandbox_id) {
    throw new Error('请先选择 Sandbox')
  }
  
  sandboxOperating.value = true
  const hideLoading = message.loading('正在重启 Sandbox...', 0)
  
  try {
    // 先停止
    await stopSandbox(agent.value.sandbox_id)
    // 等待一下再启动
    await new Promise(resolve => setTimeout(resolve, 1000))
    // 再启动
    await startSandbox(agent.value.sandbox_id)
    hideLoading()
    message.success('Sandbox 重启成功')
    // 重启后断开 Scrcpy 连接
    scrcpyStatus.value = 'disconnected'
    // 刷新 Agent 信息以获取最新状态
    await fetchAgent()
  } catch (err) {
    hideLoading()
    throw err
  } finally {
    sandboxOperating.value = false
  }
}

// 重启 Sandbox（带确认对话框）
const handleRestartSandbox = async () => {
  if (!agent.value?.sandbox_id) {
    message.warning('请先选择 Sandbox')
    return
  }
  
  Modal.confirm({
    title: '确认重启',
    content: '确定要重启当前 Sandbox 吗？这将先停止然后启动 Sandbox。',
    okText: '确定',
    cancelText: '取消',
    onOk: async () => {
      try {
        await restartSandboxInternal()
      } catch (err) {
        message.error(err.message || '重启失败')
      }
    },
  })
}

const refreshScrcpy = async () => {
  if (!agent.value?.sandbox_id) return
  try {
    const { data } = await getScrcpyStatus(agent.value.sandbox_id)
    scrcpyStatus.value = data?.active ? 'playing' : 'disconnected'
  } catch {
    scrcpyStatus.value = 'disconnected'
  }
}

const startScrcpySession = async () => {
  if (!agent.value?.sandbox_id) return
  try {
    scrcpyStatus.value = 'connecting'
    await startScrcpy(agent.value.sandbox_id)
    scrcpyStatus.value = 'playing'
  } catch (err) {
    scrcpyStatus.value = 'disconnected'
    const errorMsg = err.message || '启动失败'
    message.error(errorMsg)
    
    // 连接失败时询问是否重启 Sandbox
    Modal.confirm({
      title: 'Scrcpy 连接失败',
      content: `${errorMsg}。是否重启 Sandbox 后重试？`,
      okText: '重启并重试',
      cancelText: '取消',
      onOk: async () => {
        try {
          // 先重启 Sandbox（不显示确认对话框）
          await restartSandboxInternal()
          // 等待 Sandbox 完全启动（包括 ADB 就绪）
          message.info('等待 Sandbox 完全启动...')
          await new Promise(resolve => setTimeout(resolve, 5000))
          // 然后自动重新尝试连接 Scrcpy
          message.info('正在自动连接 Scrcpy...')
          await startScrcpySession()
        } catch (restartErr) {
          message.error(restartErr.message || '重启失败，无法重试连接')
        }
      },
    })
  }
}

const stopScrcpySession = async () => {
  if (!agent.value?.sandbox_id) return
  try {
    await stopScrcpy(agent.value.sandbox_id)
    scrcpyStatus.value = 'disconnected'
  } catch (err) {
    message.error(err.message || '停止失败')
  }
}

onMounted(async () => {
  await fetchAgent()
  await fetchSandboxes()
  await fetchShares()
  await refreshScrcpy()
  await startMetricsPolling()
  connectShell()
  // 如果默认 tab 是 apks，则加载 APK 列表
  if (activeTab.value === 'apks') {
    await fetchApks()
  }
})

onBeforeUnmount(() => {
  stopMetricsPolling()
  stopAutoRefresh() // 清理自动刷新定时器
  if (wsRef.value) {
    wsRef.value.close()
  }
})

// WebSocket 辅助：附带 Authorization 头（浏览器原生 WS 无自定义 Header，使用 query 兜底）
const createWebSocketWithHeaders = (url, headers = {}) => {
  if (!headers.Authorization) {
    return new WebSocket(url)
  }
  const token = headers.Authorization.replace(/^Bearer\s+/i, '')
  const sep = url.includes('?') ? '&' : '?'
  const urlWithToken = `${url}${sep}token=${encodeURIComponent(token)}`
  return new WebSocket(urlWithToken)
}

const connectShell = () => {
  if (!agentId) return
  
  // 关闭旧连接
  if (wsRef.value) {
    wsRef.value.close()
  }
  shellLoading.value = true
  shellConnected.value = false
  terminalLogs.value = ['连接中...']

  const token = localStorage.getItem('sandroidx_token') || ''
  const wsBase = origin ? origin.replace(/^http/, 'ws') : ''
  if (!wsBase) {
    shellLoading.value = false
    shellConnected.value = false
    terminalLogs.value.push('当前环境缺少 location.origin，无法建立 WebSocket 连接')
    return
  }
  const url = `${wsBase}/api/v1/agents/shell/${agentId}?shell=/bin/sh`
  const headers = token ? { Authorization: `Bearer ${token}` } : undefined
  const ws = createWebSocketWithHeaders(url, headers)
  wsRef.value = ws

  ws.onopen = () => {
    shellConnected.value = true
    shellLoading.value = false
    terminalLogs.value = ['已连接，输入命令回车发送']
  }
  
  // 追加流式 delta，只有遇到真正的 \n 才创建新行
  const appendDelta = (delta) => {
    // 移除 \r（避免终端控制字符干扰）
    const cleanDelta = delta.replace(/\r/g, '')
    if (!cleanDelta) return
    
    // 确保至少有一条 log
    if (terminalLogs.value.length === 0) {
      terminalLogs.value.push('')
    }
    
    // 追加到最后一条
    terminalLogs.value[terminalLogs.value.length - 1] += cleanDelta
    
    // 如果 delta 里带换行，把最后一条按换行拆开
    const last = terminalLogs.value[terminalLogs.value.length - 1]
    const parts = last.split('\n')
    terminalLogs.value[terminalLogs.value.length - 1] = parts[0]
    for (let i = 1; i < parts.length; i++) {
      terminalLogs.value.push(parts[i])
    }
  }
  
  ws.onmessage = (evt) => {
    const text = typeof evt.data === 'string' ? evt.data : ''
    if (text) appendDelta(text)
  }
  ws.onerror = (evt) => {
    shellConnected.value = false
    shellLoading.value = false
    terminalLogs.value.push('连接错误，检查 Agent 是否运行 / token 是否有效')
    console.error('ws error', evt)
  }
  ws.onclose = (evt) => {
    shellConnected.value = false
    shellLoading.value = false
    terminalLogs.value.push(`连接已关闭 (code=${evt.code})`)
  }
}

const sendShell = (cmd) => {
  if (!wsRef.value || wsRef.value.readyState !== WebSocket.OPEN) {
    message.error('Shell 未连接')
    return
  }
  
  // 检查是否有 sandbox_id，如果没有则不允许发送命令
  if (!agent.value?.sandbox_id) {
    message.warning('请先选择一个 Sandbox 才能发送命令')
    return
  }
  
  const payload = cmd.endsWith('\n') ? cmd : `${cmd}\n`
  wsRef.value.send(payload)
  // 不在这里显示命令，让后端的 ECHO 机制来显示（避免重复）
}

// 发送原始数据（不补换行），用于 Ctrl+C / 未来的控制字符
const sendShellRaw = (payload) => {
  if (!wsRef.value || wsRef.value.readyState !== WebSocket.OPEN) {
    message.error('Shell 未连接')
    return
  }
  wsRef.value.send(payload)
  // 不在这里显示控制字符，让后端的 ECHO 机制来显示（避免重复）
}

const clearShellLogs = () => {
  terminalLogs.value = []
}

// Scrcpy 交互：不写入 Shell（Shell 仅用于手动调试），改走 /sandboxes/exec/:id 静默执行
const scrcpyExecQueue = []
let scrcpyExecRunning = false
const normalizeExecCmd = (cmd) => {
  const c = String(cmd || '').trim()
  if (!c) return ''
  // Scrcpy 端通常发的是 "input ..."，exec 端需要 "shell input ..."
  return c.startsWith('shell ') ? c : `shell ${c}`
}
const enqueueScrcpyExec = (cmd) => {
  const c = normalizeExecCmd(cmd)
  if (!c) return
  scrcpyExecQueue.push(c)
  pumpScrcpyExec()
}
const pumpScrcpyExec = async () => {
  if (scrcpyExecRunning) return
  const sandboxId = agent.value?.sandbox_id
  if (!sandboxId) return
  scrcpyExecRunning = true
  try {
    while (scrcpyExecQueue.length) {
      const c = scrcpyExecQueue.shift()
      await adbExec(sandboxId, { command: c })
    }
  } catch (err) {
    scrcpyExecQueue.length = 0
    message.error(err?.message || 'Scrcpy 交互执行失败')
  } finally {
    scrcpyExecRunning = false
  }
}
const handleScrcpyAdb = (payload) => {
  const cmd = payload?.cmd
  if (!cmd) return
  enqueueScrcpyExec(cmd)
}

// APK 应用商店相关函数
const normalizeArray = (arr) => {
  if (!arr) return []
  return Array.isArray(arr) ? arr : []
}

const fetchApks = async () => {
  apkLoading.value = true
  try {
    const { data } = await listApks()
    apkDataSource.value = normalizeArray(data)
  } catch (err) {
    message.error(err.message || '加载 APK 列表失败')
    apkDataSource.value = []
  } finally {
    apkLoading.value = false
  }
}

const groupedApks = computed(() => {
  const groups = {}
  for (const apk of apkDataSource.value) {
    const pkg = apk.package_name || 'unknown'
    if (!groups[pkg]) {
      groups[pkg] = {
        package_name: pkg,
        name: apk.name || pkg,
        icon: apk.icon || '',
        versions: [],
      }
    }
    groups[pkg].versions.push(apk)
  }
  return Object.values(groups)
})

const currentApkVersions = computed(() => {
  if (!selectedApkPackage.value) return []
  return apkDataSource.value.filter((apk) => apk.package_name === selectedApkPackage.value)
})

const handleViewApkPackage = (packageName) => {
  selectedApkPackage.value = packageName
}

const handleBackApkList = () => {
  selectedApkPackage.value = null
}

const handleInstallApk = async (apkRecord) => {
  if (!apkRecord?.id) return
  if (!agent.value?.sandbox_id) {
    message.error('当前 Agent 未关联 Sandbox，无法安装 APK')
    return
  }
  if (installingApkId.value) return // 如果正在安装，忽略重复点击

  installingApkId.value = apkRecord.id
  const hideLoading = message.loading('正在安装 APK...', 0)

  try {
    await installApk(agent.value.sandbox_id, apkRecord.id)
    hideLoading()
    message.success('APK 安装成功')
    // 安装成功后刷新 Sandbox 信息以更新已安装列表
    if (agent.value?.sandbox_id) {
      try {
        const { data: sandboxData } = await getSandbox(agent.value.sandbox_id)
        currentSandbox.value = sandboxData
        // 如果 installed_apk_ids 是字符串，尝试解析为数组
        if (currentSandbox.value && typeof currentSandbox.value.installed_apk_ids === 'string') {
          try {
            currentSandbox.value.installed_apk_ids = JSON.parse(currentSandbox.value.installed_apk_ids)
          } catch (e) {
            console.error('Failed to parse installed_apk_ids:', e)
          }
        }
      } catch (err) {
        console.warn('刷新 Sandbox 信息失败:', err)
      }
    }
  } catch (err) {
    hideLoading()
    message.error(err.message || '安装失败')
  } finally {
    installingApkId.value = null
  }
}

// 判断 APK 是否已安装（从关联的 Sandbox 中获取）
const isApkInstalled = (apkId) => {
  if (!apkId) return false
  if (!currentSandbox.value) return false
  
  let installedIds = currentSandbox.value.installed_apk_ids || currentSandbox.value.installedApkIds
  
  // 如果是字符串，尝试解析为数组
  if (typeof installedIds === 'string') {
    try {
      installedIds = JSON.parse(installedIds)
    } catch (e) {
      console.error('Failed to parse installed_apk_ids as JSON:', e)
      return false
    }
  }
  
  if (!Array.isArray(installedIds)) {
    return false
  }
  
  // 确保 ID 类型一致（都转为字符串比较）
  const apkIdStr = String(apkId)
  return installedIds.map(String).includes(apkIdStr)
}

// 判断包名是否有任何版本已安装
const hasInstalledVersion = (packageName) => {
  let installedIds = currentSandbox.value?.installed_apk_ids || currentSandbox.value?.installedApkIds
  
  // 如果是字符串，尝试解析为数组
  if (typeof installedIds === 'string') {
    try {
      installedIds = JSON.parse(installedIds)
    } catch (e) {
      return false
    }
  }
  
  if (!Array.isArray(installedIds)) {
    return false
  }
  
  const group = groupedApks.value.find((g) => g.package_name === packageName)
  if (!group) return false
  const installedIdsStr = installedIds.map(String)
  return group.versions.some(apk => installedIdsStr.includes(String(apk.id)))
}

const handleCreateNewApkVersion = () => {
  if (!selectedApkPackage.value) return
  const currentGroup = groupedApks.value.find((g) => g.package_name === selectedApkPackage.value)
  const packageName = selectedApkPackage.value
  const name = currentGroup?.name || packageName
  router.push({
    path: '/apks/create',
    query: { package_name: packageName, name },
  })
}

const getIconUrl = (iconPath) => {
  if (!iconPath) return null
  const baseURL = import.meta.env.VITE_API_BASE_URL || ''
  const iconFile = iconPath.split('/').pop()
  return `${baseURL}/api/v1/apks/icon/${iconFile}`
}

</script>

<template>
  <div class="page-container detail-page">
    <div class="page-title">
      <div class="title-row">
        <div>
          <h1>{{ agent?.name || agent?.id || 'Agent 详情' }}</h1>
          <p class="muted">性能监控、Shell、运行命令、视频流与 Sandbox 切换</p>
        </div>
        <a-space>
          <a-button type="primary" :loading="shareLoading" @click="handleCreateShare">分享（只读链接）</a-button>
        </a-space>
      </div>
    </div>

    <a-row :gutter="[24, 24]">
      <a-col :xs="24" :lg="16">
        <a-space direction="vertical" :size="24" style="width: 100%">
          <PerformanceMonitor :metrics="metrics" :loading="loading || metricsLoading" />

          <a-tabs v-model:activeKey="activeTab" class="detail-tabs" @change="(key) => { 
            if (key === 'apks') fetchApks()
            if (key === 'command-logs') {
              fetchCommandLogs(false, false)
              startAutoRefresh()
            } else {
              stopAutoRefresh()
            }
          }">
            <a-tab-pane key="overview" tab="概览">
              <a-space direction="vertical" :size="16" style="width: 100%">
                <a-card title="基础信息" bordered>
                  <a-descriptions size="small" :column="2">
                    <a-descriptions-item label="Agent ID">{{ agent?.id || '-' }}</a-descriptions-item>
                    <a-descriptions-item label="名称">{{ agent?.name || '-' }}</a-descriptions-item>
                    <a-descriptions-item label="状态">{{ agent?.status || '-' }}</a-descriptions-item>
                    <a-descriptions-item label="Sandbox">{{ agent?.sandbox_id || '-' }}</a-descriptions-item>
                    <a-descriptions-item label="镜像">{{ agent?.image || '-' }}</a-descriptions-item>
                    <a-descriptions-item label="Container">{{ agent?.container_id || '-' }}</a-descriptions-item>
                    <a-descriptions-item label="Mapping">{{ agent?.mapping_id || '-' }}</a-descriptions-item>
                    <a-descriptions-item label="创建时间">{{ agent?.created_at || '-' }}</a-descriptions-item>
                    <a-descriptions-item label="更新时间">{{ agent?.updated_at || '-' }}</a-descriptions-item>
                  </a-descriptions>
                  <a-typography-paragraph v-if="agent?.last_error" class="muted" style="margin-top: 12px">
                    <b>最近错误：</b>{{ agent.last_error }}
                  </a-typography-paragraph>
                </a-card>

                <a-card title="挂载卷（Volumes）" bordered>
                  <a-empty v-if="!(agent?.volumes && agent.volumes.length)" description="暂无挂载卷" />
                  <a-table
                    v-else
                    size="small"
                    row-key="id"
                    :data-source="agent.volumes"
                    :pagination="false"
                    :columns="[
                      { title: 'Volume ID', dataIndex: 'volume_id', key: 'volume_id' },
                      { title: '容器路径', dataIndex: 'container_path', key: 'container_path' },
                      { title: '只读', key: 'read_only' },
                      { title: '状态', dataIndex: 'status', key: 'status' },
                    ]"
                  >
                    <template #bodyCell="{ column, record }">
                      <template v-if="column.key === 'read_only'">
                        <a-tag v-if="record.read_only" color="blue">RO</a-tag>
                        <a-tag v-else color="green">RW</a-tag>
                      </template>
                    </template>
                  </a-table>
                </a-card>
              </a-space>
            </a-tab-pane>

            <a-tab-pane key="console" tab="控制台">
              <a-space direction="vertical" :size="24" style="width: 100%">
                <TerminalShell
                  title="交互式 Shell"
                  :logs="terminalLogs"
                  :connected="shellConnected"
                  :loading="shellLoading"
                  @send="sendShell"
                  @raw="sendShellRaw"
                  @clear="clearShellLogs"
                />

                <a-card title="运行命令" :loading="loading" bordered>
                  <p class="muted">根据 Running Variables 生成表单，一键执行。</p>
                  <FormBuilder
                    v-if="agent?.running_variables"
                    :variables="agent.running_variables"
                    :commands="agent.running_commands"
                    :loading="execLoading"
                    @submit="handleExecute"
                  />
                  <a-empty v-else description="暂无运行命令配置" />
                </a-card>
              </a-space>
            </a-tab-pane>

            <a-tab-pane key="share" tab="分享链接">
              <a-card title="分享链接" :loading="sharesLoading" bordered>
                <p class="muted">对外链接：可填表单执行 + 实时输出 + Scrcpy（只读）。</p>
                <a-empty v-if="!shares.length" description="暂无分享链接，点击右上角“分享（只读链接）”生成" />
                <a-table
                  v-else
                  size="small"
                  row-key="token"
                  :data-source="shares"
                  :pagination="false"
                  :columns="[
                    { title: '链接', key: 'url' },
                    { title: '创建时间', dataIndex: 'created_at', key: 'created_at' },
                    { title: '过期时间', dataIndex: 'expires_at', key: 'expires_at' },
                    { title: '状态', key: 'status' },
                    { title: '操作', key: 'action' },
                  ]"
                >
                  <template #bodyCell="{ column, record }">
                    <template v-if="column.key === 'url'">
                      <a-typography-paragraph class="mono" copyable>
                        {{ `${origin}${record.share_path || ''}` }}
                      </a-typography-paragraph>
                    </template>
                    <template v-else-if="column.key === 'expires_at'">
                      <span>{{ record.expires_at || '-' }}</span>
                    </template>
                    <template v-else-if="column.key === 'status'">
                      <a-tag v-if="record.expired" color="default">已过期</a-tag>
                      <a-tag v-else color="green">有效</a-tag>
                    </template>
                    <template v-else-if="column.key === 'action'">
                      <a-space>
                        <a-button size="small" @click="openShareUrl(record.share_path)">跳转</a-button>
                        <a-popconfirm title="确定撤销该分享？" @confirm="() => handleRevokeShare(record.token)">
                          <a-button size="small" danger>撤销</a-button>
                        </a-popconfirm>
                      </a-space>
                    </template>
                  </template>
                </a-table>
              </a-card>
            </a-tab-pane>

            <a-tab-pane key="apks" tab="APK 应用商店">
              <div class="apk-store-container">
                <div v-if="!selectedApkPackage" class="apk-store-header">
                  <h2>APK 应用商店</h2>
                  <p class="muted">选择应用并安装到当前 Sandbox</p>
                  <p v-if="!agent?.sandbox_id" class="muted" style="color: #ff4d4f;">
                    提示：当前 Agent 未关联 Sandbox，请先切换 Sandbox
                  </p>
                </div>
                <div v-else class="apk-store-header">
                  <a-button type="text" @click="handleBackApkList">
                    <template #icon>
                      <ArrowLeftOutlined />
                    </template>
                    返回
                  </a-button>
                  <h2 style="margin: 0;">{{ currentApkVersions[0]?.name || selectedApkPackage }}</h2>
                  <a-button type="primary" @click="handleCreateNewApkVersion">新建 APK</a-button>
                </div>

                <a-card bordered :loading="apkLoading">
                  <!-- 应用列表（按包名分组） -->
                  <div v-if="!selectedApkPackage" class="app-grid">
                    <div
                      v-for="group in groupedApks"
                      :key="group.package_name"
                      class="app-card"
                      @click="handleViewApkPackage(group.package_name)"
                    >
                      <div class="app-icon">
                        <img
                          v-if="getIconUrl(group.icon)"
                          :src="getIconUrl(group.icon)"
                          :alt="group.name"
                          @error="(e) => (e.target.style.display = 'none')"
                        />
                        <div v-else class="app-icon-placeholder">
                          {{ (group.name || group.package_name).charAt(0).toUpperCase() }}
                        </div>
                      </div>
                      <div class="app-info">
                        <div class="app-name">
                          {{ group.name || group.package_name }}
                          <a-tag v-if="hasInstalledVersion(group.package_name)" color="success" style="margin-left: 8px;">
                            已安装
                          </a-tag>
                        </div>
                        <div class="app-package">{{ group.package_name }}</div>
                        <div class="app-versions">{{ group.versions.length }} 个版本</div>
                      </div>
                    </div>
                  </div>

                  <!-- 版本列表 -->
                  <div v-else>
                    <a-table
                      :data-source="currentApkVersions"
                      :loading="apkLoading"
                      row-key="id"
                      :pagination="false"
                      :columns="[
                        { title: '包名', dataIndex: 'package_name', key: 'package_name', width: 200 },
                        { title: '版本', dataIndex: 'version', key: 'version', width: 120 },
                        { title: '状态', key: 'status', width: 100 },
                        { title: '类型', dataIndex: 'type', key: 'type', width: 80 },
                        { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180 },
                        { title: '操作', key: 'action', width: 150 },
                      ]"
                    >
                      <template #bodyCell="{ column, record }">
                        <template v-if="column.key === 'type'">
                          <a-tag>{{ record.type === 'remote' ? '远程' : '本地' }}</a-tag>
                        </template>

                        <template v-else-if="column.key === 'package_name'">
                          <span class="text-ellipsis">{{ record.package_name }}</span>
                        </template>

                        <template v-else-if="column.key === 'status'">
                          <a-tag v-if="isApkInstalled(record.id)" color="success">已安装</a-tag>
                          <a-tag v-else color="default">未安装</a-tag>
                        </template>

                        <template v-else-if="column.key === 'created_at'">
                          {{ new Date(record.created_at).toLocaleString() }}
                        </template>

                        <template v-else-if="column.key === 'action'">
                          <a-button
                            v-if="!isApkInstalled(record.id)"
                            type="primary"
                            size="small"
                            :loading="installingApkId === record.id"
                            :disabled="!!installingApkId || !agent?.sandbox_id"
                            @click.stop="handleInstallApk(record)"
                          >
                            安装
                          </a-button>
                          <span v-else style="color: #52c41a; font-size: 12px;">✓ 已安装</span>
                        </template>
                      </template>
                    </a-table>
                  </div>
                </a-card>
              </div>
            </a-tab-pane>
            <a-tab-pane key="command-logs" tab="命令日志">
              <a-card bordered>
                <template #title>
                  <span>ADB 命令日志（Agent 操作）</span>
                </template>

                <div v-if="!agent?.mapping_id" style="padding: 20px; text-align: center;">
                  <a-alert
                    message="该 Agent 没有关联的 ADB 映射"
                    description="可能尚未创建或启动，无法查询命令日志"
                    type="warning"
                    show-icon
                  />
                </div>
                <template v-else>
                  <div style="margin-bottom: 16px; padding: 0 4px;">
                    <a-space wrap :size="[12, 12]" style="width: 100%; margin-bottom: 8px;">
                      <a-range-picker
                        v-model:value="dateRange"
                        show-time
                        format="YYYY-MM-DD HH:mm:ss"
                        @change="() => { 
                          stopAutoRefresh()
                          fetchCommandLogs(false, false)
                        }"
                      />
                      <a-select
                        v-model:value="selectedSandboxFilter"
                        placeholder="筛选 Sandbox"
                        allow-clear
                        style="width: 200px"
                        @change="() => {
                          // 筛选时重置到第一页
                          commandLogsPagination.value.current = 1
                        }"
                      >
                        <a-select-option :value="null">全部 Sandbox</a-select-option>
                        <a-select-option
                          v-for="sandboxId in uniqueSandboxIds"
                          :key="sandboxId"
                          :value="sandboxId"
                        >
                          {{ getSandboxNameById(sandboxId) }}
                        </a-select-option>
                      </a-select>
                      <a-button type="primary" @click="() => fetchCommandLogs(false, false)" :loading="commandLogsLoading && !isAutoRefreshing">
                        查询
                      </a-button>
                      <a-switch
                        v-model:checked="autoRefreshEnabled"
                        checked-children="自动刷新"
                        un-checked-children="手动刷新"
                        @change="(checked) => { if (checked) startAutoRefresh(); else stopAutoRefresh() }"
                      />
                    </a-space>
                    <a-space wrap :size="[12, 12]" style="width: 100%;">
                      <a-button
                        type="primary"
                        :disabled="selectedLogIdsCount < 1 || batchExecuting"
                        :loading="batchExecuting"
                        @click="executeBatchCommands"
                      >
                        <template #icon>
                          <PlaySquareOutlined />
                        </template>
                        批量执行 ({{ selectedLogIdsCount }})
                      </a-button>
                      <a-button
                        type="primary"
                        danger
                        :disabled="selectedLogIdsCount < 1 || deletingLogs"
                        :loading="deletingLogs"
                        @click="deleteBatchCommandLogs"
                      >
                        <template #icon>
                          <DeleteOutlined />
                        </template>
                        批量删除 ({{ selectedLogIdsCount }})
                      </a-button>
                      <a-button
                        type="default"
                        :disabled="selectedLogIdsCount < 1"
                        @click="exportSelectedCommandLogs"
                      >
                        <template #icon>
                          <DownloadOutlined />
                        </template>
                        导出选中 ({{ selectedLogIdsCount }})
                      </a-button>
                      <a-button
                        type="default"
                        danger
                        :disabled="!agent?.mapping_id || deletingLogs"
                        :loading="deletingLogs"
                        @click="clearAllCommandLogs"
                      >
                        <template #icon>
                          <ClearOutlined />
                        </template>
                        清空所有日志
                      </a-button>
                    </a-space>
                  </div>
                  <div style="overflow-x: auto; margin-top: 8px;">
                    <a-table
                      :data-source="commandLogs"
                      :loading="commandLogsLoading && !isAutoRefreshing"
                      :pagination="{
                        current: commandLogsPagination.current,
                        pageSize: commandLogsPagination.pageSize,
                        total: commandLogsTotal,
                        showSizeChanger: true,
                        showTotal: (total) => `共 ${total} 条`,
                      }"
                      row-key="id"
                      :row-selection="rowSelection"
                      @change="handleCommandLogsTableChange"
                      :scroll="{ x: 'max-content' }"
                      :row-class-name="getRowClassName"
                      :columns="[
                        { title: '时间', dataIndex: 'time', key: 'time', width: 180 },
                        { title: 'Sandbox', dataIndex: 'to_id', key: 'to_id', width: 150, ellipsis: true },
                        { title: '命令', dataIndex: 'adb_command', key: 'adb_command', ellipsis: true },
                        { title: '操作', key: 'action', width: 120, fixed: 'right' },
                      ]"
                    >
                    <template #bodyCell="{ column, record }">
                      <template v-if="column.key === 'time'">
                        {{ new Date(record.time).toLocaleString() }}
                      </template>
                      <template v-else-if="column.key === 'to_id'">
                        <a-tag v-if="getSandboxName(record) !== '-'" color="blue">{{ getSandboxName(record) }}</a-tag>
                        <span v-else style="color: #999;">-</span>
                      </template>
                      <template v-else-if="column.key === 'adb_command'">
                        <!-- 特殊渲染 Agent 执行开始标记 -->
                        <div v-if="isAgentExecutionStart(record.adb_command)" class="agent-execution-start">
                          <a-tag color="blue" style="margin-right: 8px;">🚀 Agent 执行开始</a-tag>
                          <span v-if="record.adb_command.includes('变量:')" class="execution-start-vars">
                            {{ extractVariables(record.adb_command) }}
                          </span>
                        </div>
                        <code v-else style="font-size: 12px;">{{ record.adb_command }}</code>
                      </template>
                      <template v-else-if="column.key === 'action'">
                        <!-- 执行开始标记行不显示操作按钮 -->
                        <template v-if="isAgentExecutionStart(record.adb_command)">
                          <span style="color: #999; font-size: 12px;">-</span>
                        </template>
                        <template v-else>
                          <a-space>
                            <a-button
                              type="link"
                              size="small"
                              :loading="executingCommandId === record.id"
                              :disabled="!!executingCommandId || !agent?.sandbox_id"
                              @click="executeCommand(record)"
                            >
                              <template #icon>
                                <PlayCircleOutlined />
                              </template>
                              执行
                            </a-button>
                            <a-button
                              type="link"
                              size="small"
                              danger
                              :loading="deletingLogs"
                              :disabled="deletingLogs"
                              @click="deleteCommandLog(record)"
                            >
                              <template #icon>
                                <DeleteOutlined />
                              </template>
                              删除
                            </a-button>
                          </a-space>
                        </template>
                      </template>
                    </template>
                    </a-table>
                  </div>
                </template>
              </a-card>
            </a-tab-pane>
          </a-tabs>
        </a-space>
      </a-col>

      <a-col :xs="24" :lg="8">
        <a-space direction="vertical" :size="24" style="width: 100%">
          <a-card title="Sandbox 切换" bordered>
            <a-space direction="vertical" :size="12" style="width: 100%">
              <a-select
                style="width: 100%"
                placeholder="选择 Sandbox"
                :options="sandboxes.map((s) => ({ label: s.id, value: s.id }))"
                :value="agent?.sandbox_id"
                @change="handleSwitchSandbox"
              />
              <a-space style="width: 100%; justify-content: space-between;">
                <a-button
                  type="primary"
                  :loading="sandboxOperating"
                  :disabled="!agent?.sandbox_id || sandboxOperating"
                  @click="handleStartSandbox"
                >
                  <template #icon>
                    <PlayCircleOutlined />
                  </template>
                  启动
                </a-button>
                <a-button
                  danger
                  :loading="sandboxOperating"
                  :disabled="!agent?.sandbox_id || sandboxOperating"
                  @click="handleStopSandbox"
                >
                  <template #icon>
                    <PoweroffOutlined />
                  </template>
                  停止
                </a-button>
                <a-button
                  :loading="sandboxOperating"
                  :disabled="!agent?.sandbox_id || sandboxOperating"
                  @click="handleRestartSandbox"
                >
                  <template #icon>
                    <ReloadOutlined />
                  </template>
                  重启
                </a-button>
              </a-space>
            </a-space>
          </a-card>

          <ScrcpyPlayer
            :sandbox-id="agent?.sandbox_id"
            :status="scrcpyStatus"
            @start="startScrcpySession"
            @stop="stopScrcpySession"
            @update:status="(s) => scrcpyStatus = s"
            @adb="handleScrcpyAdb"
          />
        </a-space>
      </a-col>
    </a-row>
  </div>
</template>

<style scoped lang="less">
@import "../../styles/variables.less";

.detail-page {
  padding: @space-xl 0 @space-xxl;
  display: flex;
  flex-direction: column;
  gap: @space-lg;
}

.page-title h1 {
  margin: 0 0 @space-sm;
  font-size: @font-size-h1;
}

.title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: @space-md;
}

.detail-tabs {
  width: 100%;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
}

// APK 应用商店样式
.apk-store-container {
  width: 100%;
}

.apk-store-header {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 16px;
}

.apk-store-header h2 {
  margin: 0;
  font-size: 20px;
}

.app-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 16px;
  padding: 16px 0;
}

.app-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 16px;
  border: 1px solid #d9d9d9;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.3s;
}

.app-card:hover {
  border-color: #1890ff;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.app-icon {
  width: 64px;
  height: 64px;
  border-radius: 12px;
  overflow: hidden;
  margin-bottom: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #f0f0f0;
}

.app-icon img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.app-icon-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  font-weight: bold;
  color: #1890ff;
  background: #e6f7ff;
}

.app-info {
  text-align: center;
  width: 100%;
}

.app-name {
  font-size: 14px;
  font-weight: 500;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.app-package {
  font-size: 12px;
  color: #8c8c8c;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

// 批量执行中的行高亮（当前正在执行）
:deep(.ant-table-tbody tr.batch-executing-row) {
  background-color: #e6f7ff !important;
  border-left: 3px solid #1890ff !important;
}

:deep(.ant-table-tbody tr.batch-executing-row td) {
  background-color: #e6f7ff !important;
  animation: pulse-highlight 1.5s ease-in-out infinite !important;
}

// 已批量执行的行高亮（已完成）
:deep(.ant-table-tbody tr.batch-executed-row) {
  background-color: #f6ffed !important;
  border-left: 3px solid #52c41a !important;
}

:deep(.ant-table-tbody tr.batch-executed-row td) {
  background-color: #f6ffed !important;
}

@keyframes pulse-highlight {
  0%, 100% {
    background-color: #e6f7ff !important;
  }
  50% {
    background-color: #bae7ff !important;
  }
}

// Agent 执行开始标记行的样式
:deep(.ant-table-tbody tr.agent-execution-start-row) {
  background-color: #f0f5ff !important;
  border-left: 3px solid #1890ff !important;
  font-weight: 500;
}

:deep(.ant-table-tbody tr.agent-execution-start-row td) {
  background-color: #f0f5ff !important;
}

.agent-execution-start {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.execution-start-vars {
  font-size: 12px;
  color: #595959;
}

.app-versions {
  font-size: 12px;
  color: #8c8c8c;
}

.text-ellipsis {
  display: inline-block;
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>

