<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { message, Modal } from 'ant-design-vue'
import {
  getSandbox,
  startSandbox,
  stopSandbox,
  deleteSandbox,
  adbExec,
  getScrcpyStatus,
  startScrcpy,
  stopScrcpy,
  installApk,
  getAdbCommandLogs,
  deleteAdbCommandLogs,
  clearAdbCommandLogsByMapping,
} from '../../api/sandboxes'
import { listApks } from '../../api/apks'
import TerminalShell from '../../components/Terminal/TerminalShell.vue'
import ScrcpyPlayer from '../../components/VideoPlayer/ScrcpyPlayer.vue'
import { ArrowLeftOutlined, PlayCircleOutlined, PlaySquareOutlined, DeleteOutlined, ClearOutlined, DownloadOutlined } from '@ant-design/icons-vue'
import dayjs from 'dayjs'

const route = useRoute()
const router = useRouter()
const sandboxId = route.params.id
const loading = ref(false)
const sandbox = ref(null)
const scrcpyStatus = ref('disconnected')
const activeTab = ref('info')

// 操作按钮的 loading 状态
const starting = ref(false)
const stopping = ref(false)
const deleting = ref(false)

// APK 应用商店相关
const apkLoading = ref(false)
const apkDataSource = ref([])
const selectedApkPackage = ref(null)
const installingApkId = ref(null) // 正在安装的 APK ID

const terminalLogs = ref([])
const shellConnected = ref(false)
const shellLoading = ref(false)
const wsRef = ref(null)

// 浏览器原生 WebSocket 不支持自定义 Header，这里用 query token 兜底
const createWebSocketWithHeaders = (url, headers = {}) => {
  if (!headers.Authorization) return new WebSocket(url)
  const token = headers.Authorization.replace(/^Bearer\s+/i, '')
  const sep = url.includes('?') ? '&' : '?'
  return new WebSocket(`${url}${sep}token=${encodeURIComponent(token)}`)
}

const fetchDetail = async () => {
  loading.value = true
  try {
    const { data } = await getSandbox(sandboxId)
    sandbox.value = data
    // 调试：打印已安装的 APK ID 列表
    console.log('Sandbox data:', sandbox.value)
    console.log('Sandbox installed_apk_ids:', sandbox.value?.installed_apk_ids)
    console.log('Sandbox installedApkIds:', sandbox.value?.installedApkIds)
    // 如果 installed_apk_ids 是字符串，尝试解析为数组
    if (sandbox.value && typeof sandbox.value.installed_apk_ids === 'string') {
      try {
        sandbox.value.installed_apk_ids = JSON.parse(sandbox.value.installed_apk_ids)
        console.log('Parsed installed_apk_ids:', sandbox.value.installed_apk_ids)
      } catch (e) {
        console.error('Failed to parse installed_apk_ids:', e)
      }
    }
  } catch (err) {
    const errorMessage = err.message || '加载失败'
    message.error(errorMessage)
    // 如果 sandbox 不存在（404 或相关错误），跳转到列表页
    if (err.response?.status === 404 || errorMessage.includes('不存在') || errorMessage.includes('not found')) {
      setTimeout(() => {
        router.push('/sandboxes')
      }, 1500)
    }
  } finally {
    loading.value = false
  }
}

const connectAdbShell = () => {
  if (!sandboxId) return
  if (wsRef.value) {
    try {
      wsRef.value.close()
    } catch (_) {}
  }

  shellLoading.value = true
  shellConnected.value = false
  terminalLogs.value = ['连接中...']

  const token = localStorage.getItem('sandroidx_token') || ''
  const wsBase = window.location.origin.replace(/^http/, 'ws')
  const url = `${wsBase}/api/v1/sandboxes/shell/${sandboxId}`
  const headers = token ? { Authorization: `Bearer ${token}` } : undefined
  const ws = createWebSocketWithHeaders(url, headers || {})
  wsRef.value = ws

  const appendOutput = (text) => {
    // 按行追加，避免 join('\n') 时把 chunk 内的换行重复叠加
    const lines = (text || '').replace(/\r/g, '').split('\n')
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i]
      if (i === lines.length - 1 && line === '') continue
      terminalLogs.value.push(line)
    }
  }

  ws.onopen = () => {
    shellConnected.value = true
    shellLoading.value = false
    terminalLogs.value = ['已连接，输入命令回车发送（这是 ADB shell 会话）']
  }
  ws.onmessage = (evt) => {
    const text = typeof evt.data === 'string' ? evt.data : ''
    if (text) appendOutput(text)
  }
  ws.onerror = (evt) => {
    shellConnected.value = false
    shellLoading.value = false
    terminalLogs.value.push('连接错误：检查 Sandbox 是否运行 / ADB 映射是否正常 / token 是否有效')
    console.error('ws error', evt)
  }
  ws.onclose = (evt) => {
    shellConnected.value = false
    shellLoading.value = false
    if (evt.code !== 1000) {
      // 非正常关闭（如网络故障），显示重连提示
      terminalLogs.value.push(`连接已断开 (code=${evt.code})，请点击重连按钮重新连接`)
    } else {
      terminalLogs.value.push(`连接已关闭 (code=${evt.code})`)
    }
  }
}

// 断开 ADB Shell 连接
const disconnectAdbShell = () => {
  if (wsRef.value) {
    try {
      wsRef.value.close(1000) // 正常关闭
      wsRef.value = null
    } catch (_) {}
  }
  shellConnected.value = false
  shellLoading.value = false
  terminalLogs.value.push('已断开连接')
}

const sendShell = (cmd) => {
  if (!wsRef.value || wsRef.value.readyState !== WebSocket.OPEN) {
    message.error('ADB Shell 未连接')
    return
  }
  const payload = cmd.endsWith('\n') ? cmd : `${cmd}\n`
  wsRef.value.send(payload)
  terminalLogs.value.push(`$ ${cmd}`)
}

const sendShellRaw = (payload) => {
  if (!wsRef.value || wsRef.value.readyState !== WebSocket.OPEN) {
    message.error('ADB Shell 未连接')
    return
  }
  wsRef.value.send(payload)
  if (payload === '\x03') terminalLogs.value.push('^C')
}

const clearShellLogs = () => {
  terminalLogs.value = []
}

const handleScrcpyAdb = (payload) => {
  const cmd = payload?.cmd
  if (!cmd) return
  enqueueScrcpyExec(cmd)
}

// Scrcpy 交互不再写入右侧 ADB Shell（那只是 debug 用）；这里走 /sandboxes/exec/:id 静默执行
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
  if (!sandboxId) return
  scrcpyExecRunning = true
  try {
    while (scrcpyExecQueue.length) {
      const c = scrcpyExecQueue.shift()
      await adbExec(sandboxId, { command: c })
    }
  } catch (err) {
    // 出错时清空队列，避免连续弹错
    scrcpyExecQueue.length = 0
    message.error(err?.message || 'Scrcpy 交互执行失败')
  } finally {
    scrcpyExecRunning = false
  }
}

const refreshScrcpy = async () => {
  if (!sandboxId) return
  try {
    const { data } = await getScrcpyStatus(sandboxId)
    scrcpyStatus.value = data?.active ? 'playing' : 'disconnected'
  } catch (_) {
    scrcpyStatus.value = 'disconnected'
  }
}

const handleStart = async () => {
  if (starting.value) return
  
  starting.value = true
  const hideLoading = message.loading('正在启动...', 0)
  
  try {
    await startSandbox(sandboxId)
    hideLoading()
    message.success('已启动')
    fetchDetail()
  } catch (err) {
    hideLoading()
    message.error(err.message || '启动失败')
  } finally {
    starting.value = false
  }
}

const handleStop = async () => {
  if (stopping.value) return
  
  stopping.value = true
  const hideLoading = message.loading('正在停止...', 0)
  
  try {
    await stopSandbox(sandboxId)
    hideLoading()
    message.success('已停止')
    fetchDetail()
  } catch (err) {
    hideLoading()
    message.error(err.message || '停止失败')
  } finally {
    stopping.value = false
  }
}

const handleDelete = async () => {
  if (deleting.value) return
  
  deleting.value = true
  const hideLoading = message.loading('正在删除...', 0)
  
  try {
    await deleteSandbox(sandboxId)
    hideLoading()
    message.success('已删除')
    // 删除成功后跳转到列表页
    router.push('/sandboxes')
  } catch (err) {
    hideLoading()
    message.error(err.message || '删除失败')
  } finally {
    deleting.value = false
  }
}

// 重启 Sandbox（内部方法，不显示确认对话框）
const restartSandboxInternal = async () => {
  if (!sandboxId) {
    throw new Error('Sandbox ID 不存在')
  }
  
  stopping.value = true
  starting.value = true
  const hideLoading = message.loading('正在重启 Sandbox...', 0)
  
  try {
    // 先停止
    await stopSandbox(sandboxId)
    // 等待一下确保完全停止
    await new Promise(resolve => setTimeout(resolve, 1000))
    // 再启动
    await startSandbox(sandboxId)
    hideLoading()
    message.success('Sandbox 重启成功')
    // 刷新详情
    await fetchDetail()
  } catch (err) {
    hideLoading()
    throw err
  } finally {
    stopping.value = false
    starting.value = false
  }
}

const startScrcpySession = async () => {
  if (!sandboxId) return
  try {
    scrcpyStatus.value = 'connecting'
    await startScrcpy(sandboxId)
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
  if (!sandboxId) return
  try {
    await stopScrcpy(sandboxId)
    scrcpyStatus.value = 'disconnected'
  } catch (err) {
    message.error(err.message || '停止失败')
  }
}

// APK 相关方法
const normalizeArray = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.data)) return payload.data
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload?.list)) return payload.list
  return []
}

const groupedApks = computed(() => {
  const groups = {}
  apkDataSource.value.forEach((apk) => {
    const pkg = apk.package_name || '未知包名'
    if (!groups[pkg]) {
      groups[pkg] = {
        package_name: pkg,
        name: apk.name,
        icon: apk.icon,
        versions: [],
      }
    }
    if (apk.icon && !groups[pkg].icon) {
      groups[pkg].icon = apk.icon
    }
    if (apk.name && (!groups[pkg].name || groups[pkg].name === '未知包名')) {
      groups[pkg].name = apk.name
    }
    groups[pkg].versions.push(apk)
  })
  
  Object.values(groups).forEach((group) => {
    group.versions.sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
  })
  
  return Object.values(groups)
})

const currentApkVersions = computed(() => {
  if (!selectedApkPackage.value) return []
  const group = groupedApks.value.find((g) => g.package_name === selectedApkPackage.value)
  return group ? group.versions : []
})

// 判断 APK 是否已安装
const isApkInstalled = (apkId) => {
  if (!apkId) return false
  if (!sandbox.value) return false
  
  // 尝试多种可能的字段名
  let installedIds = sandbox.value.installed_apk_ids || sandbox.value.installedApkIds
  
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
  const result = installedIds.map(String).includes(apkIdStr)
  
  return result
}

// 判断包名是否有任何版本已安装
const hasInstalledVersion = (packageName) => {
  let installedIds = sandbox.value?.installed_apk_ids || sandbox.value?.installedApkIds
  
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

const fetchApks = async () => {
  apkLoading.value = true
  try {
    const { data } = await listApks()
    apkDataSource.value = normalizeArray(data)
  } catch (err) {
    message.error(err.message || '加载 APK 列表失败')
  } finally {
    apkLoading.value = false
  }
}

const handleViewApkPackage = (packageName) => {
  selectedApkPackage.value = packageName
}

const handleBackApkList = () => {
  selectedApkPackage.value = null
}

const handleInstallApk = async (apkRecord) => {
  if (!apkRecord?.id) return
  if (installingApkId.value) return // 如果正在安装，忽略重复点击
  
  installingApkId.value = apkRecord.id
  const hideLoading = message.loading('正在安装 APK...', 0)
  
  try {
    await installApk(sandboxId, apkRecord.id)
    hideLoading()
    message.success('APK 安装成功')
    // 刷新 Sandbox 信息以更新已安装的 APK 列表
    await fetchDetail()
  } catch (err) {
    hideLoading()
    message.error(err.message || '安装失败')
  } finally {
    installingApkId.value = null
  }
}

const handleCreateNewApkVersion = () => {
  if (!selectedApkPackage.value) return
  const currentGroup = groupedApks.value.find((g) => g.package_name === selectedApkPackage.value)
  const packageName = selectedApkPackage.value
  const name = currentGroup?.name || packageName
  
  router.push({
    path: '/apks/create',
    query: {
      package_name: packageName,
      name: name,
    },
  })
}

const getIconUrl = (iconPath) => {
  if (!iconPath) return null
  if (iconPath.startsWith('http://') || iconPath.startsWith('https://')) {
    return iconPath
  }
  const parts = iconPath.split('/')
  const fileName = parts[parts.length - 1]
  if (fileName) {
    return `/api/v1/apks/icon/${encodeURIComponent(fileName)}`
  }
  return null
}

// ADB 命令日志相关
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
const rowSelection = computed(() => ({
  selectedRowKeys: selectedLogIds.value,
  onChange: (keys) => {
    console.log('[DEBUG] rowSelection onChange', {
      keys,
      keysType: typeof keys,
      keysLength: keys?.length,
      selectedLogIdsBefore: selectedLogIds.value
    })
    // 保证类型一致：全部转成字符串（与 row-key="id" 对齐）
    selectedLogIds.value = (keys || []).map(String)
    console.log('[DEBUG] rowSelection onChange after', {
      selectedLogIdsAfter: selectedLogIds.value,
      selectedLogIdsAfterLength: selectedLogIds.value?.length
    })
  },
  preserveSelectedRowKeys: true, // 分页/刷新后保留勾选
}))
const autoRefreshEnabled = ref(false)
const autoRefreshInterval = ref(null)
const isAutoRefreshing = ref(false) // 区分自动刷新和手动查询

// 查询命令日志
const fetchCommandLogs = async (silent = false, isAutoRefresh = false) => {
  if (!sandboxId) {
    if (!silent) message.warning('Sandbox ID 不存在')
    return
  }

  // 只有手动查询时才显示 loading，自动刷新时不显示（避免白屏）
  if (!isAutoRefresh) {
    commandLogsLoading.value = true
  } else {
    isAutoRefreshing.value = true
  }
  
  try {
    // 使用 sandbox 的 AgentUserMappingID（用于记录 agent 和用户在 scrcpy player 上的操作）
    const agentUserMappingId = sandbox.value?.agent_user_mapping_id
    
    if (!agentUserMappingId) {
      if (!silent) message.warning('该 Sandbox 没有关联的 Agent/User 映射，可能尚未创建或启动')
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

    const response = await getAdbCommandLogs(agentUserMappingId, params)
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
    
    // 过滤掉指定的命令
    const filteredLogs = allLogs.filter(log => !shouldFilterCommand(log.adb_command))
    
    commandLogs.value = filteredLogs.map(log => ({
      ...log,
      id: String(log.id || log.ID) // 确保 id 是字符串
    }))
    
    // 更新总数（过滤后的数量）
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
    if (activeTab.value === 'command-logs' && sandbox.value?.agent_user_mapping_id) {
      // 自动刷新时，如果当前在第一页，保持第一页以查看最新数据
      // 如果不在第一页，保持当前页
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
  if (!log?.adb_command) return
  
  executingCommandId.value = log.id
  try {
    // 确保命令格式正确（如果命令不包含 "shell " 前缀，则添加）
    let cmd = log.adb_command.trim()
    if (!cmd.startsWith('shell ') && !cmd.startsWith('adb ')) {
      cmd = `shell ${cmd}`
    }
    
    await adbExec(sandboxId, { command: cmd })
    message.success('命令执行成功')
  } catch (err) {
    message.error(err.message || '命令执行失败')
  } finally {
    executingCommandId.value = null
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

// 批量执行命令（从某条到某条）
const executeBatchCommands = async () => {
  console.log('[DEBUG] executeBatchCommands called', {
    selectedLogIdsLength: selectedLogIds.value.length,
    selectedLogIds: selectedLogIds.value,
    sandboxId: sandboxId,
    commandLogsLength: commandLogs.value.length
  })
  
  if (selectedLogIds.value.length < 1) {
    console.log('[DEBUG] No selected logs')
    message.warning('请至少选择一条命令')
    return
  }

  // 根据时间排序，确保按顺序执行
  const selectedLogs = commandLogs.value
    .filter(log => selectedLogIds.value.includes(String(log.id)))
    .sort((a, b) => new Date(a.time) - new Date(b.time))

  console.log('[DEBUG] After filter', {
    selectedLogsLength: selectedLogs.length,
    selectedLogs: selectedLogs.map(l => ({ id: l.id, adb_command: l.adb_command?.substring(0, 50) }))
  })

  if (selectedLogs.length < 1) {
    console.log('[DEBUG] No filtered logs')
    message.warning('请选择至少一条命令')
    return
  }

  batchExecuting.value = true
  currentBatchExecutingId.value = null
  batchExecutedIds.value.clear()
  const hideLoading = message.loading('正在批量执行命令...', 0)

  try {
    console.log('[DEBUG] Starting batch execution', { selectedLogsLength: selectedLogs.length, sandboxId })
    
    let successCount = 0
    let failCount = 0
    
    for (const log of selectedLogs) {
      console.log('[DEBUG] Processing log', {
        logId: log.id,
        hasAdbCommand: !!log?.adb_command,
        adbCommand: log?.adb_command?.substring(0, 50),
        sandboxId: sandboxId
      })
      
      if (!log?.adb_command) {
        console.log('[DEBUG] Skipping log without adb_command')
        continue
      }
      
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
      
      console.log('[DEBUG] Before adbExec', { sandboxId, cmd })
      
      try {
        // 执行命令 - 严格等待前一个命令完全执行完成（包括响应返回）后再执行下一个
        // await 会等待 HTTP 响应完全返回，确保前一个命令执行完成
        const response = await adbExec(sandboxId, { command: cmd })
        console.log('[DEBUG] adbExec success', { sandboxId, cmd })
        
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
        
        console.error('[DEBUG] adbExec error', { sandboxId, cmd, error: cmdErr })
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
    console.log('[DEBUG] Batch execution completed successfully')
  } catch (err) {
    console.error('[DEBUG] executeBatchCommands error', {
      error: err.message,
      errorStack: err.stack,
      selectedLogsLength: selectedLogs.length
    })
    hideLoading()
    message.error(err.message || '批量执行失败')
    currentBatchExecutingId.value = null
    batchExecutedIds.value.clear()
  } finally {
    batchExecuting.value = false
  }
}

// 获取行的类名（用于高亮显示执行状态）
const getRowClassName = (record) => {
  try {
    if (!record || !record.id) return ''
    const id = String(record.id)
    
    // 当前正在执行的命令
    if (currentBatchExecutingId.value === id) {
      return 'batch-executing-row'
    }
    
    // 已执行的命令
    if (batchExecutedIds.value && batchExecutedIds.value instanceof Set && batchExecutedIds.value.has(id)) {
      return 'batch-executed-row'
    }
    
    return ''
  } catch (e) {
    console.error('getRowClassName error:', e, record)
    return ''
  }
}

// 清空当前映射的所有命令日志
const clearAllCommandLogs = async () => {
  if (!sandbox.value?.agent_user_mapping_id) {
    message.warning('当前 Sandbox 没有关联的映射，无法清空日志')
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
        await clearAdbCommandLogsByMapping(sandbox.value.agent_user_mapping_id)
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
    sandboxId: sandbox.value?.id || '',
    sandboxName: sandbox.value?.name || '',
    mappingId: sandbox.value?.agent_user_mapping_id || '',
    totalCount: sortedLogs.length,
    logs: sortedLogs.map(log => ({
      id: log.id,
      time: log.time,
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

// 处理分页变化
const handleCommandLogsTableChange = (pagination) => {
  commandLogsPagination.value = {
    current: pagination.current,
    pageSize: pagination.pageSize,
  }
  fetchCommandLogs(false, false)
}


onMounted(async () => {
  await fetchDetail()
  await refreshScrcpy()
  connectAdbShell()
})

onBeforeUnmount(() => {
  if (wsRef.value) {
    try {
      wsRef.value.close()
    } catch (_) {}
  }
  // 清理自动刷新定时器
  stopAutoRefresh()
})
</script>

<template>
  <div class="page-container detail-page">
    <div class="page-title">
      <h1>Sandbox 详情</h1>
      <p class="muted">{{ sandbox?.id || sandboxId }}</p>
    </div>

    <a-row :gutter="[24, 24]">
      <a-col :xs="24" :lg="16">
        <a-tabs v-model:activeKey="activeTab" @change="(key) => { 
          if (key === 'apks') fetchApks()
          if (key === 'command-logs') {
            fetchCommandLogs(false, false)
            startAutoRefresh()
          } else {
            stopAutoRefresh()
          }
        }">
          <a-tab-pane key="info" tab="基础信息">
            <a-space direction="vertical" :size="24" style="width: 100%">
              <a-card title="基础信息" :loading="loading" bordered>
                <a-descriptions bordered :column="1" size="small">
                  <a-descriptions-item label="类型">{{ sandbox?.type }}</a-descriptions-item>
                  <a-descriptions-item label="镜像">{{ sandbox?.image }}</a-descriptions-item>
                  <a-descriptions-item label="状态">{{ sandbox?.status }}</a-descriptions-item>
                  <a-descriptions-item label="端口">{{ (sandbox?.ports || []).join(', ') }}</a-descriptions-item>
                </a-descriptions>
                <div class="actions">
                  <a-space>
                    <a-button type="primary" @click="handleStart" :loading="starting" :disabled="starting || stopping || deleting">
                      启动
                    </a-button>
                    <a-button @click="handleStop" :loading="stopping" :disabled="starting || stopping || deleting">
                      停止
                    </a-button>
                    <a-button danger type="text" @click="handleDelete" :loading="deleting" :disabled="starting || stopping || deleting">
                      删除
                    </a-button>
                  </a-space>
                </div>
              </a-card>

              <div>
                <TerminalShell
                  title="ADB Shell"
                  :logs="terminalLogs"
                  :loading="shellLoading"
                  :connected="shellConnected"
                  placeholder="例如：getprop ro.build.version.release / ls /"
                  @send="sendShell"
                  @raw="sendShellRaw"
                  @clear="clearShellLogs"
                />
                <div style="margin-top: 8px; display: flex; gap: 8px; justify-content: flex-end;">
                  <a-button
                    size="small"
                    :disabled="shellLoading || !shellConnected"
                    @click="disconnectAdbShell"
                  >
                    断开连接
                  </a-button>
                  <a-button
                    type="primary"
                    size="small"
                    :loading="shellLoading"
                    :disabled="shellConnected && !shellLoading"
                    @click="connectAdbShell"
                  >
                    重连
                  </a-button>
                </div>
              </div>
            </a-space>
          </a-tab-pane>

          <a-tab-pane key="apks" tab="APK 应用商店">
        <div class="apk-store-container">
          <div v-if="!selectedApkPackage" class="apk-store-header">
            <h2>APK 应用商店</h2>
            <p class="muted">选择应用并安装到当前 Sandbox</p>
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
                      :disabled="!!installingApkId"
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
                <span>ADB 命令日志</span>
              </template>
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
                    :disabled="!sandbox?.agent_user_mapping_id || deletingLogs"
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
                :row-class-name="getRowClassName"
                @change="handleCommandLogsTableChange"
                  :columns="[
                    { title: '时间', dataIndex: 'time', key: 'time', width: 180 },
                    { title: '命令', dataIndex: 'adb_command', key: 'adb_command', ellipsis: true },
                    { title: '连接ID', dataIndex: 'connection_id', key: 'connection_id', width: 200, ellipsis: true },
                    { title: '操作', key: 'action', width: 120 },
                  ]"
              >
                  <template #bodyCell="{ column, record }">
                    <template v-if="column.key === 'time'">
                      {{ new Date(record.time).toLocaleString() }}
                    </template>
                    <template v-else-if="column.key === 'adb_command'">
                      <code style="font-size: 12px;">{{ record.adb_command }}</code>
                    </template>
                    <template v-else-if="column.key === 'connection_id'">
                      <span style="font-size: 12px; color: #666;">{{ record.connection_id }}</span>
                    </template>
                  <template v-else-if="column.key === 'action'">
                    <a-space>
                      <a-button
                        type="link"
                        size="small"
                        :loading="executingCommandId === record.id"
                        :disabled="!!executingCommandId"
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
              </a-table>
            </a-card>
          </a-tab-pane>
        </a-tabs>
      </a-col>

      <a-col :xs="24" :lg="8">
        <ScrcpyPlayer
          :sandbox-id="sandboxId"
          :status="scrcpyStatus"
          @start="startScrcpySession"
          @stop="stopScrcpySession"
          @update:status="(s) => (scrcpyStatus = s)"
          @adb="handleScrcpyAdb"
        />
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

.actions {
  margin-top: @space-md;
}

.apk-store-container {
  padding: @space-md 0;
}

.apk-store-header {
  margin-bottom: @space-md;
  display: flex;
  align-items: center;
  gap: @space-md;
}

.apk-store-header h2 {
  margin: 0;
  font-size: 20px;
}

.text-ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 300px;
  display: inline-block;
}

.mono {
  font-family: monospace;
}

.app-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: @space-md;
  padding: @space-md 0;
}

.app-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: @space-md;
  border: 1px solid #d9d9d9;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.3s;
  
  &:hover {
    border-color: #1890ff;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  }
}

.app-icon {
  width: 64px;
  height: 64px;
  margin-bottom: @space-sm;
  display: flex;
  align-items: center;
  justify-content: center;
  
  img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
}

.app-icon-placeholder {
  width: 64px;
  height: 64px;
  border-radius: 12px;
  background-color: #1890ff;
  color: white;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  font-weight: bold;
}

.app-info {
  text-align: center;
  width: 100%;
}

.app-name {
  font-weight: 500;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.app-package {
  font-size: 12px;
  color: #999;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.app-versions {
  font-size: 12px;
  color: #666;
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
    opacity: 1;
  }
  50% {
    opacity: 0.8;
  }
}
</style>

