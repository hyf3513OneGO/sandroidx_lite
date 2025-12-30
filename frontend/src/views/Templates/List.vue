<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { listTemplates } from '../../api/templates'

const router = useRouter()
const loading = ref(false)
const dataSource = ref([])

const normalizeArray = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.data)) return payload.data
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload?.list)) return payload.list
  return []
}

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id' },
  { title: '名称', dataIndex: 'name', key: 'name' },
  { title: '描述', dataIndex: 'description', key: 'description' },
  { title: '创建时间', dataIndex: 'created_at', key: 'created_at' },
  { title: '操作', key: 'action' },
]

const fetchList = async () => {
  loading.value = true
  try {
    const { data } = await listTemplates()
    dataSource.value = normalizeArray(data)
  } catch (err) {
    message.error(err.message || '加载失败')
  } finally {
    loading.value = false
  }
}

onMounted(fetchList)
</script>

<template>
  <div class="page-container list-page">
    <div class="page-title">
      <h1>模板列表</h1>
      <p class="muted">创建、查看、编辑、删除。</p>
      <a-button type="primary" @click="router.push('/templates/create')">新建模板</a-button>
    </div>
    <a-card bordered>
      <a-table
        :data-source="dataSource"
        :columns="columns"
        :loading="loading"
        row-key="id"
        :pagination="false"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'action'">
            <a-space>
              <a-button type="link" @click="router.push(`/templates/${record.id}`)">编辑</a-button>
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
  gap: @space-md;
}

.page-title h1 {
  margin: 0;
  font-size: @font-size-h1;
}
</style>

