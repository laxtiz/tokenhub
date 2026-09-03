import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import './theme.css'
import App from './App.vue'
import { getToken } from './api'

document.documentElement.classList.add('dark')

import Login from './views/Login.vue'
import Layout from './views/Layout.vue'
import Dashboard from './views/Dashboard.vue'
import ModelsView from './views/Models.vue'
import LogsView from './views/Logs.vue'
import ProvidersView from './views/Providers.vue'
import UsersView from './views/Users.vue'
import KeysView from './views/Keys.vue'
import AdminStats from './views/AdminStats.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: Login },
    {
      path: '/', component: Layout,
      children: [
        { path: '', component: Dashboard },
        { path: 'models', component: ModelsView },
        { path: 'logs', component: LogsView },
        { path: 'providers', component: ProvidersView, meta: { admin: true } },
        { path: 'users', component: UsersView, meta: { admin: true } },
        { path: 'admin/stats', component: AdminStats, meta: { admin: true } },
        { path: 'keys', component: KeysView }
      ]
    }
  ]
})

router.beforeEach((to) => {
  if (to.path !== '/login' && !getToken()) return '/login'
})

createApp(App).use(router).use(ElementPlus).mount('#app')
