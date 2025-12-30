<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { setupAdmin } from '../../api/auth'
import { useAuthStore } from '../../stores/auth'

const router = useRouter()
const auth = useAuthStore()
const loading = ref(false)
const form = ref({ name: 'admin', email: '', password: '' })

const handleSubmit = async () => {
  loading.value = true
  try {
    const { data } = await setupAdmin(form.value)
    // 标记已完成初始化，避免守卫反复跳回 setup-admin
    auth.setStatus({ admin_initialized: true, allow_registration: true })
    // 不自动登录，清空 token；如需自动登录可改为 setToken(data?.token)
    auth.logout()
    message.success('管理员已初始化，请登录')
    router.push('/auth/login')
  } catch (err) {
    message.error(err.message || '初始化失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="auth-page">
    <a-card class="auth-card" bordered>
      <h2>初始化管理员</h2>
      <a-form :model="form" layout="vertical" @finish="handleSubmit">
        <a-form-item name="name" label="管理员用户名" required>
          <a-input v-model:value="form.name" disabled />
        </a-form-item>
        <a-form-item name="email" label="管理员邮箱" required>
          <a-input v-model:value="form.email" type="email" />
        </a-form-item>
        <a-form-item name="password" label="管理员密码" required>
          <a-input-password v-model:value="form.password" />
        </a-form-item>
        <a-button type="primary" html-type="submit" block :loading="loading">初始化</a-button>
        <div class="links">
          <RouterLink to="/auth/login">返回登录</RouterLink>
        </div>
      </a-form>
    </a-card>
  </div>
</template>

<style scoped lang="less">
@import "../../styles/variables.less";

.auth-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  background: @color-surface;
}

.auth-card {
  width: 360px;
  padding: @space-lg;
  box-shadow: @shadow-soft;
  border-radius: @radius-md;
}

.auth-card h2 {
  margin: 0 0 @space-md;
  font-size: @font-size-h2;
}

.links {
  margin-top: @space-md;
  display: flex;
  justify-content: space-between;
}
</style>

