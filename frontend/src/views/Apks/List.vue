<script setup>
import { onMounted, ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { ArrowLeftOutlined } from '@ant-design/icons-vue'
import { listApks, downloadApk, deleteApk } from '../../api/apks'

const router = useRouter()
const loading = ref(false)
const dataSource = ref([])
const selectedPackage = ref(null)

const normalizeArray = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.data)) return payload.data
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload?.list)) return payload.list
  return []
}

// 按包名分组
const groupedApks = computed(() => {
  const groups = {}
  dataSource.value.forEach((apk) => {
    const pkg = apk.package_name || '未知包名'
    if (!groups[pkg]) {
      groups[pkg] = {
        package_name: pkg,
        name: apk.name,
        icon: apk.icon,
        versions: [],
      }
    }
    // 使用最新的图标和名称
    if (apk.icon && !groups[pkg].icon) {
      groups[pkg].icon = apk.icon
    }
    if (apk.name && (!groups[pkg].name || groups[pkg].name === '未知包名')) {
      groups[pkg].name = apk.name
    }
    groups[pkg].versions.push(apk)
  })
  
  // 对每个包的版本按创建时间倒序排列
  Object.values(groups).forEach((group) => {
    group.versions.sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
  })
  
  return Object.values(groups)
})

// 当前包的版本列表
const currentVersions = computed(() => {
  if (!selectedPackage.value) return []
  const group = groupedApks.value.find((g) => g.package_name === selectedPackage.value)
  return group ? group.versions : []
})

const fetchList = async () => {
  loading.value = true
  try {
    const { data } = await listApks()
    dataSource.value = normalizeArray(data)
  } catch (err) {
    message.error(err.message || '加载失败')
  } finally {
    loading.value = false
  }
}

onMounted(fetchList)

const handleDownload = async (record) => {
  if (!record?.id) return
  if (record.type !== 'remote') return
  loading.value = true
  try {
    await downloadApk(record.id)
    message.success('下载成功')
    await fetchList()
  } catch (err) {
    message.error(err.message || '下载失败')
  } finally {
    loading.value = false
  }
}

const handleDelete = async (record) => {
  if (!record?.id) return
  try {
    await deleteApk(record.id)
    message.success('已删除')
    await fetchList()
    // 如果删除的是当前查看的包的版本，且该包没有其他版本了，返回列表
    if (selectedPackage.value) {
      const group = groupedApks.value.find((g) => g.package_name === selectedPackage.value)
      if (!group || group.versions.length === 0) {
        selectedPackage.value = null
      }
    }
  } catch (err) {
    message.error(err.message || '删除失败')
  }
}

const handleViewPackage = (packageName) => {
  selectedPackage.value = packageName
}

const handleBack = () => {
  selectedPackage.value = null
}

// 获取图标URL
const getIconUrl = (iconPath) => {
  if (!iconPath) return null
  // 如果已经是完整URL，直接返回
  if (iconPath.startsWith('http://') || iconPath.startsWith('https://')) {
    return iconPath
  }
  // 图标路径是相对于 data_path/apks 的，需要提取文件名
  // 例如：/root/2026.01/sandroidx_lite/container/data_volumes/apks/com.android.chromium_icon.png
  // 应该提取：com.android.chromium_icon.png
  const parts = iconPath.split('/')
  const fileName = parts[parts.length - 1]
  if (fileName) {
    return `/api/v1/apks/icon/${encodeURIComponent(fileName)}`
  }
  return null
}

// 简化路径显示（只显示文件名）
const getFileName = (path) => {
  if (!path) return ''
  const parts = path.split('/')
  return parts[parts.length - 1] || path
}

// 简化URL显示（只显示域名和文件名）
const getShortUrl = (url) => {
  if (!url) return ''
  try {
    const urlObj = new URL(url)
    const pathParts = urlObj.pathname.split('/')
    const fileName = pathParts[pathParts.length - 1] || ''
    return `${urlObj.hostname}${fileName ? '/' + fileName : ''}`
  } catch {
    // 如果不是有效URL，截取前50个字符
    return url.length > 50 ? url.substring(0, 50) + '...' : url
  }
}

</script>

<template>
  <div class="page-container list-page">
    <div class="page-title">
      <h1 v-if="!selectedPackage">APK 应用商店</h1>
      <div v-else style="display: flex; align-items: center; gap: 16px;">
        <a-button type="text" @click="handleBack">
          <template #icon>
            <ArrowLeftOutlined />
          </template>
          返回
        </a-button>
        <h1 style="margin: 0;">{{ currentVersions[0]?.name || selectedPackage }}</h1>
      </div>
      <a-button type="primary" @click="router.push('/apks/create')">新建 APK</a-button>
    </div>

    <a-card bordered>
      <!-- 应用列表（按包名分组） -->
      <div v-if="!selectedPackage" class="app-grid">
        <div
          v-for="group in groupedApks"
          :key="group.package_name"
          class="app-card"
          @click="handleViewPackage(group.package_name)"
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
            <div class="app-name">{{ group.name || group.package_name }}</div>
            <div class="app-package">{{ group.package_name }}</div>
            <div class="app-versions">{{ group.versions.length }} 个版本</div>
          </div>
        </div>
      </div>

      <!-- 版本列表 -->
      <div v-else>
        <a-table
          :data-source="currentVersions"
          :loading="loading"
          row-key="id"
          :pagination="false"
          :columns="[
            { title: '版本', dataIndex: 'version', key: 'version', width: 120 },
            { title: '类型', dataIndex: 'type', key: 'type', width: 80 },
            { title: 'URL', dataIndex: 'url', key: 'url' },
            { title: '路径', dataIndex: 'path', key: 'path' },
            { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180 },
            { title: '操作', key: 'action', width: 150 },
          ]"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'type'">
              <a-tag>{{ record.type === 'remote' ? '远程' : '本地' }}</a-tag>
            </template>

            <template v-else-if="column.key === 'url'">
              <a-tooltip v-if="record.url" :title="record.url">
                <span class="text-ellipsis">{{ getShortUrl(record.url) }}</span>
              </a-tooltip>
              <span v-else style="color: #999;">-</span>
            </template>

            <template v-else-if="column.key === 'path'">
              <a-tooltip :title="record.path">
                <span class="text-ellipsis mono">{{ getFileName(record.path) }}</span>
              </a-tooltip>
            </template>

            <template v-else-if="column.key === 'created_at'">
              {{ new Date(record.created_at).toLocaleString() }}
            </template>

            <template v-else-if="column.key === 'action'">
              <a-space>
                <a-button type="link" size="small" @click.stop="router.push(`/apks/${record.id}`)">编辑</a-button>
                <a-button
                  v-if="record.type === 'remote'"
                  type="link"
                  size="small"
                  :disabled="!!record.path"
                  @click.stop="handleDownload(record)"
                >
                  {{ record.path ? '已下载' : '下载' }}
                </a-button>
                <a-button type="link" size="small" danger @click.stop="handleDelete(record)">删除</a-button>
              </a-space>
            </template>
          </template>
        </a-table>
      </div>
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
  margin: 0;
  font-size: @font-size-h1;
}

.app-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: @space-lg;
  padding: @space-md 0;
}

.app-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: @space-lg;
  border: 1px solid #e8e8e8;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.3s;
  background: #fff;

  &:hover {
    border-color: #1890ff;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
    transform: translateY(-2px);
  }
}

.app-icon {
  width: 64px;
  height: 64px;
  margin-bottom: @space-md;
  border-radius: 12px;
  overflow: hidden;
  background: #f0f0f0;
  display: flex;
  align-items: center;
  justify-content: center;

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
}

.app-icon-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 32px;
  font-weight: bold;
  color: #999;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
}

.app-info {
  text-align: center;
  width: 100%;
}

.app-name {
  font-size: 16px;
  font-weight: 500;
  margin-bottom: 4px;
  color: #333;
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

.text-ellipsis {
  display: inline-block;
  max-width: 300px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 12px;
}
</style>
