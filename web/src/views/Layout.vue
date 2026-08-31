<template>
  <div class="shell" :class="{ 'is-mobile': isMobile }">
    <header v-if="isMobile" class="topbar">
      <button class="hamburger" aria-label="menu" @click="drawerOpen = true">
        <span></span><span></span><span></span>
      </button>
      <div class="topbar-brand">
        <span class="prompt">$</span> tokenhub<span class="cursor"></span>
      </div>
    </header>

    <aside class="side" :class="{ open: drawerOpen }">
      <div class="brand">
        <span class="prompt">$</span> tokenhub<span class="cursor"></span>
      </div>

      <nav class="nav" @click="drawerOpen = false">
        <template v-for="group in navGroups" :key="group.label">
          <div v-if="group.items.length" class="nav-group"># {{ group.label }}</div>
          <RouterLink
            v-for="item in group.items"
            :key="item.path"
            :to="item.path"
            class="nav-item"
            :class="{ active: $route.path === item.path }"
          >
            <span class="prefix">{{ $route.path === item.path ? '$' : '>' }}</span>{{ item.title }}
          </RouterLink>
        </template>
      </nav>

      <div class="side-footer">
        <el-dropdown @command="onCmd" style="width:100%">
          <div class="user-chip">
            <span class="username">{{ user ? user.username : '' }}@tokenhub</span>
            <span class="role-tag">[{{ user?.role === 'admin' ? 'admin' : 'user' }}]</span>
          </div>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="pwd">修改密码</el-dropdown-item>
              <el-dropdown-item command="logout" divided>退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </aside>

    <div v-if="isMobile && drawerOpen" class="drawer-mask" @click="drawerOpen = false" />

    <main class="main">
      <div class="content">
        <RouterView />
      </div>
    </main>
  </div>

  <el-dialog v-model="pwdVisible" title="passwd --change" width="400px">
    <el-form label-width="80px">
      <el-form-item label="旧密码"><el-input v-model="pwd.old_password" type="password" show-password /></el-form-item>
      <el-form-item label="新密码"><el-input v-model="pwd.new_password" type="password" show-password /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="pwdVisible = false">取消</el-button>
      <el-button type="primary" @click="changePwd">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { RouterLink, RouterView, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { get, post, clearToken } from '../api'

const user = ref(null)
const router = useRouter()
const pwdVisible = ref(false)
const pwd = ref({ old_password: '', new_password: '' })
const isMobile = ref(false)
const drawerOpen = ref(false)

function syncViewport() {
  isMobile.value = window.innerWidth <= 768
  if (!isMobile.value) drawerOpen.value = false
}

const navGroups = computed(() => {
  const admin = user.value?.role === 'admin'
  return [
    { label: 'overview', items: [
      { path: '/', title: '用量统计' },
      { path: '/models', title: '模型列表' }
    ]},
    { label: 'account', items: [
      { path: '/logs', title: '请求日志' },
      { path: '/keys', title: 'API Key' }
    ]},
    { label: 'admin', items: admin ? [
      { path: '/providers', title: '供应商' },
      { path: '/users', title: '用户' }
    ] : [] }
  ]
})

onMounted(async () => {
  try { user.value = await get('/api/me') } catch {}
  syncViewport()
  window.addEventListener('resize', syncViewport)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', syncViewport)
})

async function changePwd() {
  try {
    await post('/api/me/password', pwd.value)
    ElMessage.success('修改成功')
    pwdVisible.value = false
  } catch (e) { ElMessage.error(e.message) }
}

function onCmd(cmd) {
  if (cmd === 'logout') { clearToken(); router.push('/login') }
  if (cmd === 'pwd') pwdVisible.value = true
}
</script>

<style scoped>
.shell { display: flex; height: 100vh; }

.side {
  width: 212px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: #080b0d;
  border-right: 1px solid var(--th-border);
}
.brand {
  padding: 18px 16px 14px;
  font-size: 14px;
  font-weight: 600;
  color: var(--th-fg);
  border-bottom: 1px solid var(--th-border);
}
.prompt { color: var(--th-green); margin-right: 2px; }

.nav { flex: 1; padding: 8px 10px; overflow-y: auto; }
.nav-group {
  font-size: 10.5px; color: var(--th-fg-dim);
  padding: 14px 8px 5px; letter-spacing: 0.06em;
}
.nav-item {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 8px; margin: 1px 0;
  border-radius: 3px;
  color: var(--el-text-color-regular);
  text-decoration: none;
  font-size: 13px;
  transition: background 0.1s, color 0.1s;
}
.nav-item:hover { background: #0f1519; color: var(--th-fg); }
.nav-item.active {
  background: rgba(74, 222, 128, 0.09);
  color: var(--th-green);
}
.prefix { width: 12px; opacity: 0.8; }

.side-footer { padding: 10px; border-top: 1px solid var(--th-border); }
.user-chip {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 8px; border-radius: 3px; cursor: pointer;
  transition: background 0.1s;
}
.user-chip:hover { background: #0f1519; }
.username { flex: 1; font-size: 12.5px; color: var(--th-fg); }
.role-tag { font-size: 11px; color: var(--th-fg-dim); }

.main { flex: 1; overflow-y: auto; padding: 24px 28px; }
.content { max-width: 1200px; margin: 0 auto; }

/* ---- 移动端：侧边栏改为抽屉 ---- */
.topbar {
  display: none;
  align-items: center;
  gap: 12px;
  height: 48px;
  padding: 0 12px;
  background: #080b0d;
  border-bottom: 1px solid var(--th-border);
  position: sticky;
  top: 0;
  z-index: 20;
}
.hamburger {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  width: 36px; height: 36px;
  padding: 8px;
  background: transparent;
  border: 1px solid var(--th-border);
  border-radius: 3px;
  cursor: pointer;
}
.hamburger span {
  display: block;
  height: 2px;
  background: var(--th-fg);
  border-radius: 1px;
}
.topbar-brand {
  font-size: 13px;
  font-weight: 600;
  color: var(--th-fg);
}

.shell.is-mobile { flex-direction: column; height: 100vh; }
.shell.is-mobile .main { padding: 14px 14px; }
.shell.is-mobile .side {
  position: fixed;
  top: 0; left: 0;
  height: 100vh;
  z-index: 30;
  width: 240px;
  transform: translateX(-100%);
  transition: transform 0.2s ease;
}
.shell.is-mobile .side.open { transform: translateX(0); }
.drawer-mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  z-index: 25;
}
@media (max-width: 768px) {
  .topbar { display: flex; }
}
</style>
