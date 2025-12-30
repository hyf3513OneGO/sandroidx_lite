<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { listUsers, deleteUser } from '../../api/users'

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
  { title: '姓名', dataIndex: 'name', key: 'name' },
  { title: '邮箱', dataIndex: 'email', key: 'email' },
  { title: '角色', dataIndex: 'role', key: 'role' },
  { title: '操作', key: 'action' },
]

const fetchList = async () => {
  loading.value = true
  try {
    const { data } = await listUsers()
    dataSource.value = normalizeArray(data)
  } catch (err) {
    message.error(err.message || '加载失败')
  } finally {
    loading.value = false
  }
}

const handleDelete = async (id) => {
  try {
    await deleteUser(id)
    message.success('已删除')
    fetchList()
  } catch (err) {
    message.error(err.message || '删除失败')
  }
}

onMounted(fetchList)
</script>

<template>
  <div class="page-container list-page">
    <div class="page-title">
      <h1>用户管理</h1>
      <a-button type="primary" @click="router.push('/users/create')">创建用户</a-button>
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
              <a-button type="link" @click="router.push(`/users/${record.id}`)">编辑</a-button>
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
  gap: @space-md;
}

.page-title h1 {
  margin: 0;
  font-size: @font-size-h1;
}
</style>

