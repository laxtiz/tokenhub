<template>
  <div class="login-wrap">
    <div class="term">
      <div class="term-bar">
        <span class="dot red"></span><span class="dot yellow"></span><span class="dot green"></span>
        <span class="term-title">tokenhub — login — 80×24</span>
      </div>
      <div class="term-body">
        <p class="line dim"># 使用管理台账号登录</p>
        <form @submit.prevent="doLogin">
          <div class="cmd-line">
            <span class="green">$</span>tokenhub auth login <span class="cont">\</span>
          </div>
          <div class="cmd-line indent">
            <span class="flag">--username</span>
            <el-input v-model="username" />
            <span class="cont">\</span>
          </div>
          <div class="cmd-line indent">
            <span class="flag">--password</span>
            <el-input v-model="password" type="password" show-password />
          </div>
          <button type="submit" class="verify-line" :disabled="loading">
            <span class="green">$</span>tokenhub auth verify<span class="cursor"></span>
            <span v-if="loading" class="dim">&nbsp;running...</span>
          </button>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { post, setToken } from '../api'

const username = ref('')
const password = ref('')
const loading = ref(false)
const router = useRouter()

async function doLogin() {
  if (!username.value || !password.value) return
  loading.value = true
  try {
    const data = await post('/api/login', { username: username.value, password: password.value })
    setToken(data.token)
    router.push('/')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-wrap {
  display: flex; align-items: center; justify-content: center;
  height: 100vh;
  background: var(--th-bg);
}
.term {
  width: 520px;
  border: 1px solid var(--th-border);
  border-radius: 8px;
  background: var(--th-panel);
  overflow: hidden;
  box-shadow: 0 0 40px rgba(74, 222, 128, 0.06);
}
.term-bar {
  display: flex; align-items: center; gap: 7px;
  padding: 9px 12px;
  background: #0d1216;
  border-bottom: 1px solid var(--th-border);
}
.dot { width: 10px; height: 10px; border-radius: 50%; }
.dot.red { background: #f87171; }
.dot.yellow { background: #fbbf24; }
.dot.green { background: #4ade80; }
.term-title {
  margin-left: 8px;
  font-size: 11px; color: var(--th-fg-dim);
}
.term-body { padding: 20px 24px 24px; }
.line { font-size: 13px; margin: 0 0 16px; }
.dim { color: var(--th-fg-dim); }
.green { color: var(--th-green); }

.cmd-line {
  display: flex; align-items: center; gap: 8px;
  padding: 5px 0;
  font-size: 13px;
}
.cmd-line.indent { padding-left: 2ch; }
.flag { color: var(--th-green); flex-shrink: 0; }
.cont { color: var(--th-fg-dim); flex-shrink: 0; }

/* 输入框做成终端值的样子：透明背景 + 下划线 */
.cmd-line :deep(.el-input__wrapper) {
  background: transparent;
  box-shadow: none !important;
  border-bottom: 1px solid var(--th-border);
  border-radius: 0;
  padding: 0 2px;
  flex: 1;
}
.cmd-line :deep(.el-input__wrapper.is-focus) {
  border-bottom-color: var(--th-green-dim);
  box-shadow: none !important;
}
.cmd-line :deep(.el-input__inner) {
  height: 24px; line-height: 24px;
  color: var(--th-fg);
  caret-color: var(--th-green);
}

/* verify 提交：一行终端命令文本，可点击 */
.verify-line {
  display: inline-flex; align-items: center; gap: 8px;
  margin-top: 14px;
  padding: 2px 4px;
  background: none;
  border: none;
  color: var(--th-fg);
  font-family: inherit;
  font-size: 13px;
  cursor: pointer;
  border-radius: 3px;
  transition: background 0.12s;
}
.verify-line:hover { background: rgba(74, 222, 128, 0.08); }
.verify-line:disabled { opacity: 0.6; cursor: wait; }
</style>
