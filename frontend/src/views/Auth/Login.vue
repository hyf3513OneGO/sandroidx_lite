<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { login } from '../../api/auth'
import { useAuthStore } from '../../stores/auth'

const router = useRouter()
const auth = useAuthStore()
const loading = ref(false)
const form = ref({ email: '', password: '' })

const handleSubmit = async () => {
  loading.value = true
  try {
    const { data } = await login(form.value)
    auth.setToken(data?.token || '')
    auth.setUser(data?.user || null)
    message.success('登录成功')
    router.push('/')
  } catch (err) {
    message.error(err.message || '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="auth-page">
    <a-card class="auth-card" bordered>
      <h2>登录</h2>
      <a-form :model="form" layout="vertical" @finish="handleSubmit">
        <a-form-item name="email" label="邮箱" required>
          <a-input v-model:value="form.email" type="email" />
        </a-form-item>
        <a-form-item name="password" label="密码" required>
          <a-input-password v-model:value="form.password" />
        </a-form-item>
        <a-button type="primary" html-type="submit" block :loading="loading">登录</a-button>
        <div class="links">
          <RouterLink to="/auth/register">注册</RouterLink>
          <RouterLink to="/auth/setup-admin">初始化管理员</RouterLink>
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

