<script setup>
import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import * as echarts from 'echarts'
import {
  BuildOutlined,
  DatabaseOutlined,
  TeamOutlined,
  ThunderboltOutlined,
  RadarChartOutlined,
  CloudUploadOutlined,
  CloudDownloadOutlined,
  MobileOutlined,
  AppstoreOutlined,
} from '@ant-design/icons-vue'

import { getOverview } from '../api/overview'

const router = useRouter()

const loading = ref(true)
const errorText = ref('')
const overview = ref(null)

const trendEl = ref(null)
let trendChart = null
let timer = null

const maxPoints = 60
const trend = ref({
  t: [],
  rx: [],
  tx: [],
})

const counts = computed(() => overview.value?.counts || {})
const system = computed(() => overview.value?.system || {})
const api = computed(() => overview.value?.api || {})

const qpsText = computed(() => {
  const n = Number(api.value.qps || 0)
  if (Number.isNaN(n)) return '0.00'
  return n.toFixed(2)
})

const qpsMeta = computed(() => {
  const ok2 = api.value.ok_2xx_10s ?? 0
  const e4 = api.value.err_4xx_10s ?? 0
  const e5 = api.value.err_5xx_10s ?? 0
  return { ok2, e4, e5 }
})

const netInText = computed(() => Number(system.value.net_rx_kbps || 0).toFixed(2))
const netOutText = computed(() => Number(system.value.net_tx_kbps || 0).toFixed(2))

function go(path) {
  if (loading.value) return
  router.push(path)
}

const cpuProgress = computed(() => Number(system.value.cpu_percent || 0))
const memProgress = computed(() => Number(system.value.mem_percent || 0))

const loadText = computed(() => {
  const l1 = system.value.load1 ?? 0
  const l5 = system.value.load5 ?? 0
  const l15 = system.value.load15 ?? 0
  return `${l1.toFixed?.(2) ?? l1} / ${l5.toFixed?.(2) ?? l5} / ${l15.toFixed?.(2) ?? l15}`
})

const uptimeText = computed(() => {
  const sec = Number(system.value.uptime_sec || 0)
  if (!sec) return '—'
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d}天 ${h}小时`
  if (h > 0) return `${h}小时 ${m}分钟`
  return `${m}分钟`
})

const diskText = computed(() => {
  const root = Number(system.value.disk_root_percent || 0).toFixed(1)
  const dataPath = system.value.disk_data_path || ''
  const dataPctRaw = system.value.disk_data_percent
  if (!dataPath) return `/ ${root}%`

  const dataPct = Number(dataPctRaw || 0).toFixed(1)
  // 同盘（或同分区）时展示一次即可
  if (Math.abs(Number(root) - Number(dataPct)) < 0.1) {
    return `/ ${root}% · data 同盘`
  }
  return `/ ${root}% · data ${dataPct}%`
})

function pushPoint() {
  const now = new Date()
  const label = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(
    2,
    '0',
  )}:${String(now.getSeconds()).padStart(2, '0')}`

  trend.value.t.push(label)
  trend.value.rx.push(Number(system.value.net_rx_kbps || 0))
  trend.value.tx.push(Number(system.value.net_tx_kbps || 0))

  if (trend.value.t.length > maxPoints) {
    trend.value.t.shift()
    trend.value.rx.shift()
    trend.value.tx.shift()
  }
}

function renderTrend() {
  if (!trendChart) return
  trendChart.setOption(
    {
      grid: { left: 42, right: 16, top: 18, bottom: 26 },
      tooltip: {
        trigger: 'axis',
        formatter: (params) => {
          const list = Array.isArray(params) ? params : [params]
          const axisLabel = list?.[0]?.axisValueLabel ?? ''
          const lines = list
            .map((p) => {
              const n = Number(p?.data)
              const v = Number.isNaN(n) ? '—' : n.toFixed(2)
              return `${p.marker}${p.seriesName} <span style="font-weight:600">${v} KB/s</span>`
            })
            .join('<br/>')
          return `${axisLabel}<br/>${lines}`
        },
      },
      legend: { top: 0, right: 8, itemWidth: 10, itemHeight: 10 },
      xAxis: {
        type: 'category',
        data: trend.value.t,
        boundaryGap: false,
        axisLabel: { color: 'rgba(17,24,39,.55)' },
        axisLine: { lineStyle: { color: 'rgba(17,24,39,.12)' } },
      },
      yAxis: {
        type: 'value',
        name: 'KB/s',
        axisLabel: {
          color: 'rgba(17,24,39,.55)',
          formatter: (v) => Number(v).toFixed(2),
        },
        splitLine: { lineStyle: { color: 'rgba(17,24,39,.08)' } },
      },
      series: [
        {
          name: '下行',
          type: 'line',
          showSymbol: false,
          smooth: true,
          lineStyle: { width: 1.5, color: 'rgba(2,132,199,.9)' },
          data: trend.value.rx,
        },
        {
          name: '上行',
          type: 'line',
          showSymbol: false,
          smooth: true,
          lineStyle: { width: 1.5, color: 'rgba(124,58,237,.9)' },
          data: trend.value.tx,
        },
      ],
    },
    { notMerge: true },
  )
}

async function refresh() {
  try {
    const { data } = await getOverview()
    overview.value = data
    pushPoint()
    renderTrend()
    errorText.value = ''
  } catch (e) {
    errorText.value = e?.message || '获取总览信息失败'
  } finally {
    loading.value = false
  }
}

function initChart() {
  if (!trendEl.value) return
  trendChart = echarts.init(trendEl.value)
  renderTrend()
  window.addEventListener('resize', onResize)
}

function onResize() {
  trendChart?.resize()
}

onMounted(async () => {
  await refresh()
  initChart()
  timer = window.setInterval(refresh, 5000)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
  window.removeEventListener('resize', onResize)
  trendChart?.dispose()
  trendChart = null
})
</script>

<template>
  <div class="page-container overview">
    <div class="page-head">
      <div class="title">
        <p class="badge">SAndroidX Lite · Overview</p>
        <h1>系统总览</h1>
        <p class="muted">实例规模、资源水位、实时趋势，一屏掌控。</p>
      </div>
    </div>

    <a-alert
      v-if="errorText"
      type="error"
      show-icon
      :message="errorText"
      class="mb"
    />

    <div class="stats-grid">
      <a-card
        class="stat-card glow glow-blue clickable"
        :loading="loading"
        :bordered="false"
        hoverable
        @click="go('/sandboxes')"
      >
        <div class="stat-top">
          <div class="stat-label">
            <BuildOutlined class="stat-icon" />
            <span>Sandboxes</span>
          </div>
          <span class="stat-chip">运行 {{ counts.sandboxes_running || 0 }}</span>
        </div>
        <div class="stat-value">{{ counts.sandboxes_total || 0 }}</div>
        <div class="stat-foot muted">总数 / 运行中</div>
      </a-card>

      <a-card
        class="stat-card glow glow-violet clickable"
        :loading="loading"
        :bordered="false"
        hoverable
        @click="go('/agents')"
      >
        <div class="stat-top">
          <div class="stat-label">
            <AppstoreOutlined class="stat-icon" />
            <span>Agents</span>
          </div>
          <span class="stat-chip">运行 {{ counts.agents_running || 0 }}</span>
        </div>
        <div class="stat-value">{{ counts.agents_total || 0 }}</div>
        <div class="stat-foot muted">总数 / 运行中</div>
      </a-card>

      <a-card
        class="stat-card glow glow-amber clickable"
        :loading="loading"
        :bordered="false"
        hoverable
        @click="go('/templates')"
      >
        <div class="stat-top">
          <div class="stat-label">
            <DatabaseOutlined class="stat-icon" />
            <span>Templates</span>
          </div>
          <span class="stat-chip subtle">配置</span>
        </div>
        <div class="stat-value">{{ counts.templates_total || 0 }}</div>
        <div class="stat-foot muted">模板数量</div>
      </a-card>

      <a-card
        class="stat-card glow glow-emerald clickable"
        :loading="loading"
        :bordered="false"
        hoverable
        @click="go('/users')"
      >
        <div class="stat-top">
          <div class="stat-label">
            <TeamOutlined class="stat-icon" />
            <span>Users</span>
          </div>
          <span class="stat-chip subtle">权限</span>
        </div>
        <div class="stat-value">{{ counts.users_total || 0 }}</div>
        <div class="stat-foot muted">用户数量</div>
      </a-card>

      <a-card
        class="stat-card glow glow-cyan clickable"
        :loading="loading"
        :bordered="false"
        hoverable
        @click="go('/apks')"
      >
        <div class="stat-top">
          <div class="stat-label">
            <MobileOutlined class="stat-icon" />
            <span>APKs</span>
          </div>
          <span class="stat-chip subtle">应用</span>
        </div>
        <div class="stat-value">{{ counts.apks_total || 0 }}</div>
        <div class="stat-foot muted">APK 数量</div>
      </a-card>
    </div>

    <div class="main-grid">
      <div class="left">
        <a-card :bordered="false" :loading="loading" class="api-card">
          <div class="section-title">API 指标</div>
          <div class="api-grid">
            <div class="api-main">
              <div class="api-k muted">QPS（近 10s 平均）</div>
              <div class="api-v">{{ qpsText }}</div>
              <div class="api-sub muted">
                2xx {{ qpsMeta.ok2 }} · 4xx {{ qpsMeta.e4 }} · 5xx {{ qpsMeta.e5 }}
              </div>
            </div>
            <div class="api-net">
              <div class="api-line">
                <span class="k muted">
                  <CloudDownloadOutlined />
                  下行
                </span>
                <span class="v">{{ netInText }} KB/s</span>
              </div>
              <div class="api-line">
                <span class="k muted">
                  <CloudUploadOutlined />
                  上行
                </span>
                <span class="v">{{ netOutText }} KB/s</span>
              </div>
              <div class="api-hint muted">网络趋势见右侧折线图</div>
            </div>
          </div>
        </a-card>

        <a-card :bordered="false" :loading="loading" class="ring-card">
          <div class="section-title">资源水位</div>
          <div class="rings">
            <div class="ring">
              <div class="ring-title">
                <ThunderboltOutlined class="mini-icon" />
                <span>CPU</span>
              </div>
              <a-progress
                type="circle"
                :percent="Number(cpuProgress.toFixed(1))"
                :stroke-width="10"
                :size="110"
              />
            </div>
            <div class="ring">
              <div class="ring-title">
                <RadarChartOutlined class="mini-icon" />
                <span>内存</span>
              </div>
              <a-progress
                type="circle"
                :percent="Number(memProgress.toFixed(1))"
                :stroke-width="10"
                :size="110"
                status="active"
              />
            </div>
            <div class="kv">
              <div class="kv-row">
                <span class="k muted">Load(1/5/15)</span>
                <span class="v">{{ loadText }}</span>
              </div>
              <div class="kv-row">
                <span class="k muted">Uptime</span>
                <span class="v">{{ uptimeText }}</span>
              </div>
              <div class="kv-row">
                <span class="k muted">Host</span>
                <span class="v">{{ system.hostname || '—' }}</span>
              </div>
              <div class="kv-row">
                <span class="k muted">Disk</span>
                <span class="v">{{ diskText }}</span>
              </div>
            </div>
          </div>
        </a-card>
      </div>

      <div class="right">
        <a-card :bordered="false" class="trend-card" :loading="loading">
          <div class="section-title">实时趋势</div>
          <div ref="trendEl" class="trend" />
        </a-card>

        <a-card :bordered="false" class="meta-card" :loading="loading">
          <div class="section-title">运行环境</div>
          <div class="meta-grid">
            <div class="meta-item">
              <div class="meta-k muted">OS / Arch</div>
              <div class="meta-v">{{ system.os || '—' }} / {{ system.arch || '—' }}</div>
            </div>
            <div class="meta-item">
              <div class="meta-k muted">Go</div>
              <div class="meta-v">{{ system.go_version || '—' }}</div>
            </div>
            <div class="meta-item">
              <div class="meta-k muted">Goroutines</div>
              <div class="meta-v">{{ system.goroutines ?? '—' }}</div>
            </div>
            <div class="meta-item">
              <div class="meta-k muted">Server Time</div>
              <div class="meta-v">{{ overview?.server_time || '—' }}</div>
            </div>
          </div>
        </a-card>
      </div>
    </div>
  </div>
</template>

<style scoped lang="less">
@import "../styles/variables.less";

.overview {
  display: flex;
  flex-direction: column;
  gap: @space-xl;
  padding: @space-xl 0 @space-xxl;
}

.page-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: @space-lg;
  flex-wrap: wrap;
}

.title h1 {
  margin: @space-sm 0 @space-xs;
  font-size: @font-size-h1;
  font-weight: @font-weight-bold;
  line-height: 1.15;
}

.badge {
  display: inline-block;
  padding: 4px 10px;
  border-radius: @radius-sm;
  background: fade(@color-accent, 10%);
  color: @color-accent;
  font-weight: @font-weight-medium;
  font-size: @font-size-small;
  margin: 0 0 @space-sm;
}

.mb {
  margin-bottom: @space-md;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: @space-lg;
  @media (max-width: 1400px) {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  @media (max-width: 1100px) {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  @media (max-width: 560px) {
    grid-template-columns: 1fr;
  }
}

.stat-card {
  position: relative;
  overflow: hidden;
  border-radius: @radius-lg;
  background: linear-gradient(180deg, #ffffff 0%, #fbfdff 100%);
  box-shadow: @shadow-soft;
}

.stat-card.clickable {
  cursor: pointer;
  transition: transform 160ms ease, box-shadow 160ms ease;
}

.stat-card.clickable:hover {
  transform: translateY(-2px);
}

.stat-card.glow::before {
  content: "";
  position: absolute;
  inset: -2px;
  background: radial-gradient(600px circle at 30% 10%, rgba(29, 78, 216, .20), transparent 45%);
  pointer-events: none;
}

.stat-card.glow.glow-blue::before {
  background: radial-gradient(600px circle at 30% 10%, rgba(29, 78, 216, .20), transparent 45%);
}

.stat-card.glow.glow-violet::before {
  background: radial-gradient(600px circle at 30% 10%, rgba(124, 58, 237, .18), transparent 45%);
}

.stat-card.glow.glow-amber::before {
  background: radial-gradient(600px circle at 30% 10%, rgba(245, 158, 11, .16), transparent 45%);
}

.stat-card.glow.glow-emerald::before {
  background: radial-gradient(600px circle at 30% 10%, rgba(16, 185, 129, .16), transparent 45%);
}

.stat-card.glow.glow-cyan::before {
  background: radial-gradient(600px circle at 30% 10%, rgba(6, 182, 212, .18), transparent 45%);
}

.stat-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: @space-sm;
}

.stat-label {
  display: flex;
  align-items: center;
  gap: @space-xs;
  color: fade(@color-primary-text, 70%);
  font-size: @font-size-small;
}

.stat-icon {
  color: @color-accent;
}

.stat-chip {
  padding: 2px 10px;
  border-radius: 999px;
  font-size: 12px;
  color: #1d4ed8;
  background: rgba(29, 78, 216, .10);
  border: 1px solid rgba(29, 78, 216, .12);
}

.stat-chip.subtle {
  color: fade(@color-primary-text, 55%);
  background: rgba(17, 24, 39, .04);
  border-color: rgba(17, 24, 39, .06);
}

.stat-value {
  margin-top: @space-sm;
  font-size: 40px;
  font-weight: @font-weight-bold;
  letter-spacing: -0.8px;
  color: @color-primary-text;
}

.stat-foot {
  margin-top: @space-xs;
}

.main-grid {
  display: grid;
  grid-template-columns: 1.2fr 1fr;
  gap: @space-lg;
  align-items: start;
  @media (max-width: 1100px) {
    grid-template-columns: 1fr;
  }
}

.left,
.right {
  display: flex;
  flex-direction: column;
  gap: @space-lg;
}

.ring-card {
  border-radius: @radius-lg;
  box-shadow: @shadow-soft;
}

.api-card {
  border-radius: @radius-lg;
  box-shadow: @shadow-soft;
}

.api-grid {
  display: grid;
  grid-template-columns: 1.1fr 1fr;
  gap: @space-lg;
  margin-top: @space-md;

  @media (max-width: 560px) {
    grid-template-columns: 1fr;
  }
}

.api-k {
  margin-bottom: 6px;
}

.api-v {
  font-size: 44px;
  font-weight: @font-weight-bold;
  letter-spacing: -0.8px;
  color: @color-primary-text;
  line-height: 1;
}

.api-sub {
  margin-top: 10px;
}

.api-net {
  display: grid;
  gap: 10px;
  align-content: start;
}

.api-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: @space-md;
  border-bottom: 1px dashed rgba(17, 24, 39, .08);
  padding-bottom: 8px;
}

.api-hint {
  margin-top: 6px;
}

.rings {
  display: grid;
  grid-template-columns: 140px 140px 1fr;
  gap: @space-lg;
  align-items: center;
  margin-top: @space-md;
  @media (max-width: 560px) {
    grid-template-columns: 1fr;
  }
}

.ring-title {
  display: flex;
  align-items: center;
  gap: @space-xs;
  margin-bottom: @space-sm;
  color: fade(@color-primary-text, 70%);
}

.mini-icon {
  color: @color-accent;
}

.kv {
  display: grid;
  gap: 10px;
}

.kv-row {
  display: flex;
  justify-content: space-between;
  gap: @space-md;
  border-bottom: 1px dashed rgba(17, 24, 39, .08);
  padding-bottom: 8px;
}

.k {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.v {
  color: @color-primary-text;
  font-weight: @font-weight-medium;
}

.trend-card {
  border-radius: @radius-lg;
  box-shadow: @shadow-soft;
}

.trend {
  height: 320px;
  width: 100%;
  margin-top: @space-md;
}

.meta-card {
  border-radius: @radius-lg;
  box-shadow: @shadow-soft;
}

.meta-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: @space-lg;
  margin-top: @space-md;
  @media (max-width: 560px) {
    grid-template-columns: 1fr;
  }
}

.meta-k {
  font-size: @font-size-small;
}

.meta-v {
  margin-top: 6px;
  font-weight: @font-weight-medium;
  color: @color-primary-text;
}
</style>

