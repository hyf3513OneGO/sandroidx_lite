<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { message, Modal } from 'ant-design-vue'
import { listAgents, startAgent, stopAgent, deleteAgent } from '../../api/agents'
import { createAgentShare, getAgentShareSummary } from '../../api/share'

const router = useRouter()
const loading = ref(false)
const dataSource = ref([])
const shareSummary = ref({})
const shareLoadingId = ref('')
const selectedRowKeys = ref([])
const batchOperating = ref(false)

const normalizeArray = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.data)) return payload.data
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload?.list)) return payload.list
  return []
}

const fetchList = async () => {
  loading.value = true
  try {
    const { data } = await listAgents()
    dataSource.value = normalizeArray(data)
    try {
      const summaryResp = await getAgentShareSummary()
      shareSummary.value = summaryResp?.data?.summary || {}
    } catch (_) {
      shareSummary.value = {}
    }
  } catch (err) {
    message.error(err.message || '加载失败')
  } finally {
    loading.value = false
  }
}

const handleStart = async (id) => {
  try {
    await startAgent(id)
    message.success('已启动')
    fetchList()
  } catch (err) {
    message.error(err.message || '启动失败')
  }
}

const handleStop = async (id) => {
  try {
    await stopAgent(id)
    message.success('已停止')
    fetchList()
  } catch (err) {
    message.error(err.message || '停止失败')
  }
}

const handleDelete = async (id) => {
  try {
    await deleteAgent(id)
    message.success('已删除')
    fetchList()
  } catch (err) {
    message.error(err.message || '删除失败')
  }
}

// 批量操作
const selectedCount = computed(() => selectedRowKeys.value.length)

const handleBatchStart = async () => {
  if (selectedCount.value === 0) {
    message.warning('请先选择要启动的 Agent')
    return
  }
  batchOperating.value = true
  let successCount = 0
  let failCount = 0
  try {
    for (const id of selectedRowKeys.value) {
      try {
        await startAgent(id)
        successCount++
      } catch (err) {
        failCount++
        console.error(`启动 Agent ${id} 失败:`, err)
      }
    }
    if (failCount === 0) {
      message.success(`成功启动 ${successCount} 个 Agent`)
    } else {
      message.warning(`成功启动 ${successCount} 个，失败 ${failCount} 个`)
    }
    selectedRowKeys.value = []
    fetchList()
  } finally {
    batchOperating.value = false
  }
}

const handleBatchStop = async () => {
  if (selectedCount.value === 0) {
    message.warning('请先选择要停止的 Agent')
    return
  }
  batchOperating.value = true
  let successCount = 0
  let failCount = 0
  try {
    for (const id of selectedRowKeys.value) {
      try {
        await stopAgent(id)
        successCount++
      } catch (err) {
        failCount++
        console.error(`停止 Agent ${id} 失败:`, err)
      }
    }
    if (failCount === 0) {
      message.success(`成功停止 ${successCount} 个 Agent`)
    } else {
      message.warning(`成功停止 ${successCount} 个，失败 ${failCount} 个`)
    }
    selectedRowKeys.value = []
    fetchList()
  } finally {
    batchOperating.value = false
  }
}

const handleBatchRestart = async () => {
  if (selectedCount.value === 0) {
    message.warning('请先选择要重启的 Agent')
    return
  }
  batchOperating.value = true
  let successCount = 0
  let failCount = 0
  try {
    for (const id of selectedRowKeys.value) {
      try {
        await stopAgent(id)
        // 等待一下再启动
        await new Promise((resolve) => setTimeout(resolve, 1000))
        await startAgent(id)
        successCount++
      } catch (err) {
        failCount++
        console.error(`重启 Agent ${id} 失败:`, err)
      }
    }
    if (failCount === 0) {
      message.success(`成功重启 ${successCount} 个 Agent`)
    } else {
      message.warning(`成功重启 ${successCount} 个，失败 ${failCount} 个`)
    }
    selectedRowKeys.value = []
    fetchList()
  } finally {
    batchOperating.value = false
  }
}

const handleBatchDelete = () => {
  if (selectedCount.value === 0) {
    message.warning('请先选择要删除的 Agent')
    return
  }
  Modal.confirm({
    title: '确认批量删除',
    content: `确定要删除选中的 ${selectedCount.value} 个 Agent 吗？此操作不可恢复。`,
    onOk: async () => {
      batchOperating.value = true
      let successCount = 0
      let failCount = 0
      try {
        for (const id of selectedRowKeys.value) {
          try {
            await deleteAgent(id)
            successCount++
          } catch (err) {
            failCount++
            console.error(`删除 Agent ${id} 失败:`, err)
          }
        }
        if (failCount === 0) {
          message.success(`成功删除 ${successCount} 个 Agent`)
        } else {
          message.warning(`成功删除 ${successCount} 个，失败 ${failCount} 个`)
        }
        selectedRowKeys.value = []
        fetchList()
      } finally {
        batchOperating.value = false
      }
    },
  })
}

const rowSelection = {
  selectedRowKeys: selectedRowKeys,
  onChange: (keys) => {
    selectedRowKeys.value = keys
  },
}

const handleQuickShare = async (id) => {
  if (!id) return
  shareLoadingId.value = id
  try {
    const { data } = await createAgentShare(id, 168)
    const path = data?.share_path || ''
    const url = path ? `${window.location.origin}${path}` : ''
    if (!url) {
      message.error('生成分享链接失败')
      return
    }
    try {
      await navigator.clipboard.writeText(url)
      message.success('分享链接已复制')
    } catch (_) {
      message.success(`分享链接：${url}`)
    }
    // 刷新分享状态
    await fetchList()
  } catch (err) {
    message.error(err.message || '生成分享失败')
  } finally {
    shareLoadingId.value = ''
  }
}

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id' },
  { title: '镜像', dataIndex: 'image', key: 'image' },
  { title: '状态', dataIndex: 'status', key: 'status' },
  { title: 'Mapping', dataIndex: 'mapping_id', key: 'mapping_id' },
  { title: '分享', key: 'share' },
  { title: '操作', key: 'action' },
]

onMounted(fetchList)
</script>

<template>
  <div class="page-container list-page">
    <div class="page-title">
      <div>
        <h1>Agents</h1>
        <p class="muted">查看、启动/停止、进入详情。</p>
      </div>
      <a-button type="primary" @click="router.push('/agents/create')">创建 Agent</a-button>
    </div>
    <a-card bordered>
      <template #title>
        <a-space>
          <span>Agent 列表</span>
          <a-tag v-if="selectedCount > 0" color="blue">已选择 {{ selectedCount }} 项</a-tag>
        </a-space>
      </template>
      <template #extra>
        <a-space>
          <a-button
            type="primary"
            :disabled="selectedCount === 0 || batchOperating"
            :loading="batchOperating"
            @click="handleBatchStart"
          >
            批量启动
          </a-button>
          <a-button
            :disabled="selectedCount === 0 || batchOperating"
            :loading="batchOperating"
            @click="handleBatchStop"
          >
            批量停止
          </a-button>
          <a-button
            :disabled="selectedCount === 0 || batchOperating"
            :loading="batchOperating"
            @click="handleBatchRestart"
          >
            批量重启
          </a-button>
          <a-button
            danger
            :disabled="selectedCount === 0 || batchOperating"
            :loading="batchOperating"
            @click="handleBatchDelete"
          >
            批量删除
          </a-button>
        </a-space>
      </template>
      <a-table
        :data-source="dataSource"
        :columns="columns"
        :loading="loading"
        :row-selection="rowSelection"
        row-key="id"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'share'">
            <a-space>
              <a-tag v-if="(shareSummary?.[record.id] || 0) > 0" color="green">
                已分享 {{ shareSummary?.[record.id] || 0 }}
              </a-tag>
              <a-tag v-else color="default">未分享</a-tag>
            </a-space>
          </template>
          <template v-if="column.key === 'action'">
            <a-space>
              <a-button type="link" @click="router.push(`/agents/${record.id}`)">详情</a-button>
              <a-button
                type="link"
                :loading="shareLoadingId === record.id"
                @click="handleQuickShare(record.id)"
              >
                一键分享
              </a-button>
              <a-button type="link" @click="handleStart(record.id)">启动</a-button>
              <a-button type="link" @click="handleStop(record.id)">停止</a-button>
              <a-popconfirm title="确定删除？" @confirm="() => handleDelete(record.id)">
                <a-button type="link" danger>删除</a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>
  </div>
</template>

<style scoped lang="less">
@import "../../styles/variables.less";

.list-page {
  padding: @space-xl 0 @space-xxl;
  display: flex;
  flex-direction: column;
  gap: @space-lg;
}

.page-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: @space-md;
}

.page-title h1 {
  margin: 0 0 @space-xs;
  font-size: @font-size-h1;
}
</style>

