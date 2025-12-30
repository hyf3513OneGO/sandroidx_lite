<script setup>
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'

const props = defineProps({
  sandboxId: { type: String, default: '' },
  // shareToken 存在时走 /share/agents/:token/* 的公开只读接口（无需登录）
  shareToken: { type: String, default: '' },
  title: { type: String, default: 'Scrcpy 视频流' },
  status: { type: String, default: 'disconnected' },
  // readonly: 禁用所有交互（触控/键盘/输入条/启动按钮），仅允许观看视频
  readonly: { type: Boolean, default: false },
})

const emit = defineEmits(['start', 'stop', 'update:status', 'tap', 'adb'])

const canvas = ref(null)
const playerWrapper = ref(null)
const connected = ref(false)
const overlayState = ref('待连接')
const overlaySize = ref('-')
const overlayFps = ref('0')

let ctx = null
let ws = null
let decoder = null
let configured = false
let sps = null
let pps = null
let pending = []
let timestamp = 0
let frameCount = 0
let lastFpsAt = performance.now()
let auParts = []
let injectedConfig = false
let nativeResolution = null
let streamResolution = null // 视频流分辨率（max_size 后的实际编码尺寸）
let connectTimer = 0

const clamp = (v, min, max) => Math.min(max, Math.max(min, v))

const clearCanvas = () => {
  try {
    if (!canvas.value) return
    if (!ctx) ctx = canvas.value.getContext('2d')
    if (!ctx) return
    // 清空画面（断开时不要停留最后一帧）
    ctx.clearRect(0, 0, canvas.value.width || 0, canvas.value.height || 0)
  } catch (_) {}
}

const buildTapCommand = (x, y) => `input tap ${x} ${y}`
const buildSwipeCommand = (x1, y1, x2, y2, durationMs) => {
  const d = Math.max(1, Math.round(durationMs || 200))
  return `input swipe ${x1} ${y1} ${x2} ${y2} ${d}`
}

const shellQuoteSingle = (s) => {
  // 'foo'"'"'bar' 这种写法在 /system/bin/sh 可用
  return `'${String(s).replace(/'/g, `'\"'\"'`)}'`
}

const encodeInputText = (s) => {
  // Android input text: 用 %s 表示空格；其它字符尽量原样保留
  // 注意：中文/IME 不保证可靠（input text 本身限制）
  return String(s).replace(/ /g, '%s')
}

const buildTextCommand = (text) => `input text ${shellQuoteSingle(encodeInputText(text))}`
const buildKeyeventCommand = (key) => `input keyevent ${key}`

const mapClientToStream = (clientX, clientY) => {
  if (!connected.value) return
  if (!canvas.value) return

  const rect = canvas.value.getBoundingClientRect()
  if (!rect.width || !rect.height) return null

  const px = clientX - rect.left
  const py = clientY - rect.top
  if (px < 0 || py < 0 || px > rect.width || py > rect.height) return null

  const streamW = canvas.value.width || streamResolution?.width
  const streamH = canvas.value.height || streamResolution?.height
  if (!streamW || !streamH) return null

  const intrinsicAR = streamW / streamH
  const containerAR = rect.width / rect.height
  let drawW = rect.width
  let drawH = rect.height
  let offX = 0
  let offY = 0
  if (containerAR > intrinsicAR) {
    // 左右黑边（pillarbox）
    drawH = rect.height
    drawW = rect.height * intrinsicAR
    offX = (rect.width - drawW) / 2
  } else if (containerAR < intrinsicAR) {
    // 上下黑边（letterbox）
    drawW = rect.width
    drawH = rect.width / intrinsicAR
    offY = (rect.height - drawH) / 2
  }

  const pxIn = px - offX
  const pyIn = py - offY
  // 点击在黑边区域则忽略
  if (pxIn < 0 || pyIn < 0 || pxIn > drawW || pyIn > drawH) return null

  const xStream = Math.round((pxIn * streamW) / drawW)
  const yStream = Math.round((pyIn * streamH) / drawH)
  return { xStream, yStream, streamW, streamH }
}

const streamToNative = (xStream, yStream, streamW, streamH) => {
  let x = xStream
  let y = yStream
  const nativeW = nativeResolution?.width
  const nativeH = nativeResolution?.height

  if (nativeW && nativeH && streamW && streamH) {
    // 处理少数情况下的横竖屏旋转（native 与 stream 宽高互换）
    if (nativeW === streamH && nativeH === streamW) {
      // 90° 旋转：stream(x,y) -> native(y, nativeH - x)
      x = Math.round((yStream * nativeW) / streamH)
      y = Math.round(((streamW - 1 - xStream) * nativeH) / streamW)
    } else {
      x = Math.round((xStream * nativeW) / streamW)
      y = Math.round((yStream * nativeH) / streamH)
    }
  }

  const finalW = nativeW || streamW
  const finalH = nativeH || streamH
  x = clamp(x, 0, Math.max(0, finalW - 1))
  y = clamp(y, 0, Math.max(0, finalH - 1))

  return { x, y, nativeW, nativeH }
}

const emitAdb = (payload) => {
  emit('adb', payload)
}

// --- 光标/触控指示器（圆形）---
const cursorIndicator = ref({
  visible: false,
  active: false,
  x: 0,
  y: 0,
})

let cursorRaf = 0
let lastCursorEvt = null
const updateCursorFromEvent = (evt) => {
  if (!canvas.value) return
  const rect = canvas.value.getBoundingClientRect()
  if (!rect.width || !rect.height) return
  const px = evt.clientX - rect.left
  const py = evt.clientY - rect.top
  cursorIndicator.value.x = clamp(px, 0, rect.width)
  cursorIndicator.value.y = clamp(py, 0, rect.height)
}

const scheduleCursorUpdate = (evt) => {
  lastCursorEvt = evt
  if (cursorRaf) return
  cursorRaf = requestAnimationFrame(() => {
    cursorRaf = 0
    if (lastCursorEvt) updateCursorFromEvent(lastCursorEvt)
  })
}

const handlePointerEnter = (evt) => {
  if (props.readonly) return
  if (!connected.value) return
  cursorIndicator.value.visible = true
  cursorIndicator.value.active = false
  scheduleCursorUpdate(evt)
}

const handlePointerLeave = () => {
  if (props.readonly) return
  cursorIndicator.value.visible = false
  cursorIndicator.value.active = false
}

// --- 手势：tap / swipe ---
const gesture = ref({
  active: false,
  pointerId: null,
  startClientX: 0,
  startClientY: 0,
  lastClientX: 0,
  lastClientY: 0,
  startAt: 0,
})

const handlePointerDown = (evt) => {
  if (props.readonly) return
  if (!connected.value) return
  if (!canvas.value) return
  try {
    canvas.value.focus?.()
    canvas.value.setPointerCapture?.(evt.pointerId)
  } catch (_) {}

  gesture.value = {
    active: true,
    pointerId: evt.pointerId,
    startClientX: evt.clientX,
    startClientY: evt.clientY,
    lastClientX: evt.clientX,
    lastClientY: evt.clientY,
    startAt: performance.now(),
  }

  // 按下时让圆形指示器更明显
  cursorIndicator.value.visible = true
  cursorIndicator.value.active = true
  scheduleCursorUpdate(evt)
}

const handlePointerMove = (evt) => {
  if (props.readonly) return
  if (!connected.value) return
  cursorIndicator.value.visible = true
  scheduleCursorUpdate(evt)

  // 如果正在手势中，同时更新手势轨迹
  if (!gesture.value.active) return
  if (gesture.value.pointerId !== evt.pointerId) return
  gesture.value.lastClientX = evt.clientX
  gesture.value.lastClientY = evt.clientY
}

const finishGesture = (evt) => {
  if (props.readonly) return
  if (!gesture.value.active) return
  if (gesture.value.pointerId !== evt.pointerId) return

  const start = mapClientToStream(gesture.value.startClientX, gesture.value.startClientY)
  const end = mapClientToStream(gesture.value.lastClientX, gesture.value.lastClientY)

  gesture.value.active = false
  gesture.value.pointerId = null
  cursorIndicator.value.active = false

  if (!start || !end) return

  const dt = Math.max(0, performance.now() - gesture.value.startAt)
  const dx = end.xStream - start.xStream
  const dy = end.yStream - start.yStream
  const dist2 = dx * dx + dy * dy

  // 小位移视为 tap；阈值按 stream 像素
  const TAP_THRESHOLD2 = 12 * 12
  if (dist2 <= TAP_THRESHOLD2) {
    const n = streamToNative(start.xStream, start.yStream, start.streamW, start.streamH)
    const cmd = buildTapCommand(n.x, n.y)
    const payload = {
      cmd,
      kind: 'tap',
      x: n.x,
      y: n.y,
      stream: { x: start.xStream, y: start.yStream, width: start.streamW, height: start.streamH },
      native: n.nativeW && n.nativeH ? { width: n.nativeW, height: n.nativeH } : null,
    }
    emit('tap', payload)
    emitAdb(payload)
    return
  }

  const n1 = streamToNative(start.xStream, start.yStream, start.streamW, start.streamH)
  const n2 = streamToNative(end.xStream, end.yStream, end.streamW, end.streamH)
  // duration 做个合理区间，避免超长/过短
  const duration = clamp(dt, 60, 1500)
  const cmd = buildSwipeCommand(n1.x, n1.y, n2.x, n2.y, duration)
  emitAdb({
    cmd,
    kind: 'swipe',
    from: { x: n1.x, y: n1.y },
    to: { x: n2.x, y: n2.y },
    duration_ms: Math.round(duration),
    native: n1.nativeW && n1.nativeH ? { width: n1.nativeW, height: n1.nativeH } : null,
  })
}

const handlePointerUp = (evt) => finishGesture(evt)
const handlePointerCancel = (evt) => finishGesture(evt)

// --- 键盘输入（简化版）---
const handleCanvasKeydown = (evt) => {
  if (props.readonly) return
  if (!connected.value) return
  if (!evt) return
  if (evt.ctrlKey || evt.metaKey || evt.altKey) return
  if (evt.repeat) return

  // 常用按键映射
  if (evt.key === 'Enter') {
    evt.preventDefault()
    emitAdb({ cmd: buildKeyeventCommand('ENTER'), kind: 'keyevent', key: 'ENTER' })
    return
  }
  if (evt.key === 'Backspace') {
    evt.preventDefault()
    emitAdb({ cmd: buildKeyeventCommand('DEL'), kind: 'keyevent', key: 'DEL' })
    return
  }
  if (evt.key === 'Escape') {
    evt.preventDefault()
    emitAdb({ cmd: buildKeyeventCommand('BACK'), kind: 'keyevent', key: 'BACK' })
    return
  }

  // 可打印字符：逐字符发送（简单可靠；不支持 IME 组合输入）
  if (typeof evt.key === 'string' && evt.key.length === 1) {
    evt.preventDefault()
    emitAdb({ cmd: buildTextCommand(evt.key), kind: 'text', text: evt.key })
  }
}

// 额外：提供一个显式“发送文本”输入框（用于粘贴/一次性发送）
const textDraft = ref('')
const sendTextDraft = () => {
  const t = (textDraft.value || '').trimEnd()
  if (!t) return
  emitAdb({ cmd: buildTextCommand(t), kind: 'text', text: t })
  textDraft.value = ''
}
const sendEnter = () => emitAdb({ cmd: buildKeyeventCommand('ENTER'), kind: 'keyevent', key: 'ENTER' })
const sendBackspace = () => emitAdb({ cmd: buildKeyeventCommand('DEL'), kind: 'keyevent', key: 'DEL' })

const log = (msg) => {
  console.log(`[Scrcpy] ${msg}`)
}

const stripStartCode = (nal) => {
  let offset = 0
  if (nal[0] === 0 && nal[1] === 0) {
    if (nal[2] === 1) offset = 3
    else if (nal[2] === 0 && nal[3] === 1) offset = 4
  }
  return nal.slice(offset)
}

const buildAvcC = (spsData, ppsData) => {
  const spsLen = spsData.length
  const ppsLen = ppsData.length
  const avcC = new Uint8Array(11 + spsLen + ppsLen)
  let i = 0
  avcC[i++] = 1
  avcC[i++] = spsData[1]
  avcC[i++] = spsData[2]
  avcC[i++] = spsData[3]
  avcC[i++] = 0xff
  avcC[i++] = 0xe1
  avcC[i++] = (spsLen >> 8) & 0xff
  avcC[i++] = spsLen & 0xff
  avcC.set(spsData, i)
  i += spsLen
  avcC[i++] = 1
  avcC[i++] = (ppsLen >> 8) & 0xff
  avcC[i++] = ppsLen & 0xff
  avcC.set(ppsData, i)
  return avcC.buffer
}

const hex2 = (v) => v.toString(16).padStart(2, '0')

const applyLayoutResolution = (width, height) => {
  if (!width || !height) return
  if (!playerWrapper.value) return

  const availableWidth = Math.max(320, window.innerWidth - 120)
  const availableHeight = Math.max(240, window.innerHeight - 400)
  const scale = Math.min(1, Math.min(availableWidth / width, availableHeight / height))
  const targetWidth = Math.round(width * scale)
  const targetHeight = Math.round(height * scale)

  playerWrapper.value.style.width = `${targetWidth}px`
  playerWrapper.value.style.height = `${targetHeight}px`
  playerWrapper.value.style.maxWidth = '100%'
  playerWrapper.value.style.aspectRatio = `${width} / ${height}`
}

const applyStreamResolution = (width, height) => {
  if (!width || !height) return
  streamResolution = { width, height }
  // 画面渲染必须用视频流尺寸
  if (canvas.value && (canvas.value.width !== width || canvas.value.height !== height)) {
    canvas.value.width = width
    canvas.value.height = height
  }
  // 布局优先使用真实分辨率（更符合设备比例），否则用流分辨率
  const layout = nativeResolution || streamResolution
  if (layout) applyLayoutResolution(layout.width, layout.height)

  const labelNative = nativeResolution ? `${nativeResolution.width} x ${nativeResolution.height}` : '-'
  const labelStream = streamResolution ? `${streamResolution.width} x ${streamResolution.height}` : '-'
  overlaySize.value = nativeResolution ? `${labelNative} (流: ${labelStream})` : labelStream
}

const buildHttpBase = () => {
  // 与 connectWebSocket 一致：默认 /api/v1；支持完整 http(s) base；支持相对 base
  const apiBase = import.meta.env.VITE_API_BASE || '/api/v1'
  if (apiBase.startsWith('http')) return apiBase
  if (apiBase.startsWith('/')) return window.location.origin + apiBase
  // 兜底：相对路径
  return window.location.origin + '/' + apiBase.replace(/^\/+/, '')
}

// 预留：输入通道/坐标映射等能力。当前版本仅提供视频流，保持为 no-op，避免连接流程报错。
const fetchInputResolution = async () => {
  return null
}

const fetchDeviceResolution = async () => {
  if (!props.sandboxId && !props.shareToken) throw new Error('请选择 sandbox')
  const httpBase = buildHttpBase()
  const token = localStorage.getItem('sandroidx_token')
  const sep = httpBase.includes('?') ? '&' : '?'
  // shareToken 优先：走公开分享接口
  const url = props.shareToken
    ? `${httpBase}/share/agents/${encodeURIComponent(props.shareToken)}/scrcpy/resolution`
    : `${httpBase}/sandboxes/scrcpy/${props.sandboxId}/resolution${token ? `${sep}token=${encodeURIComponent(token)}` : ''}`
  log(`GET ${url}`)
  const resp = await fetch(url, {
    headers: props.shareToken ? {} : token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (!resp.ok) throw new Error(`获取分辨率失败，HTTP ${resp.status}`)
  const data = await resp.json()
  if (!data.width || !data.height) {
    throw new Error('未获取到有效的分辨率数据')
  }
  nativeResolution = { width: data.width, height: data.height }
  // 先按真实尺寸做布局；canvas 实际会在第一帧到来后切到 stream 尺寸
  applyLayoutResolution(nativeResolution.width, nativeResolution.height)
  overlaySize.value = `${nativeResolution.width} x ${nativeResolution.height}`
  return data
}

const ensureDecoderConfigured = () => {
  if (configured || !sps || !pps) return
  if (!('VideoDecoder' in window)) {
    log('当前浏览器不支持 WebCodecs')
    return
  }
  const codec = `avc1.${hex2(sps[1])}${hex2(sps[2])}${hex2(sps[3])}`
  const description = buildAvcC(sps, pps)
  decoder = new VideoDecoder({
    output: renderFrame,
    error: (e) => log(`解码错误: ${e.message}`),
  })
  decoder.configure({ codec, description })
  configured = true
  log(`解码器已配置: ${codec}`)
  while (pending.length) {
    const chunk = pending.shift()
    decodeChunk(chunk.data, chunk.nalType)
  }
}

const nalTypeFromAnnexB = (nal) => {
  let offset = 0
  if (nal[0] === 0 && nal[1] === 0) {
    if (nal[2] === 1) offset = 3
    else if (nal[2] === 0 && nal[3] === 1) offset = 4
  }
  return nal[offset] & 0x1f
}

const decodeChunk = (avccData, nalType) => {
  if (!decoder || decoder.state === 'closed') return
  const isKey = nalType === 5
  const chunk = new EncodedVideoChunk({
    type: isKey ? 'key' : 'delta',
    timestamp: timestamp,
    data: avccData,
  })
  timestamp += 33_000
  decoder.decode(chunk)
}

const renderFrame = async (frame) => {
  try {
    if (!canvas.value || !ctx) return
    if (canvas.value.width !== frame.codedWidth || canvas.value.height !== frame.codedHeight) {
      applyStreamResolution(frame.codedWidth, frame.codedHeight)
    }
    const bitmap = await createImageBitmap(frame)
    ctx.drawImage(bitmap, 0, 0, canvas.value.width, canvas.value.height)
    bitmap.close()
    frame.close()
    frameCount++
    const now = performance.now()
    if (now - lastFpsAt >= 1000) {
      overlayFps.value = frameCount.toString()
      frameCount = 0
      lastFpsAt = now
    }
  } catch (e) {
    log(`渲染帧错误: ${e.message}`)
  }
}

const splitAnnexB = (buffer) => {
  const data = new Uint8Array(buffer)
  const positions = []
  for (let i = 0; i < data.length - 3; i++) {
    if (data[i] === 0 && data[i + 1] === 0) {
      if (data[i + 2] === 1) {
        positions.push(i)
        i += 2
      } else if (data[i + 2] === 0 && data[i + 3] === 1) {
        positions.push(i)
        i += 3
      }
    }
  }
  if (positions.length === 0) return [data]
  const nals = []
  for (let i = 0; i < positions.length; i++) {
    const start = positions[i]
    const end = i + 1 < positions.length ? positions[i + 1] : data.length
    nals.push(data.slice(start, end))
  }
  return nals
}

const toAvcc = (nal) => {
  const raw = stripStartCode(nal)
  const len = raw.length
  const out = new Uint8Array(4 + len)
  out[0] = (len >>> 24) & 0xff
  out[1] = (len >>> 16) & 0xff
  out[2] = (len >>> 8) & 0xff
  out[3] = len & 0xff
  out.set(raw, 4)
  return out
}

const concatBuffers = (chunks) => {
  const total = chunks.reduce((s, c) => s + c.length, 0)
  const out = new Uint8Array(total)
  let offset = 0
  for (const c of chunks) {
    out.set(c, offset)
    offset += c.length
  }
  return out
}

const flushAccessUnit = (isKey) => {
  if (!decoder || decoder.state === 'closed') return
  if (!auParts.length) return
  const data = concatBuffers(auParts)
  auParts = []
  decodeChunk(data, isKey ? 5 : 1)
}

const handleBinary = (buffer) => {
  const nals = splitAnnexB(buffer)
  for (const nal of nals) {
    const nType = nalTypeFromAnnexB(nal)
    if (nType === 7) {
      sps = stripStartCode(nal)
      ensureDecoderConfigured()
      continue
    }
    if (nType === 8) {
      pps = stripStartCode(nal)
      ensureDecoderConfigured()
      continue
    }

    const avcc = toAvcc(nal)

    if (!configured) {
      pending.push({ data: avcc, nalType: nType })
      continue
    }

    if (!injectedConfig && sps && pps) {
      auParts.push(toAvcc(new Uint8Array([0, 0, 0, 1, ...sps])))
      auParts.push(toAvcc(new Uint8Array([0, 0, 0, 1, ...pps])))
      injectedConfig = true
    }

    if (nType === 9 || nType === 6 || nType === 7 || nType === 8) {
      auParts.push(avcc)
      continue
    }

    if (nType === 5 || nType === 1) {
      auParts.push(avcc)
      flushAccessUnit(nType === 5)
    } else {
      auParts.push(avcc)
      flushAccessUnit(false)
    }
  }
}

const resetDecoder = () => {
  if (decoder) {
    try {
      decoder.close()
    } catch (_) {}
  }
  decoder = null
  configured = false
  sps = null
  pps = null
  pending = []
  timestamp = 0
  frameCount = 0
  lastFpsAt = performance.now()
  auParts = []
  injectedConfig = false
  nativeResolution = null
  streamResolution = null
  overlaySize.value = '-'
  overlayFps.value = '0'
  clearCanvas()
}

const connectWebSocket = async () => {
  if (!props.sandboxId && !props.shareToken) {
    log('没有提供 sandboxId/shareToken')
    return
  }

  disconnectWebSocket()
  resetDecoder()

  overlayState.value = '连接中'
  emit('update:status', 'connecting')

  // 先尝试获取设备分辨率
  try {
    await fetchInputResolution()
    await fetchDeviceResolution()
  } catch (err) {
    log(`获取分辨率失败，使用视频流尺寸: ${err.message}`)
  }

  // 允许通过 VITE_API_BASE 自定义后端地址；否则使用同源
  const apiBase = import.meta.env.VITE_API_BASE || '/api/v1'
  // 规范化：若为相对路径，则拼同源；若为 http(s) 则替换为 ws
  const buildWsBase = () => {
    if (apiBase.startsWith('http')) {
      return apiBase.replace(/^http/, 'ws')
    }
    return window.location.origin.replace(/^http/, 'ws') + apiBase
  }

  const token = localStorage.getItem('sandroidx_token')
  // 注意 buildWsBase() 可能返回带路径的完整前缀，这里不要多次调用以保持一致
  const wsBase = buildWsBase()
  const sep = wsBase.includes('?') ? '&' : '?'
  const wsUrl = props.shareToken
    ? `${wsBase}/share/agents/${encodeURIComponent(props.shareToken)}/scrcpy`
    : `${wsBase}/sandboxes/scrcpy/${props.sandboxId}${token ? `${sep}token=${encodeURIComponent(token)}` : ''}`
  log(`连接到 ${wsUrl}`)

  try {
    ws = new WebSocket(wsUrl)
  } catch (e) {
    log(`创建 WebSocket 失败: ${e.message}`)
    overlayState.value = '连接失败'
    emit('update:status', 'disconnected')
    return
  }

  ws.binaryType = 'arraybuffer'

  ws.onopen = () => {
    log('WebSocket 已连接，等待视频流...')
    connected.value = true
    overlayState.value = '已连接'
    emit('update:status', 'playing')
  }

  ws.onmessage = (evt) => {
    if (typeof evt.data === 'string') {
      log(`收到文本消息: ${evt.data}`)
      // video ws 上的文本主要是 ready/error；input_ack 优先从 wsInput 收
    } else {
      handleBinary(evt.data)
    }
  }

  ws.onerror = (e) => {
    log(`WebSocket 错误`)
    console.error(e)
  }

  ws.onclose = () => {
    log('WebSocket 已断开')
    connected.value = false
    overlayState.value = '已断开'
    emit('update:status', 'disconnected')
    clearCanvas()
    cursorIndicator.value.visible = false
    cursorIndicator.value.active = false
  }
}

const disconnectWebSocket = () => {
  if (connectTimer) {
    clearTimeout(connectTimer)
    connectTimer = 0
  }
  if (ws) {
    try {
      ws.close()
    } catch (_) {}
  }
  ws = null
  connected.value = false
  overlayState.value = '未连接'
  clearCanvas()
  cursorIndicator.value.visible = false
  cursorIndicator.value.active = false
}

const handleStop = () => {
  disconnectWebSocket()
  resetDecoder()
  emit('stop')
}

const handleConnect = () => {
  // 关掉“启动”按钮后：把“连接”作为唯一入口
  // - 私有场景（非 shareToken）下：若未启动 scrcpy，则先触发 start（由父组件调用后端 startScrcpy），再连接视频流
  // - shareToken 场景：只能观看，直接连接
  if (!connected.value && !props.readonly && !props.shareToken) {
    // 父组件通常会把 status 设为 connecting/playing，这里避免重复触发
    if (props.status !== 'playing' && props.status !== 'connecting') {
      emit('start')
    }
    // 给后端一点时间拉起 scrcpy，再去连 WS
    if (connectTimer) clearTimeout(connectTimer)
    connectTimer = setTimeout(() => {
      connectTimer = 0
      connectWebSocket()
    }, 1000)
    return
  }
  connectWebSocket()
}

onMounted(() => {
  if (canvas.value) {
    ctx = canvas.value.getContext('2d')
  }
  
  // 窗口大小变化时重新计算尺寸
  const handleResize = () => {
    const layout = nativeResolution || streamResolution
    if (layout) applyLayoutResolution(layout.width, layout.height)
  }
  window.addEventListener('resize', handleResize)
  
  // 如果已经是 playing 状态，自动连接
  if (props.status === 'playing' && (props.sandboxId || props.shareToken)) {
    connectWebSocket()
  }
})

onBeforeUnmount(() => {
  // 这里留给后续：如果需要彻底移除 resize，需要把 handleResize 提升到顶层常量并复用引用
  if (connectTimer) {
    clearTimeout(connectTimer)
    connectTimer = 0
  }
  disconnectWebSocket()
  resetDecoder()
})

watch(() => props.sandboxId, (newId, oldId) => {
  if (newId && newId !== oldId && connected.value) {
    // sandbox 切换时重新连接
    connectWebSocket()
  }
})

watch(() => props.status, (newStatus) => {
  if (newStatus === 'playing' && (props.sandboxId || props.shareToken) && !connected.value) {
    connectWebSocket()
  } else if (newStatus === 'disconnected' && connected.value) {
    disconnectWebSocket()
  }
})
</script>

<template>
  <div class="player">
    <div class="player-header">
      <div class="title">
        <span class="dot" :class="props.status" />
        {{ title }}
      </div>
      <a-space>
        <a-button
          size="small"
          @click="handleConnect"
          :disabled="(!sandboxId && !shareToken) || connected"
        >
          {{ connected ? '已连接' : '连接' }}
        </a-button>
        <!-- <a-button size="small" type="text" danger @click="handleStop">停止</a-button> -->
      </a-space>
    </div>
    <div class="player-body" ref="playerWrapper">
      <canvas
        ref="canvas"
        class="video-canvas"
        tabindex="0"
        @pointerenter="handlePointerEnter"
        @pointerleave="handlePointerLeave"
        @pointerdown="handlePointerDown"
        @pointermove="handlePointerMove"
        @pointerup="handlePointerUp"
        @pointercancel="handlePointerCancel"
        @keydown="handleCanvasKeydown"
      ></canvas>
      <div
        v-if="connected && cursorIndicator.visible"
        class="touch-cursor"
        :class="{ active: cursorIndicator.active }"
        :style="{ left: `${cursorIndicator.x}px`, top: `${cursorIndicator.y}px` }"
      ></div>
      <div class="empty-state" v-if="!connected">
        <a-empty description="点击连接查看视频流" />
      </div>
    </div>
    <!-- 视频流信息显示 -->
    <div class="video-info" v-if="connected">
      <div class="info-item">
        <span class="info-label">分辨率</span>
        <span class="info-value">{{ overlaySize }}</span>
      </div>
      <div class="info-divider"></div>
      <div class="info-item">
        <span class="info-label">FPS</span>
        <span class="info-value">{{ overlayFps }}</span>
      </div>
      <div class="info-divider"></div>
      <div class="info-item">
        <span class="info-label">状态</span>
        <span class="info-value status-value">
          <span class="status-dot" :class="props.status"></span>
          {{ overlayState }}
        </span>
      </div>
    </div>

    <div class="input-bar" v-if="connected && !readonly">
      <a-space style="width: 100%" :size="8">
        <a-input
          v-model:value="textDraft"
          placeholder="在这里输入/粘贴文本（会发送到设备 input text；中文/IME 可能不稳定）"
          @pressEnter="sendTextDraft"
        />
        <a-button @click="sendTextDraft">发送</a-button>
        <a-button @click="sendBackspace">退格</a-button>
        <a-button @click="sendEnter">回车</a-button>
      </a-space>
    </div>
  </div>
</template>

<style scoped lang="less">
@import "../../styles/variables.less";

.player {
  background: @color-surface;
  border-radius: @radius-md;
  box-shadow: @shadow-soft;
  padding: @space-md;
  display: flex;
  flex-direction: column;
  gap: @space-md;
}

.player-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.title {
  display: inline-flex;
  align-items: center;
  gap: @space-sm;
  font-weight: @font-weight-medium;
}

.dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: fade(@color-primary-text, 30%);

  &.connecting {
    background: fade(@color-accent, 50%);
    animation: pulse 1.5s ease-in-out infinite;
  }

  &.playing {
    background: @color-accent;
  }
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}

.player-body {
  position: relative;
  min-height: 260px;
  background: #0d131f;
  border-radius: @radius-sm;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  align-self: center;
  margin: 0 auto;
  width: fit-content;
  max-width: 100%;
  border: 1px solid #1f2a3d;
}

.video-canvas {
  width: 100%;
  height: 100%;
  display: block;
  margin: 0 auto;
  object-fit: contain;
  background: #0a0f19;
  outline: none;
  touch-action: none;
  // 模拟 DevTools 手机模式：隐藏系统鼠标箭头，用圆形指示器替代
  cursor: none;
}

.touch-cursor {
  position: absolute;
  width: 26px;
  height: 26px;
  border-radius: 999px;
  border: 2px solid rgba(255, 255, 255, 0.92);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.35);
  transform: translate(-50%, -50%);
  pointer-events: none;
  opacity: 0.85;
  transition: opacity 120ms ease, transform 120ms ease, background-color 120ms ease;
  background: rgba(255, 255, 255, 0.08);
  backdrop-filter: blur(2px);
}

.touch-cursor::after {
  content: '';
  position: absolute;
  left: 50%;
  top: 50%;
  width: 6px;
  height: 6px;
  transform: translate(-50%, -50%);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.9);
  box-shadow: 0 1px 6px rgba(0, 0, 0, 0.35);
}

.touch-cursor.active {
  opacity: 1;
  transform: translate(-50%, -50%) scale(0.92);
  background: rgba(255, 255, 255, 0.18);
}

.input-bar {
  margin-top: @space-sm;
}

.video-info {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: @space-lg;
  padding: @space-sm @space-md;
  background: fade(@color-surface, 50%);
  border-radius: @radius-sm;
  border: 1px solid fade(@color-border, 30%);
  margin-top: @space-sm;
}

.info-item {
  display: flex;
  align-items: center;
  gap: @space-xs;
}

.info-label {
  font-size: 12px;
  color: fade(@color-primary-text, 60%);
  font-weight: @font-weight-medium;
}

.info-value {
  font-size: 13px;
  color: @color-primary-text;
  font-weight: @font-weight-semibold;
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
}

.status-value {
  display: flex;
  align-items: center;
  gap: 6px;
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: fade(@color-primary-text, 30%);
  
  &.connecting {
    background: fade(@color-accent, 70%);
    animation: pulse-dot 1.5s ease-in-out infinite;
  }
  
  &.playing {
    background: @color-accent;
  }
}

@keyframes pulse-dot {
  0%, 100% { 
    opacity: 1; 
    transform: scale(1);
  }
  50% { 
    opacity: 0.6;
    transform: scale(1.2);
  }
}

.info-divider {
  width: 1px;
  height: 16px;
  background: fade(@color-border, 20%);
}

.overlay {
  position: absolute;
  top: 10px;
  left: 10px;
  background: rgba(0, 0, 0, 0.35);
  padding: 8px 10px;
  border-radius: 8px;
  font-size: 12px;
  color: #cde1ff;
  backdrop-filter: blur(4px);
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.overlay-item {
  display: flex;
  align-items: center;
  gap: 6px;
}

.tag {
  padding: 4px 8px;
  border-radius: 12px;
  background: #1f2a3d;
  color: #8ad0ff;
  font-size: 12px;
  display: inline-block;
  font-weight: 600;
}

.empty-state {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
}
</style>
