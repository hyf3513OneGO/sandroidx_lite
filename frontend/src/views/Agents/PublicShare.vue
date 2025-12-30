<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { message } from 'ant-design-vue'
import { getSharedAgent } from '../../api/share'
import FormBuilder from '../../components/FormBuilder/FormBuilder.vue'
import TerminalShell from '../../components/Terminal/TerminalShell.vue'
import ScrcpyPlayer from '../../components/VideoPlayer/ScrcpyPlayer.vue'

const route = useRoute()
const token = String(route.params.token || '')

const loading = ref(false)
const agent = ref(null)
const share = ref(null)
const execLoading = ref(false)
const execLogs = ref([])
const execConnected = ref(false)
const execWsRef = ref(null)

const scrcpyStatus = ref('playing')

const clearExecLogs = () => {
  execLogs.value = []
}

const fetchShared = async () => {
  if (!token) return
  loading.value = true
  try {
    const { data } = await getSharedAgent(token)
    agent.value = data?.agent || null
    share.value = data?.share || null
  } catch (err) {
    message.error(err.message || '分享加载失败')
  } finally {
    loading.value = false
  }
}

const closeExecWs = () => {
  if (execWsRef.value) {
    try {
      execWsRef.value.close()
    } catch (_) {}
  }
  execWsRef.value = null
  execConnected.value = false
}

// 追加流式 delta，只有遇到真正的 \n 才创建新行（与 agent 页面保持一致）
const appendDelta = (delta) => {
  // 移除 \r（避免终端控制字符干扰）
  const cleanDelta = delta.replace(/\r/g, '')
  if (!cleanDelta) return
  
  // 确保至少有一条 log
  if (execLogs.value.length === 0) {
    execLogs.value.push('')
  }
  
  // 追加到最后一条
  execLogs.value[execLogs.value.length - 1] += cleanDelta
  
  // 如果 delta 里带换行，把最后一条按换行拆开
  const last = execLogs.value[execLogs.value.length - 1]
  const parts = last.split('\n')
  execLogs.value[execLogs.value.length - 1] = parts[0]
  for (let i = 1; i < parts.length; i++) {
    execLogs.value.push(parts[i])
  }
}

const startExecStream = (variables) => {
  closeExecWs()
  execLogs.value = ['连接执行通道中...']
  const wsBase = window.location.origin.replace(/^http/, 'ws')
  const url = `${wsBase}/api/v1/share/agents/${encodeURIComponent(token)}/execute-running/ws`
  const ws = new WebSocket(url)
  execWsRef.value = ws

  ws.onopen = () => {
    execConnected.value = true
    execLogs.value = ['已连接执行通道，开始执行...']
    ws.send(JSON.stringify({ variables: variables || {} }))
  }
  ws.onmessage = (evt) => {
    const text = typeof evt.data === 'string' ? evt.data : ''
    if (!text) return
    try {
      const msg = JSON.parse(text)
      if (msg.type === 'start') {
        execLogs.value.push(`\n#${msg.index}/${msg.total}: ${msg.cmd}`)
        return
      }
      if (msg.type === 'output') {
        // 使用 appendDelta 处理流式输出，避免分词问题
        const chunk = msg.data || ''
        if (chunk) {
          appendDelta(chunk)
        }
        return
      }
      if (msg.type === 'exit') {
        execLogs.value.push(`(exit=${msg.exit_code})`)
        return
      }
      if (msg.type === 'done') {
        execLogs.value.push('\n执行完成')
        return
      }
      if (msg.type === 'error') {
        execLogs.value.push(`错误: ${msg.message || ''}`)
      }
    } catch (_) {
      // 兜底：非 JSON 直接输出
      appendDelta(text)
    }
  }
  ws.onerror = () => {
    execConnected.value = false
    execLogs.value.push('执行通道错误')
  }
  ws.onclose = (evt) => {
    execConnected.value = false
    execLogs.value.push(`执行通道已关闭 (code=${evt.code})`)
  }
}

const handleExecute = async (variables) => {
  if (!token) return
  execLoading.value = true
  try {
    startExecStream(variables || {})
  } catch (err) {
    message.error(err.message || '执行失败')
  } finally {
    execLoading.value = false
  }
}

onMounted(async () => {
  await fetchShared()
})

onBeforeUnmount(() => {
  closeExecWs()
})
</script>

<template>
  <div class="share-page">
    <div class="share-card">
      <div class="share-left">
        <div class="header">
          <div>
            <h2 class="title">{{ agent?.id || 'Agent 分享' }}</h2>
            <p class="muted">
              分享 Token: <span class="mono" v-text="share?.token || token"></span>
              <span v-if="share?.expires_at"> · 过期: {{ share.expires_at }}</span>
            </p>
          </div>
        </div>

        <a-card title="Running Commands" :loading="loading" bordered>
          <FormBuilder
            v-if="agent?.running_variables"
            :variables="agent.running_variables"
            :commands="agent.running_commands"
            :loading="execLoading"
            @submit="handleExecute"
          />
          <a-empty v-else description="暂无运行命令配置" />
        </a-card>

        <TerminalShell
          title="执行输出（实时）"
          :logs="execLogs"
          :connected="execConnected"
          :loading="false"
          :readonly="true"
          placeholder="只读模式"
          @clear="clearExecLogs"
        />
      </div>

      <div class="share-right">
        <ScrcpyPlayer
          title="Scrcpy（仅观看）"
          :share-token="token"
          :status="scrcpyStatus"
          :readonly="true"
        />
      </div>
    </div>
  </div>
</template>

<style scoped lang="less">
@import "../../styles/variables.less";

.share-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  background: @color-surface;
  padding: @space-xl @space-md;
}

.share-card {
  width: min(1200px, 100%);
  display: grid;
  grid-template-columns: 1.15fr 0.85fr;
  gap: @space-xl;
  background: @color-surface;
  box-shadow: @shadow-soft;
  border-radius: @radius-md;
  padding: @space-xl;

  @media (max-width: 992px) {
    grid-template-columns: 1fr;
  }
}

.share-left {
  display: flex;
  flex-direction: column;
  gap: @space-lg;
}

.header .title {
  margin: 0 0 @space-xs;
  font-size: @font-size-h2;
}

.header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: @space-md;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
}

</style>


