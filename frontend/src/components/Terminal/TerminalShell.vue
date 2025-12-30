<script setup>
import { ref, computed } from 'vue'

const props = defineProps({
  title: { type: String, default: '交互式 Shell' },
  logs: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  placeholder: { type: String, default: '输入命令后回车' },
  connected: { type: Boolean, default: false },
  readonly: { type: Boolean, default: false },
})

const emit = defineEmits(['send', 'raw', 'clear', 'reconnect'])
const input = ref('')

// 直接按行展示，不做任何格式化处理
const formattedLogs = computed(() => {
  return (props.logs ?? []).join('\n')
})


const handleSend = () => {
  if (!input.value) return
  emit('send', input.value)
  input.value = ''
}

const handleClear = () => {
  emit('clear')
}

const handleReconnect = () => {
  if (props.readonly) return
  emit('reconnect')
}

const handleInterrupt = () => {
  if (props.readonly) return
  if (!props.connected || props.loading) return
  // ETX: 等价 Ctrl+C
  emit('raw', '\x03')
}

const handleKeydown = (e) => {
  // 在输入框里模拟终端行为：Ctrl+C 发送 SIGINT（而不是复制）
  if (props.readonly) return
  if (!props.connected || props.loading) return
  if (e?.ctrlKey && !e?.altKey && !e?.metaKey && (e.key === 'c' || e.key === 'C')) {
    e.preventDefault()
    handleInterrupt()
  }
}
</script>

<template>
  <div class="terminal">
    <div class="terminal-header">
      <span>{{ title }}</span>
      <div class="status">
        <span class="dot" :class="connected ? 'on' : 'off'" />
        <span>{{ connected ? '已连接' : '未连接' }}</span>
        <span v-if="readonly" class="readonly-badge">只读</span>
        <a-spin v-if="loading" size="small" />
        <a-button 
          v-if="!connected && !readonly" 
          size="small" 
          type="primary"
          :loading="loading"
          @click="handleReconnect"
        >
          重连
        </a-button>
        <a-button size="small" :disabled="loading || logs.length === 0" @click="handleClear">清屏</a-button>
        <a-button
          size="small"
          danger
          :disabled="readonly || !connected || loading"
          @click="handleInterrupt"
        >
          停止(Ctrl+C)
        </a-button>
      </div>
    </div>
    <div class="terminal-body">
      <pre v-if="logs.length" class="log"><code>{{ formattedLogs }}</code></pre>
      <a-empty v-else description="等待输出..." />
    </div>
    <div class="terminal-input" v-if="!readonly">
      <a-input-search
        v-model:value="input"
        :placeholder="placeholder"
        enter-button="发送"
        :disabled="!connected || loading"
        @search="handleSend"
        @keydown="handleKeydown"
      />
    </div>
    <div class="terminal-input" v-else>
      <a-input :value="''" disabled placeholder="只读模式：不可输入" />
    </div>
  </div>
</template>

<style scoped lang="less">
@import "../../styles/variables.less";

.terminal {
  background: @color-primary-text;
  color: #f5f5f5;
  border-radius: @radius-md;
  padding: @space-md;
  display: flex;
  flex-direction: column;
  gap: @space-md;
  min-height: 280px;
}

.terminal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-weight: @font-weight-medium;
}
.status {
  display: inline-flex;
  align-items: center;
  gap: @space-xs;
  color: #dfe3e8;
}

.readonly-badge {
  padding: 0 8px;
  border: 1px solid rgba(255, 255, 255, 0.25);
  border-radius: 999px;
  font-size: 12px;
  line-height: 20px;
  opacity: 0.9;
}

.dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: fade(@color-surface, 30%);
  &.on {
    background: @color-accent;
  }
}

.terminal-body {
  flex: 1;
  background: rgba(255, 255, 255, 0.04);
  border-radius: @radius-sm;
  padding: @space-md;
  overflow: auto;
  min-height: 180px;
  max-height: 400px;
}

.log {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
}

.terminal-input :deep(.ant-input-search-button) {
  background: @color-accent;
  border-color: @color-accent;
}
</style>

