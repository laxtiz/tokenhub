<template>
  <div class="shell">
    <aside class="side">
      <div class="brand">
        <span class="prompt">$</span> tokenhub<span class="cursor"></span>
      </div>

      <nav class="nav">
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
import { ref, computed, onMounted } from 'vue'
import { RouterLink, RouterView, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { get, post, clearToken } from '../api'

const user = ref(null)
const router = useRouter()
const pwdVisible = ref(false)
const pwd = ref({ old_password: '', new_password: '' })

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
</style>
