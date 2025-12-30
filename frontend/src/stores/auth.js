import { defineStore } from 'pinia'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('sandroidx_token') || '',
    user: null,
    statusLoaded: false,
    adminInitialized: true,
    allowRegistration: true,
  }),
  actions: {
    setToken(token) {
      this.token = token
      if (token) {
        localStorage.setItem('sandroidx_token', token)
      } else {
        localStorage.removeItem('sandroidx_token')
      }
    },
    setUser(user) {
      this.user = user
    },
    logout() {
      this.setToken('')
      this.setUser(null)
    },
    setStatus(status) {
      this.statusLoaded = true
      this.adminInitialized = !!status?.admin_initialized
      this.allowRegistration = status?.allow_registration !== false
    },
  },
})

