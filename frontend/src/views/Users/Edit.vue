<script setup>
import { onMounted, ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { getUser, createUser, updateUser } from '../../api/users'

const route = useRoute()
const router = useRouter()
const userId = computed(() => route.params.id)

const loading = ref(false)
const form = ref({
  name: '',
  email: '',
  password: '',
  role: 'user',
})

const fetchDetail = async () => {
  if (!userId.value) return
  loading.value = true
  try {
    const { data } = await getUser(userId.value)
    form.value = {
      name: data.name,
      email: data.email,
      password: '',
      role: data.role || 'user',
    }
  } catch (err) {
    message.error(err.message || '加载失败')
  } finally {
    loading.value = false
  }
}

const handleSubmit = async () => {
  loading.value = true
  try {
    const payload = { ...form.value }
    if (userId.value) {
      await updateUser(userId.value, payload)
      message.success('已更新')
    } else {
      await createUser(payload)
      message.success('已创建')
    }
    router.push('/users')
  } catch (err) {
    message.error(err.message || '保存失败')
  } finally {
    loading.value = false
  }
}

onMounted(fetchDetail)
</script>

<template>
  <div class="page-container edit-page">
    <div class="page-title">
      <h1>{{ userId ? '编辑用户' : '创建用户' }}</h1>
    </div>

    <a-card :loading="loading" bordered>
      <a-form layout="vertical" @finish="handleSubmit">
        <a-form-item label="姓名" required>
          <a-input v-model:value="form.name" />
        </a-form-item>
        <a-form-item label="邮箱" required>
          <a-input v-model:value="form.email" type="email" />
        </a-form-item>
        <a-form-item label="密码" :required="!userId">
          <a-input-password v-model:value="form.password" placeholder="留空则不修改" />
        </a-form-item>
        <a-form-item label="角色">
          <a-select v-model:value="form.role">
            <a-select-option value="admin">admin</a-select-option>
            <a-select-option value="user">user</a-select-option>
          </a-select>
        </a-form-item>
        <a-space>
          <a-button type="primary" html-type="submit" :loading="loading">保存</a-button>
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

