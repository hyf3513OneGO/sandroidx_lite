<script setup>
import { onMounted, ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { getApk, createApk, updateApk, deleteApk, uploadApk, downloadApk, uploadApkPreview, downloadApkPreview } from '../../api/apks'

const route = useRoute()
const router = useRouter()
const apkId = computed(() => route.params.id)

const loading = ref(false)
const uploading = ref(false)
const downloading = ref(false)
const uploadProgress = ref(0)
const previewTempPath = ref(null) // 预览后保存的临时文件路径
const form = ref({
  name: '',
  package_name: '',
  version: '',
  type: 'local',
  url: '',
  path: '',
  description: '',
})

const fileRef = ref(null)

const onFileChange = (e) => {
  const f = e?.target?.files?.[0] || null
  fileRef.value = f
  // 文件改变时清除之前的预览临时路径
  previewTempPath.value = null
}

const fetchDetail = async () => {
  if (!apkId.value) {
    // 新建模式：检查查询参数，预填充表单
    const query = route.query
    if (query.package_name) {
      form.value.package_name = query.package_name
    }
    if (query.name) {
      form.value.name = query.name
    }
    // 清除之前的预览临时路径
    previewTempPath.value = null
    return
  }
  
  loading.value = true
  try {
    const { data } = await getApk(apkId.value)
    form.value = {
      name: data.name || '',
      package_name: data.package_name || '',
      version: data.version || '',
      type: data.type || 'local',
      url: data.url || '',
      path: data.path || '',
      description: data.description || '',
    }
  } catch (err) {
    message.error(err.message || '加载失败')
  } finally {
    loading.value = false
  }
}

const handleSubmit = async () => {
  if (!form.value.name || !form.value.name.trim()) {
    message.error('请输入名称')
    return
  }
  // 包名和版本现在是可选的（后端会自动解析）
  if (!form.value.type) {
    message.error('请选择类型')
    return
  }
  if (form.value.type === 'remote') {
    if (!form.value.url || !form.value.url.trim()) {
      message.error('请输入 URL')
      return
    }
  } else if (form.value.type === 'local') {
    if (!apkId.value && !fileRef.value) {
      message.error('请选择要上传的 APK 文件')
      return
    }
  }

  loading.value = true
  try {
    if (form.value.type === 'local') {
      if (apkId.value) {
        // 仅允许改元信息（文件替换不做在这里，避免误操作）
        await updateApk(apkId.value, {
          name: form.value.name.trim(),
          package_name: form.value.package_name.trim(),
          version: form.value.version.trim(),
          type: 'local',
          description: form.value.description,
        })
        message.success('已更新')
      } else {
        // 如果已有预览临时文件，使用临时文件创建（避免重复上传）
        if (previewTempPath.value) {
          const fd = new FormData()
          fd.append('temp_path', previewTempPath.value)
          fd.append('name', form.value.name.trim())
          fd.append('package_name', form.value.package_name.trim())
          fd.append('version', form.value.version.trim())
          if (form.value.description) fd.append('description', form.value.description)
          await uploadApk(fd)
          previewTempPath.value = null // 清除临时路径
          message.success('已创建')
        } else {
          // 没有预览，直接上传
          const fd = new FormData()
          fd.append('file', fileRef.value)
          fd.append('name', form.value.name.trim())
          fd.append('package_name', form.value.package_name.trim())
          fd.append('version', form.value.version.trim())
          if (form.value.description) fd.append('description', form.value.description)
          await uploadApk(fd)
          message.success('已上传并创建')
        }
      }
    } else {
      const payload = {
        name: form.value.name.trim(),
        package_name: form.value.package_name.trim(),
        version: form.value.version.trim(),
        type: 'remote',
        url: form.value.url.trim(),
        description: form.value.description,
      }
      if (apkId.value) {
        await updateApk(apkId.value, payload)
        message.success('已更新')
      } else {
        await createApk(payload)
        message.success('已创建')
      }
    }
    router.push('/apks')
  } catch (err) {
    message.error(err.message || '保存失败')
  } finally {
    loading.value = false
  }
}

const handleDownload = async () => {
  if (!apkId.value) return
  loading.value = true
  try {
    await downloadApk(apkId.value)
    message.success('下载成功')
    await fetchDetail()
  } catch (err) {
    message.error(err.message || '下载失败')
  } finally {
    loading.value = false
  }
}

const handleDelete = async () => {
  if (!apkId.value) return
  try {
    await deleteApk(apkId.value)
    message.success('已删除')
    router.push('/apks')
  } catch (err) {
    message.error(err.message || '删除失败')
  }
}

const handleUploadPreview = async () => {
  if (!fileRef.value) {
    message.error('请先选择文件')
    return
  }

  uploading.value = true
  uploadProgress.value = 0
  try {
    const fd = new FormData()
    fd.append('file', fileRef.value)
    const { data } = await uploadApkPreview(fd, (percent) => {
      uploadProgress.value = percent
    })
    
    // 填充解析结果到表单
    if (data.package_name) {
      form.value.package_name = data.package_name
    }
    if (data.version) {
      form.value.version = data.version
    }
    // 保存临时文件路径，用于后续创建时避免重复上传
    if (data.temp_path) {
      previewTempPath.value = data.temp_path
    }
    
    message.success('上传并解析成功')
  } catch (err) {
    message.error(err.message || '上传失败')
  } finally {
    uploading.value = false
    uploadProgress.value = 0
  }
}

const handleDownloadPreview = async () => {
  if (!form.value.url || !form.value.url.trim()) {
    message.error('请先输入 URL')
    return
  }

  downloading.value = true
  try {
    const { data } = await downloadApkPreview(form.value.url.trim())
    
    // 填充解析结果到表单
    if (data.package_name) {
      form.value.package_name = data.package_name
    }
    if (data.version) {
      form.value.version = data.version
    }
    
    message.success('下载并解析成功')
  } catch (err) {
    message.error(err.message || '下载失败')
  } finally {
    downloading.value = false
  }
}

onMounted(fetchDetail)
</script>

<template>
  <div class="page-container edit-page">
    <div class="page-title">
      <h1>{{ apkId ? '编辑 APK' : '新建 APK' }}</h1>
      <p class="muted">
        local 类型：必须上传文件，会自动校验是否为有效 APK<br />
        remote 类型：填写 URL 后保存时会自动下载并校验，校验通过才会创建记录
      </p>
    </div>

    <a-card :loading="loading" bordered>
      <a-form layout="vertical">
        <a-form-item label="名称" required>
          <a-input v-model:value="form.name" placeholder="例如：WeChat / Chrome" />
        </a-form-item>

        <a-form-item label="包名（package_name）">
          <a-input v-model:value="form.package_name" placeholder="可选，会自动解析（如果解析失败请手动填写）" />
        </a-form-item>

        <a-form-item label="版本（version）">
          <a-input v-model:value="form.version" placeholder="可选，会自动解析（如果解析失败请手动填写）" />
        </a-form-item>

        <a-form-item label="类型" required>
          <a-select
            v-model:value="form.type"
            :options="[
              { label: '本地（local）', value: 'local' },
              { label: '远程（remote）', value: 'remote' },
            ]"
          />
        </a-form-item>

        <a-form-item v-if="form.type === 'remote'" label="URL（remote 必填）" required>
          <div style="display: flex; gap: 8px;">
            <a-input v-model:value="form.url" placeholder="https://example.com/app.apk" style="flex: 1;" />
            <a-button
              type="primary"
              :loading="downloading"
              :disabled="!form.url || !form.url.trim()"
              @click="handleDownloadPreview"
            >
              下载并解析
            </a-button>
          </div>
          <div class="muted" style="margin-top: 8px;">输入 URL 后点击下载，会自动解析包名和版本。</div>
        </a-form-item>

        <a-form-item v-if="form.type === 'local' && !apkId" label="上传 APK（local 必须上传）" required>
          <div style="display: flex; gap: 8px; align-items: center;">
            <input
              type="file"
              accept=".apk"
              @change="onFileChange"
              style="flex: 1;"
            />
            <a-button
              type="primary"
              :loading="uploading"
              :disabled="!fileRef"
              @click="handleUploadPreview"
            >
              上传并解析
            </a-button>
          </div>
          <a-progress
            v-if="uploading"
            :percent="uploadProgress"
            :status="uploadProgress === 100 ? 'success' : 'active'"
            style="margin-top: 8px;"
          />
          <div class="muted" style="margin-top: 8px;">选择文件后点击上传，会自动解析包名和版本。</div>
        </a-form-item>

        <a-form-item v-if="apkId" label="本地路径（落盘后自动写入）">
          <a-input v-model:value="form.path" readonly :title="form.path" />
          <div class="muted" style="margin-top: 4px; font-size: 12px;">
            {{ form.path ? form.path.split('/').pop() : '' }}
          </div>
        </a-form-item>

        <a-form-item label="描述">
          <a-input v-model:value="form.description" placeholder="可选描述（比如是否需要下载/如何使用）" />
        </a-form-item>

        <a-space>
          <a-button type="primary" :loading="loading" @click="handleSubmit">保存</a-button>
          <a-button
            v-if="apkId && form.type === 'remote'"
            :loading="loading"
            :disabled="!!form.path"
            @click="handleDownload"
          >
            {{ form.path ? '已下载' : '下载到本地' }}
          </a-button>
          <a-button v-if="apkId" danger type="text" @click="handleDelete">删除</a-button>
        </a-space>
      </a-form>
    </a-card>
  </div>
</template>

<style scoped lang="less">
@import "../../styles/variables.less";

.edit-page {
  padding: @space-xl 0 @space-xxl;
  display: flex;
  flex-direction: column;
  gap: @space-lg;
}

.page-title h1 {
  margin: 0 0 @space-sm;
  font-size: @font-size-h1;
}
</style>


