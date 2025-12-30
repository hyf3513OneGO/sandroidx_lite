<script setup>
import { DashboardOutlined, HddOutlined, CloudUploadOutlined, CloudDownloadOutlined } from '@ant-design/icons-vue'

defineProps({
  metrics: {
    type: Object,
    default: () => ({ cpu: 0, memory: 0, networkIn: 0, networkOut: 0 }),
  },
  loading: { type: Boolean, default: false },
})
</script>

<template>
  <a-card :loading="loading" :bordered="false">
    <div class="section-title">性能监控</div>
    <div class="grid">
      <div class="metric">
        <div class="label">
          <DashboardOutlined class="icon" />
          <span>CPU</span>
        </div>
        <p class="value">{{ metrics.cpu }}%</p>
      </div>
      <div class="metric">
        <div class="label">
          <HddOutlined class="icon" />
          <span>内存</span>
        </div>
        <p class="value">{{ metrics.memory }}%</p>
      </div>
      <div class="metric">
        <div class="label">
          <CloudUploadOutlined class="icon" />
          <span>网络上行</span>
        </div>
        <p class="value">{{ metrics.networkOut }} KB/s</p>
      </div>
      <div class="metric">
        <div class="label">
          <CloudDownloadOutlined class="icon" />
          <span>网络下行</span>
        </div>
        <p class="value">{{ metrics.networkIn }} KB/s</p>
      </div>
    </div>
  </a-card>
</template>

<style scoped lang="less">
@import "../../styles/variables.less";

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: @space-lg;
  margin-top: @space-md;
}

.metric {
  padding: @space-md;
  background: @color-surface-alt;
  border-radius: @radius-md;
}

.label {
  margin: 0 0 @space-xs;
  color: fade(@color-primary-text, 60%);
  font-size: @font-size-small;
  display: flex;
  align-items: center;
  gap: @space-xs;
}

.value {
  margin: 0;
  font-size: @font-size-h2;
  font-weight: @font-weight-medium;
}

.icon {
  color: @color-accent;
  font-size: @font-size-body;
}
</style>

