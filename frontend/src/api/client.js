import axios from 'axios'

// 默认使用相对路径，结合 Vite dev 代理以避免本地 CORS；如需自定义请设置 VITE_API_BASE
const baseURL = import.meta.env.VITE_API_BASE || '/api/v1'

// 默认超时时间（30分钟，1800000ms），如果从后端获取到配置则使用配置值
let defaultTimeout = 1800000

// 创建临时 axios 实例用于获取配置
const tempClient = axios.create({
  baseURL,
  timeout: 5000, // 获取配置的超时时间较短
})

// 尝试从后端获取上传配置
const fetchUploadConfig = async () => {
  try {
    const token = localStorage.getItem('sandroidx_token')
    if (token) {
      const response = await tempClient.get('/system/settings', {
        headers: { Authorization: `Bearer ${token}` },
      })
      if (response?.data?.data?.upload_config?.timeout_seconds) {
        defaultTimeout = response.data.data.upload_config.timeout_seconds * 1000 // 转换为毫秒
      }
    }
  } catch (err) {
    // 静默失败，使用默认值
    console.warn('无法获取上传配置，使用默认超时时间:', err)
  }
}

// 立即尝试获取配置（不阻塞）
fetchUploadConfig()

const client = axios.create({
  baseURL,
  timeout: defaultTimeout,
})

// 简单的鉴权拦截，可后续接入真实 token 存储
client.interceptors.request.use((config) => {
  const token = localStorage.getItem('sandroidx_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

client.interceptors.response.use(
  (res) => res,
  (error) => {
    const status = error?.response?.status
    // token 过期/无效：清理本地 token 并跳转登录
    if (status === 401) {
      try {
        localStorage.removeItem('sandroidx_token')
      } catch (e) {
        // ignore
      }
      // 避免在登录/注册页和share页面死循环跳转
      const path = window.location?.pathname || ''
      if (!path.startsWith('/auth/') && !path.startsWith('/share/')) {
        const redirect = encodeURIComponent(
          `${window.location.pathname}${window.location.search || ''}${window.location.hash || ''}`,
        )
        window.location.href = `/auth/login?redirect=${redirect}`
      }
    }
    const message =
      error?.response?.data?.error ||
      error?.message ||
      '请求失败，请稍后重试'
    return Promise.reject(new Error(message))
  },
)

export default client

