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
</script>

<template>
  <a-layout class="app-shell">
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
</style>
