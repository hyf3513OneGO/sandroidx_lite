<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { message, Modal } from 'ant-design-vue'
import {
  createSandbox,
  listSandboxes,
  getSandbox,
  startSandbox,
  stopSandbox,
  deleteSandbox,
} from '../../api/sandboxes'

const router = useRouter()
const loading = ref(false)
const dataSource = ref([])
const selectedRowKeys = ref([])
const batchOperating = ref(false)

const normalizeArray = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.data)) return payload.data
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload?.list)) return payload.list
  return []
}

const createVisible = ref(false)
const createLoading = ref(false)
const createForm = reactive({
  type: 'phone/redroid',
  image: '',
  ports: [''],
  privileged: false,
  args: [''],
  setup_adb_commands: [''],
  envs: [{ key: '', value: '' }],
  mounts: [{ volume: '', container_path: '/data', read_only: false }],
  apks: [{ name: '', package_name: '', version: '', url: '', type: 'remote' }],
})

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id' },
  { title: '类型', dataIndex: 'type', key: 'type' },
  { title: '镜像', dataIndex: 'image', key: 'image' },
  { title: '状态', dataIndex: 'status', key: 'status' },
  { title: 'Mapping', dataIndex: 'adb_mapping_id', key: 'adb_mapping_id' },
  { title: '端口', dataIndex: 'ports', key: 'ports' },
  { title: '操作', key: 'action' },
]

const addRow = (list, row) => list.push({ ...row })
const removeRow = (list, index) => list.splice(index, 1)

const toMap = (entries = []) => {
  return entries.reduce((acc, cur) => {
    const key = (cur.key || '').trim()
    if (key) acc[key] = cur.value
    return acc
  }, {})
}

const openCreate = () => {
  createVisible.value = true
}

const resetCreateForm = () => {
  createForm.type = 'phone/redroid'
  createForm.image = ''
  createForm.ports = ['']
  createForm.privileged = false
  createForm.args = ['']
  createForm.setup_adb_commands = ['']
  createForm.envs = [{ key: '', value: '' }]
  createForm.mounts = [{ volume: '', container_path: '/data', read_only: false }]
  createForm.apks = [{ name: '', package_name: '', version: '', url: '', type: 'remote' }]
}

const handleCreate = async () => {
  const image = (createForm.image || '').trim()
  if (!image) {
    message.error('请填写镜像')
    return
  }

  createLoading.value = true
  message.loading('正在创建 Sandbox...', 0)
  try {
    const payload = {
      type: (createForm.type || '').trim() || 'phone/redroid',
      image,
      ports: (createForm.ports || []).map((p) => (p || '').trim()).filter(Boolean),
      privileged: !!createForm.privileged,
      args: (createForm.args || []).map((a) => (a || '').trim()).filter(Boolean),
      setup_adb_commands: (createForm.setup_adb_commands || [])
        .map((c) => (c || '').trim())
        .filter(Boolean),
      envs: toMap(createForm.envs || []),
      mounts: (createForm.mounts || [])
        .filter((m) => (m?.container_path || '').trim())
        .map((m) => ({
          volume: (m.volume || '').trim(),
          container_path: (m.container_path || '').trim(),
          read_only: !!m.read_only,
        })),
      apks: (createForm.apks || [])
        .filter((a) => (a?.package_name || '').trim() && (a?.version || '').trim())
        .map((a) => ({
          name: (a.name || '').trim(),
          package_name: (a.package_name || '').trim(),
          version: (a.version || '').trim(),
          url: (a.url || '').trim(),
          type: (a.type || 'remote').trim(),
        })),
    }

    const { data } = await createSandbox(payload)
    message.destroy()
    message.success('Sandbox 创建请求已提交（异步）')
    createVisible.value = false
    resetCreateForm()
    await fetchList()
    if (data?.id) router.push(`/sandboxes/${data.id}`)
  } catch (err) {
    message.destroy()
    message.error(err.message || '创建失败')
  } finally {
    createLoading.value = false
  }
}

const fetchList = async () => {
  loading.value = true
  try {
    const { data } = await listSandboxes()
    dataSource.value = normalizeArray(data)
  } catch (err) {
    message.error(err.message || '加载失败')
  } finally {
    loading.value = false
  }
}

const handleStart = async (id) => {
  message.info('开始启动')
  try {
    await startSandbox(id)
    message.success('已启动')
    fetchList()
  } catch (err) {
    message.error(err.message || '启动失败')
  }
}

const handleStop = async (id) => {
  message.info('开始停止')
  try {
    await stopSandbox(id)
    message.success('已停止')
    fetchList()
  } catch (err) {
    message.error(err.message || '停止失败')
  }
}

const handleDelete = async (id) => {
  try {
    // 获取 sandbox 详细信息，以便获取 volumes
    const { data: sandbox } = await getSandbox(id)
    // 获取所有非系统 volumes 的 ID
    const volumesToDelete = []
    if (sandbox?.volumes && Array.isArray(sandbox.volumes)) {
      for (const vol of sandbox.volumes) {
        // 只删除非系统 volumes (volume_type !== 'system')
        if (vol.volume_type !== 'system' && vol.volume_id) {
          volumesToDelete.push(vol.volume_id)
        }
      }
    }
    await deleteSandbox(id, volumesToDelete)
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
    message.warning('请先选择要启动的 Sandbox')
    return
  }
  batchOperating.value = true
  let successCount = 0
  let failCount = 0
  try {
    for (const id of selectedRowKeys.value) {
      try {
        await startSandbox(id)
        successCount++
      } catch (err) {
        failCount++
        console.error(`启动 Sandbox ${id} 失败:`, err)
      }
    }
    if (failCount === 0) {
      message.success(`成功启动 ${successCount} 个 Sandbox`)
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
    message.warning('请先选择要停止的 Sandbox')
    return
  }
  batchOperating.value = true
  let successCount = 0
  let failCount = 0
  try {
    for (const id of selectedRowKeys.value) {
      try {
        await stopSandbox(id)
        successCount++
      } catch (err) {
        failCount++
        console.error(`停止 Sandbox ${id} 失败:`, err)
      }
    }
    if (failCount === 0) {
      message.success(`成功停止 ${successCount} 个 Sandbox`)
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
    message.warning('请先选择要重启的 Sandbox')
    return
  }
  batchOperating.value = true
  let successCount = 0
  let failCount = 0
  try {
    for (const id of selectedRowKeys.value) {
      try {
        await stopSandbox(id)
        // 等待一下再启动
        await new Promise((resolve) => setTimeout(resolve, 1000))
        await startSandbox(id)
        successCount++
      } catch (err) {
        failCount++
        console.error(`重启 Sandbox ${id} 失败:`, err)
      }
    }
    if (failCount === 0) {
      message.success(`成功重启 ${successCount} 个 Sandbox`)
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
    message.warning('请先选择要删除的 Sandbox')
    return
  }
  Modal.confirm({
    title: '确认批量删除',
    content: `确定要删除选中的 ${selectedCount.value} 个 Sandbox 吗？此操作不可恢复。将自动删除非系统 volumes。`,
    onOk: async () => {
      batchOperating.value = true
      let successCount = 0
      let failCount = 0
      try {
        for (const id of selectedRowKeys.value) {
          try {
            // 获取 sandbox 详细信息，以便获取 volumes
            const { data: sandbox } = await getSandbox(id)
            // 获取所有非系统 volumes 的 ID
            const volumesToDelete = []
            if (sandbox?.volumes && Array.isArray(sandbox.volumes)) {
              for (const vol of sandbox.volumes) {
                // 只删除非系统 volumes (volume_type !== 'system')
                if (vol.volume_type !== 'system' && vol.volume_id) {
                  volumesToDelete.push(vol.volume_id)
                }
              }
            }
            await deleteSandbox(id, volumesToDelete)
            successCount++
            // 每个删除成功都弹出提示
            message.success(`已删除 Sandbox: ${id}`)
          } catch (err) {
            failCount++
            console.error(`删除 Sandbox ${id} 失败:`, err)
            message.error(`删除 Sandbox ${id} 失败: ${err.message || '未知错误'}`)
          }
        }
        if (failCount === 0) {
          message.success(`批量删除完成：成功删除 ${successCount} 个 Sandbox`)
        } else {
          message.warning(`批量删除完成：成功 ${successCount} 个，失败 ${failCount} 个`)
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

onMounted(fetchList)
</script>

<template>
  <div class="page-container list-page">
    <div class="page-title">
      <div>
        <h1>Sandboxes</h1>
        <p class="muted">查看、启动/停止、删除、进入详情。</p>
      </div>
      <a-space>
        <a-button type="primary" @click="openCreate">创建</a-button>
        <a-button @click="fetchList">刷新</a-button>
      </a-space>
    </div>
    <a-card bordered>
      <template #title>
        <a-space>
          <span>Sandbox 列表</span>
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
          <template v-if="column.key === 'ports'">
            {{ (record.ports || []).join(', ') || '-' }}
          </template>
          <template v-if="column.key === 'action'">
            <a-space>
              <a-button type="link" @click="router.push(`/sandboxes/${record.id}`)">详情</a-button>
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

    <a-modal
      v-model:open="createVisible"
      title="创建 Sandbox"
      :confirm-loading="createLoading"
      ok-text="创建"
      cancel-text="取消"
      :mask-closable="false"
      :width="920"
      @ok="handleCreate"
      @cancel="() => (createVisible = false)"
    >
      <a-form layout="vertical">
        <a-row :gutter="[16, 16]">
          <a-col :xs="24" :md="12">
            <a-form-item label="类型">
              <a-input v-model:value="createForm.type" placeholder="phone/redroid" />
            </a-form-item>
          </a-col>
          <a-col :xs="24" :md="12">
            <a-form-item label="镜像" required>
              <a-input v-model:value="createForm.image" placeholder="镜像名称（必填）" />
            </a-form-item>
          </a-col>
        </a-row>

        <a-form-item>
          <a-switch v-model:checked="createForm.privileged" /> 特权模式
        </a-form-item>

        <a-row :gutter="[16, 16]">
          <a-col :xs="24" :md="12">
            <div class="section-title">端口（支持 5555 / 8080:5555 / 5555-5560）</div>
            <div class="kv-list">
              <div v-for="(p, idx) in createForm.ports" :key="idx" class="kv-row">
                <a-input v-model:value="createForm.ports[idx]" placeholder="例如：5555 或 8080:5555" />
                <a-button
                  type="text"
                  danger
                  :disabled="createForm.ports.length === 1"
                  @click="removeRow(createForm.ports, idx)"
                >
                  删除
                </a-button>
              </div>
              <a-button type="dashed" block @click="createForm.ports.push('')">添加端口</a-button>
            </div>
          </a-col>

          <a-col :xs="24" :md="12">
            <div class="section-title">挂载</div>
            <div class="kv-list">
              <div v-for="(m, idx) in createForm.mounts" :key="idx" class="kv-row">
                <a-input v-model:value="m.volume" placeholder="Volume ID（可空，自动创建）" />
                <a-input v-model:value="m.container_path" placeholder="/data" />
                <a-switch v-model:checked="m.read_only" size="small" /> 只读
                <a-button
                  type="text"
                  danger
                  :disabled="createForm.mounts.length === 1"
                  @click="removeRow(createForm.mounts, idx)"
                >
                  删除
                </a-button>
              </div>
              <a-button
                type="dashed"
                block
                @click="
                  addRow(createForm.mounts, {
                    volume: '',
                    container_path: '/data',
                    read_only: false,
                  })
                "
              >
                添加挂载
              </a-button>
            </div>
          </a-col>
        </a-row>

        <a-row :gutter="[16, 16]" style="margin-top: 12px">
          <a-col :xs="24" :md="12">
            <div class="section-title">环境变量</div>
            <div class="kv-list">
              <div v-for="(e, idx) in createForm.envs" :key="idx" class="kv-row">
                <a-input v-model:value="e.key" placeholder="KEY" />
                <a-input v-model:value="e.value" placeholder="VALUE" />
                <a-button
                  type="text"
                  danger
                  :disabled="createForm.envs.length === 1"
                  @click="removeRow(createForm.envs, idx)"
                >
                  删除
                </a-button>
              </div>
              <a-button type="dashed" block @click="addRow(createForm.envs, { key: '', value: '' })">
                添加变量
              </a-button>
            </div>
          </a-col>

          <a-col :xs="24" :md="12">
            <div class="section-title">启动参数（args）</div>
            <div class="kv-list">
              <div v-for="(a, idx) in createForm.args" :key="idx" class="kv-row">
                <a-input v-model:value="createForm.args[idx]" placeholder="单条参数，例如：--foo=bar" />
                <a-button
                  type="text"
                  danger
                  :disabled="createForm.args.length === 1"
                  @click="removeRow(createForm.args, idx)"
                >
                  删除
                </a-button>
              </div>
              <a-button type="dashed" block @click="createForm.args.push('')">添加参数</a-button>
            </div>

            <div class="section-title" style="margin-top: 12px">ADB 初始化命令（不含 adb 前缀）</div>
            <div class="kv-list">
              <div v-for="(c, idx) in createForm.setup_adb_commands" :key="idx" class="kv-row">
                <a-input v-model:value="createForm.setup_adb_commands[idx]" placeholder="例如：install -r /path/app.apk" />
                <a-button
                  type="text"
                  danger
                  :disabled="createForm.setup_adb_commands.length === 1"
                  @click="removeRow(createForm.setup_adb_commands, idx)"
                >
                  删除
                </a-button>
              </div>
              <a-button type="dashed" block @click="createForm.setup_adb_commands.push('')">
                添加命令
              </a-button>
            </div>
          </a-col>
        </a-row>

        <a-row :gutter="[16, 16]" style="margin-top: 12px">
          <a-col :xs="24">
            <div class="section-title">APK 安装配置（将在 setup_adb_commands 之前安装）</div>
            <div class="kv-list">
              <div v-for="(apk, idx) in createForm.apks" :key="idx" class="apk-row">
                <a-row :gutter="8" style="width: 100%">
                  <a-col :xs="24" :md="6">
                    <a-input v-model:value="apk.name" placeholder="名称（可选）" />
                  </a-col>
                  <a-col :xs="24" :md="6">
                    <a-input v-model:value="apk.package_name" placeholder="包名 *" />
                  </a-col>
                  <a-col :xs="24" :md="4">
                    <a-input v-model:value="apk.version" placeholder="版本 *" />
                  </a-col>
                  <a-col :xs="24" :md="4">
                    <a-select v-model:value="apk.type" style="width: 100%">
                      <a-select-option value="remote">remote</a-select-option>
                      <a-select-option value="local">local</a-select-option>
                    </a-select>
                  </a-col>
                  <a-col :xs="24" :md="4">
                    <a-input
                      v-model:value="apk.url"
                      :placeholder="apk.type === 'remote' ? 'URL *' : 'URL（local 时为空）'"
                    />
                  </a-col>
                </a-row>
                <a-button
                  type="text"
                  danger
                  :disabled="createForm.apks.length === 1"
                  @click="removeRow(createForm.apks, idx)"
                >
                  删除
                </a-button>
              </div>
              <a-button
                type="dashed"
                block
                @click="addRow(createForm.apks, { name: '', package_name: '', version: '', url: '', type: 'remote' })"
              >
                添加 APK
              </a-button>
            </div>
          </a-col>
        </a-row>
      </a-form>
    </a-modal>
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

.section-title {
  font-weight: 600;
  margin: 0 0 @space-sm;
}

.kv-list {
  display: flex;
  flex-direction: column;
  gap: @space-sm;
}

.kv-row {
  display: flex;
  align-items: center;
  gap: @space-sm;

  @media (max-width: 768px) {
    flex-direction: column;
    align-items: flex-start;
  }
}

.apk-row {
  display: flex;
  align-items: flex-start;
  gap: @space-sm;
  padding: @space-sm;
  border: 1px solid #d9d9d9;
  border-radius: 4px;
  margin-bottom: @space-sm;
}
</style>

