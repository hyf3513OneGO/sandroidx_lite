<script setup>
import { onMounted, ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { getTemplate, createTemplate, updateTemplate, deleteTemplate } from '../../api/templates'

const route = useRoute()
const router = useRouter()
const templateId = computed(() => route.params.id)

const loading = ref(false)
const form = ref({
  name: '',
  description: '',
  content: '{\n  "agent": {},\n  "sandbox": {}\n}',
})

const fetchDetail = async () => {
  if (!templateId.value) return
  loading.value = true
  try {
    const { data } = await getTemplate(templateId.value)
    form.value = {
      name: data.name,
      description: data.description,
      content: JSON.stringify(data.content || {}, null, 2),
    }
  } catch (err) {
    message.error(err.message || '加载失败')
  } finally {
    loading.value = false
  }
}

const handleSubmit = async () => {
  // 先验证必填字段
  if (!form.value.name || !form.value.name.trim()) {
    message.error('请输入模板名称')
    return
  }
  
  if (!form.value.content || !form.value.content.trim()) {
    message.error('请输入模板内容')
    return
  }

  loading.value = true
  try {
    // 验证 JSON 格式
    let parsedContent
    try {
      parsedContent = JSON.parse(form.value.content)
    } catch (parseErr) {
      message.error(`JSON 格式错误: ${parseErr.message}`)
      loading.value = false
      return
    }

    const payload = {
      name: form.value.name.trim(),
      description: form.value.description,
      content: parsedContent,
    }
    
    console.log('提交模板数据:', payload)
    
    if (templateId.value) {
      const response = await updateTemplate(templateId.value, payload)
      console.log('更新成功:', response)
      message.success('已更新')
    } else {
      const response = await createTemplate(payload)
      console.log('创建成功:', response)
      message.success('已创建')
    }
    router.push('/templates')
  } catch (err) {
    console.error('保存失败:', err)
    message.error(err.message || '保存失败')
  } finally {
    loading.value = false
  }
}

const handleDelete = async () => {
  if (!templateId.value) return
  try {
    await deleteTemplate(templateId.value)
    message.success('已删除')
    router.push('/templates')
  } catch (err) {
    message.error(err.message || '删除失败')
  }
}

onMounted(fetchDetail)
</script>

<template>
  <div class="page-container edit-page">
    <div class="page-title">
      <h1>{{ templateId ? '编辑模板' : '新建模板' }}</h1>
      <p class="muted">支持直接编辑 JSON 内容。</p>
    </div>

    <a-card :loading="loading" bordered>
      <a-form layout="vertical">
        <a-form-item label="名称" required>
          <a-input v-model:value="form.name" placeholder="请输入模板名称" />
        </a-form-item>
        <a-form-item label="描述">
          <a-input v-model:value="form.description" placeholder="可选描述" />
        </a-form-item>
        <a-form-item label="内容（JSON）" required>
          <a-textarea
            v-model:value="form.content"
            :auto-size="{ minRows: 12, maxRows: 24 }"
            spellcheck="false"
          />
        </a-form-item>
        <a-space>
          <a-button type="primary" :loading="loading" @click="handleSubmit">保存</a-button>
          <a-button v-if="templateId" danger type="text" @click="handleDelete">删除</a-button>
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

