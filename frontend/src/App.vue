<script setup>
import { computed, h, ref } from 'vue'
import { useRoute, useRouter, RouterView } from 'vue-router'
import {
  DashboardOutlined,
  AppstoreOutlined,
  BuildOutlined,
  DatabaseOutlined,
  TeamOutlined,
  SettingOutlined,
} from '@ant-design/icons-vue'

const router = useRouter()
const route = useRoute()
const collapsed = ref(false)

const menuItems = [
  { key: '/', icon: DashboardOutlined, label: '总览' },
  { key: '/agents', icon: AppstoreOutlined, label: 'Agents' },
  { key: '/sandboxes', icon: BuildOutlined, label: 'Sandboxes' },
  { key: '/templates', icon: DatabaseOutlined, label: 'Templates' },
  { key: '/apks', icon: DatabaseOutlined, label: 'APKs' },
  { key: '/users', icon: TeamOutlined, label: 'Users' },
]

const selectedKeys = computed(() => {
  const path = route.path
  if (path.startsWith('/agents')) {
    return ['/agents']
  }
  if (path.startsWith('/sandboxes')) return ['/sandboxes']
  if (path.startsWith('/templates')) return ['/templates']
  if (path.startsWith('/apks')) return ['/apks']
  if (path.startsWith('/users')) return ['/users']
  return ['/']
})

const onMenuClick = ({ key }) => {
  router.push(key)
}

// 判断是否为share页面（公开页面，不需要侧边栏和头部）
const isSharePage = computed(() => {
  return route.path.startsWith('/share/')
})
</script>

<template>
  <!-- Share页面：不显示侧边栏和头部 -->
  <RouterView v-if="isSharePage" />
  
  <!-- 其他页面：显示完整布局 -->
  <a-layout v-else class="app-shell">
    <a-layout-sider
      class="app-sider"
      collapsible
      v-model:collapsed="collapsed"
      :width="220"
      :collapsed-width="72"
      theme="dark"
    >
      <div class="brand">
        <span class="brand-mark" />
        <span v-if="!collapsed" class="brand-name">SAndroidX Lite</span>
      </div>
      <a-menu
        mode="inline"
        theme="dark"
        :selected-keys="selectedKeys"
        :items="menuItems.map((item) => ({ ...item, icon: () => h(item.icon) }))"
        @click="onMenuClick"
      />
    </a-layout-sider>

    <a-layout>
      <a-layout-header class="app-header">
        <div class="header-actions">
          <a-button type="text" shape="circle" :icon="h(SettingOutlined)" />
        </div>
      </a-layout-header>

      <a-layout-content class="app-content">
        <RouterView />
      </a-layout-content>
    </a-layout>
  </a-layout>
  
  <!-- 版权角标 - 在所有页面显示 -->
  <a href="https://github.com/hyf3513OneGO/sandroidx_lite" target="_blank" class="copyright-badge">
    <span>© SAndroidX Lite</span>
  </a>
</template>

<style scoped lang="less">
@import "./styles/variables.less";

.app-shell {
  min-height: 100vh;
  background: var(--color-surface);
}

.app-sider {
  background: @color-primary-text;
}

.brand {
  height: 64px;
  display: flex;
  align-items: center;
  gap: @space-sm;
  padding: 0 @space-md;
  color: #fff;
  font-weight: @font-weight-medium;
}

.brand-mark {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: @color-accent;
  display: inline-block;
}

.brand-name {
  letter-spacing: 0.4px;
  white-space: nowrap;
}

.app-header {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding: 0 @space-xl;
  background: var(--color-surface);
  box-shadow: var(--shadow-soft);
}

.header-actions :deep(.ant-btn) {
  color: var(--color-primary-text);
}

.app-content {
  padding: @space-xl 0 @space-xxl;
}

.copyright-badge {
  position: fixed;
  bottom: 16px;
  right: 16px;
  padding: 6px 12px;
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  font-size: 12px;
  text-decoration: none;
  border-radius: 4px;
  transition: all 0.3s ease;
  z-index: 1000;
  backdrop-filter: blur(4px);
  
  &:hover {
    background: rgba(0, 0, 0, 0.8);
    color: #fff;
    transform: translateY(-2px);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  }
  
  span {
    display: inline-block;
  }
}
</style>
