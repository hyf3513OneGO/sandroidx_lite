import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import HomeView from '../views/Home.vue'
import AgentCreate from '../views/Agents/Create.vue'
import AgentDetail from '../views/Agents/Detail.vue'
import AgentPublicShare from '../views/Agents/PublicShare.vue'
import AgentList from '../views/Agents/List.vue'
import SandboxList from '../views/Sandboxes/List.vue'
import SandboxDetail from '../views/Sandboxes/Detail.vue'
import TemplateList from '../views/Templates/List.vue'
import TemplateEdit from '../views/Templates/Edit.vue'
import ApkList from '../views/Apks/List.vue'
import ApkEdit from '../views/Apks/Edit.vue'
import UserList from '../views/Users/List.vue'
import UserEdit from '../views/Users/Edit.vue'
import Login from '../views/Auth/Login.vue'
import Register from '../views/Auth/Register.vue'
import SetupAdmin from '../views/Auth/SetupAdmin.vue'
import { status as fetchStatus } from '../api/auth'

const routes = [
  {
    path: '/',
    name: 'home',
    component: HomeView,
    meta: { requiresAuth: true },
  },
  {
    path: '/agents/create',
    name: 'agent-create',
    component: AgentCreate,
    meta: { requiresAuth: true },
  },
  {
    path: '/agents',
    name: 'agent-list',
    component: AgentList,
    meta: { requiresAuth: true },
  },
  {
    path: '/agents/:id',
    name: 'agent-detail',
    component: AgentDetail,
    meta: { requiresAuth: true },
  },
  {
    path: '/share/agents/:token',
    name: 'agent-public-share',
    component: AgentPublicShare,
  },
  {
    path: '/sandboxes',
    name: 'sandbox-list',
    component: SandboxList,
    meta: { requiresAuth: true },
  },
  {
    path: '/sandboxes/:id',
    name: 'sandbox-detail',
    component: SandboxDetail,
    meta: { requiresAuth: true },
  },
  {
    path: '/templates',
    name: 'template-list',
    component: TemplateList,
    meta: { requiresAuth: true },
  },
  {
    path: '/apks',
    name: 'apk-list',
    component: ApkList,
    meta: { requiresAuth: true },
  },
  {
    path: '/templates/create',
    name: 'template-create',
    component: TemplateEdit,
    meta: { requiresAuth: true },
  },
  {
    path: '/apks/create',
    name: 'apk-create',
    component: ApkEdit,
    meta: { requiresAuth: true },
  },
  {
    path: '/templates/:id',
    name: 'template-edit',
    component: TemplateEdit,
    meta: { requiresAuth: true },
  },
  {
    path: '/apks/:id',
    name: 'apk-edit',
    component: ApkEdit,
    meta: { requiresAuth: true },
  },
  {
    path: '/users',
    name: 'user-list',
    component: UserList,
    meta: { requiresAuth: true },
  },
  {
    path: '/users/create',
    name: 'user-create',
    component: UserEdit,
    meta: { requiresAuth: true },
  },
  {
    path: '/users/:id',
    name: 'user-edit',
    component: UserEdit,
    meta: { requiresAuth: true },
  },
  {
    path: '/auth/login',
    name: 'login',
    component: Login,
  },
  {
    path: '/auth/register',
    name: 'register',
    component: Register,
  },
  {
    path: '/auth/setup-admin',
    name: 'setup-admin',
    component: SetupAdmin,
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior() {
    return { top: 0 }
  },
})

const whiteList = ['/auth/login', '/auth/register', '/auth/setup-admin']

router.beforeEach(async (to, from, next) => {
  const auth = useAuthStore()
  const hasToken = !!auth.token

  // 未登录场景下，优先检查系统状态以判断是否需要初始化管理员
  if (!hasToken && !auth.statusLoaded) {
    try {
      const { data } = await fetchStatus()
      auth.setStatus({
        admin_initialized: data?.admin_initialized,
        allow_registration: data?.allow_registration,
      })
    } catch (e) {
      // 静默，避免阻塞路由
    }
  }

  // 未初始化管理员时强制跳转到 setup-admin（除非正访问该页）
  if (!hasToken && auth.statusLoaded && !auth.adminInitialized && to.path !== '/auth/setup-admin') {
    next('/auth/setup-admin')
    return
  }

  if (hasToken && whiteList.includes(to.path)) {
    next('/')
    return
  }

  if (!hasToken && to.meta.requiresAuth) {
    next({ path: '/auth/login', query: { redirect: to.fullPath } })
    return
  }

  next()
})

export default router

