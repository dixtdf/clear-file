import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  { path: '/', name: 'dashboard', component: () => import('../views/DashboardView.vue'), meta: { title: '首页' } },
  { path: '/files', name: 'files', component: () => import('../views/FilesView.vue'), meta: { title: '文件管理' } },
  { path: '/cleanup', name: 'cleanup', component: () => import('../views/CleanupView.vue'), meta: { title: '文件清理' } },
  { path: '/duplicates', name: 'duplicates', component: () => import('../views/DuplicatesView.vue'), meta: { title: '重复文件' } },
  { path: '/disk', name: 'disk', component: () => import('../views/DiskView.vue'), meta: { title: '空间分析' } }
]

export default createRouter({
  history: createWebHistory(),
  routes
})
