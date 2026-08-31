<template>
  <div>
    <el-card>
      <template #header>
        <div class="card-head-flex">
          <span>供应商管理</span>
          <el-button type="primary" @click="openProvForm(null)">新建供应商</el-button>
        </div>
      </template>
      <el-table :data="providers" stripe>
        <el-table-column prop="name" label="名称" width="160" />
        <el-table-column prop="type" label="协议" width="110">
          <template #default="{ row }">
            <el-tag size="small">{{ row.type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="base_url" label="Base URL" min-width="220" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag size="small" :type="row.disabled ? 'info' : 'success'">{{ row.disabled ? '停用' : '启用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="API Keys" width="140">
          <template #default="{ row }">{{ (row.keys || []).length }} 个</template>
        </el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="openProvForm(row)">编辑</el-button>
            <el-button size="small" type="primary" @click="openKeys(row)">Keys</el-button>
            <el-popconfirm title="确认删除？" @confirm="removeProvider(row)">
              <template #reference><el-button size="small" type="danger">删除</el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
      <div class="card-list">
        <div v-for="row in providers" :key="row.id" class="card-row">
          <div class="row-head">
            <span class="row-title">{{ row.name }}</span>
            <el-tag size="small" :type="row.disabled ? 'info' : 'success'">{{ row.disabled ? '停用' : '启用' }}</el-tag>
          </div>
          <div class="field"><span class="k">协议</span><span class="v"><el-tag size="small">{{ row.type }}</el-tag></span></div>
          <div class="field"><span class="k">Base URL</span><span class="v mono">{{ row.base_url }}</span></div>
          <div class="field" v-if="row.user_agent">
            <span class="k">UA</span><span class="v mono">{{ row.user_agent }}</span>
          </div>
          <div class="field" v-if="row.custom_headers">
            <span class="k">Headers</span><span class="v mono">{{ row.custom_headers }}</span>
          </div>
          <div class="field"><span class="k">API Keys</span><span class="v">{{ (row.keys || []).length }} 个</span></div>
          <div class="row-actions">
            <el-button size="small" @click="openProvForm(row)">编辑</el-button>
            <el-button size="small" type="primary" @click="openKeys(row)">Keys</el-button>
            <el-popconfirm title="确认删除？" @confirm="removeProvider(row)">
              <template #reference><el-button size="small" type="danger">删除</el-button></template>
            </el-popconfirm>
          </div>
        </div>
      </div>
    </el-card>

    <!-- 供应商表单 -->
    <el-dialog v-model="provFormVisible" :title="provForm.id ? '编辑供应商' : '新建供应商'" width="540px">
      <el-form :model="provForm" label-width="120px">
        <el-form-item label="名称"><el-input v-model="provForm.name" /></el-form-item>
        <el-form-item label="协议">
          <el-select v-model="provForm.type" :disabled="!!provForm.id">
            <el-option value="openai" label="OpenAI (chat/completions)" />
            <el-option value="anthropic" label="Anthropic (messages)" />
          </el-select>
        </el-form-item>
        <el-form-item label="Base URL">
          <el-input v-model="provForm.base_url"
            :placeholder="provForm.type === 'anthropic' ? 'https://api.anthropic.com' : 'https://api.openai.com/v1'" />
        </el-form-item>
        <el-form-item label="User-Agent">
          <el-input v-model="provForm.user_agent" placeholder="留空使用 Go 默认 UA" />
        </el-form-item>
        <el-form-item label="Headers">
          <el-input v-model="provForm.custom_headers" type="textarea" :rows="4"
            placeholder='{"X-Trace-Id":"abc","X-Org":"team-a"}' />
        </el-form-item>
        <el-form-item label="停用"><el-switch v-model="provForm.disabled" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="provFormVisible = false">取消</el-button>
        <el-button type="primary" @click="saveProvider">保存</el-button>
      </template>
    </el-dialog>

    <!-- Key 管理 -->
    <el-dialog v-model="keysVisible" :title="`API Keys - ${current?.name}`" width="760px">
      <div style="display:flex;gap:8px;margin-bottom:10px">
        <el-input v-model="newKeyName" placeholder="名称（可选）" style="width:200px" />
        <el-input v-model="newKey" placeholder="粘贴上游 API Key" style="width:380px" show-password />
        <el-button type="primary" @click="addKey" :disabled="!newKey">添加</el-button>
      </div>
      <el-table :data="keys" size="small">
        <el-table-column label="名称" width="180">
          <template #default="{ row }">
            <template v-if="editingKeyId === row.id">
              <el-input v-model="editingKeyName" size="small" placeholder="名称" @keyup.enter="saveKeyName(row)" />
              <el-button size="small" type="primary" link @click="saveKeyName(row)">保存</el-button>
              <el-button size="small" link @click="cancelEditKeyName">取消</el-button>
            </template>
            <template v-else>
              <span :class="{ 'name-empty': !row.name }">{{ row.name || '未命名' }}</span>
              <el-button size="small" link @click="startEditKeyName(row)">编辑</el-button>
            </template>
          </template>
        </el-table-column>
        <el-table-column label="Key" min-width="200">
          <template #default="{ row }">
            <span style="font-family:monospace">{{ mask(row.api_key) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="keyTagType(row.status)">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="consecutive_fails" label="连续失败" width="90" />
        <el-table-column prop="last_error" label="最近错误" min-width="160" show-overflow-tooltip />
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button size="small" :loading="testing === row.id" @click="testKey(row)">测试</el-button>
            <el-button size="small" :type="row.disabled ? 'success' : 'warning'" @click="toggleKey(row)">
              {{ row.disabled ? '启用' : '禁用' }}
            </el-button>
            <el-popconfirm title="确认删除？" @confirm="removeKey(row)">
              <template #reference><el-button size="small" type="danger">删除</el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { get, post, patch, del } from '../api'

const providers = ref([])
const provFormVisible = ref(false)
const provForm = ref({})
const keysVisible = ref(false)
const current = ref(null)
const keys = ref([])
const newKey = ref('')
const newKeyName = ref('')
const testing = ref(null)
const editingKeyId = ref(null)
const editingKeyName = ref('')

const mask = (k) => (k && k.length > 12 ? k.slice(0, 6) + '****' + k.slice(-4) : k)
const keyTagType = (s) => ({ active: 'success', rate_limited: 'warning', invalid: 'danger', disabled: 'info' }[s] || '')

async function load() {
  providers.value = await get('/api/admin/providers')
}

function openProvForm(row) {
  provForm.value = row
    ? { ...row }
    : { name: '', type: 'openai', base_url: '', disabled: false, user_agent: '', custom_headers: '' }
  // 旧数据可能没这两个字段
  if (provForm.value.user_agent === undefined) provForm.value.user_agent = ''
  if (provForm.value.custom_headers === undefined) provForm.value.custom_headers = ''
  provFormVisible.value = true
}

async function saveProvider() {
  try {
    if (provForm.value.id) {
      await patch(`/api/admin/providers/${provForm.value.id}`, provForm.value)
    } else {
      await post('/api/admin/providers', provForm.value)
    }
    provFormVisible.value = false
    ElMessage.success('已保存')
    load()
  } catch (e) { ElMessage.error(e.message) }
}

async function removeProvider(row) {
  try {
    await del(`/api/admin/providers/${row.id}`)
    load()
  } catch (e) { ElMessage.error(e.message) }
}

function openKeys(row) {
  current.value = row
  keys.value = row.keys || []
  newKey.value = ''
  newKeyName.value = ''
  editingKeyId.value = null
  keysVisible.value = true
}

async function addKey() {
  if (!newKey.value) return
  try {
    await post(`/api/admin/providers/${current.value.id}/keys`, {
      name: newKeyName.value,
      api_key: newKey.value
    })
    newKey.value = ''
    newKeyName.value = ''
    ElMessage.success('已添加')
    await load()
    keys.value = providers.value.find(p => p.id === current.value.id)?.keys || []
  } catch (e) { ElMessage.error(e.message) }
}

function startEditKeyName(row) {
  editingKeyId.value = row.id
  editingKeyName.value = row.name || ''
}

function cancelEditKeyName() {
  editingKeyId.value = null
  editingKeyName.value = ''
}

async function saveKeyName(row) {
  const name = editingKeyName.value
  try {
    await patch(`/api/admin/provider-keys/${row.id}`, { name })
    editingKeyId.value = null
    ElMessage.success('已保存')
    await load()
    keys.value = providers.value.find(p => p.id === current.value.id)?.keys || []
  } catch (e) { ElMessage.error(e.message) }
}

async function testKey(row) {
  testing.value = row.id
  try {
    const r = await post(`/api/admin/provider-keys/${row.id}/test`)
    if (r.ok) ElMessage.success('连通正常')
    else ElMessage.error(`失败 (${r.status}): ${String(r.error).slice(0, 120)}`)
    await load()
    keys.value = providers.value.find(p => p.id === current.value.id)?.keys || []
  } catch (e) { ElMessage.error(e.message) }
  finally { testing.value = null }
}

async function toggleKey(row) {
  await patch(`/api/admin/provider-keys/${row.id}`, { status: row.disabled ? 'active' : 'disabled' })
  await load()
  keys.value = providers.value.find(p => p.id === current.value.id)?.keys || []
}

async function removeKey(row) {
  await del(`/api/admin/provider-keys/${row.id}`)
  await load()
  keys.value = providers.value.find(p => p.id === current.value.id)?.keys || []
}

onMounted(load)
</script>

<style scoped>
.name-empty { color: var(--el-text-color-secondary); font-style: italic; }
</style>
