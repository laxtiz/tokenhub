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
        <el-table-column label="API Keys" width="120">
          <template #default="{ row }">
            <el-button link type="primary" @click="openKeys(row)">{{ (row.keys || []).length }} 个</el-button>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="230" fixed="right">
          <template #default="{ row }">
            <div style="display:flex;gap:6px;align-items:center;white-space:nowrap">
              <el-button size="small" @click="openProvForm(row)">编辑</el-button>
              <el-button size="small" @click="openModels(row)">模型列表</el-button>
              <el-popconfirm title="确认删除？" @confirm="removeProvider(row)">
                <template #reference><el-button size="small" type="danger">删除</el-button></template>
              </el-popconfirm>
            </div>
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
          <div class="field"><span class="k">API Keys</span>
            <span class="v">
              <el-button link type="primary" @click="openKeys(row)">{{ (row.keys || []).length }} 个</el-button>
            </span>
          </div>
          <div class="row-actions">
            <el-button size="small" @click="openProvForm(row)">编辑</el-button>
            <el-button size="small" @click="openModels(row)">模型列表</el-button>
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
    <el-dialog v-model="keysVisible" :title="`API Keys - ${current?.name}`" width="1000px">
      <div style="display:flex;gap:8px;margin-bottom:10px">
        <el-input v-model="newKeyName" placeholder="名称（可选）" style="width:200px" />
        <el-input v-model="newKey" placeholder="粘贴上游 API Key" style="width:380px" show-password />
        <el-button type="primary" @click="addKey" :disabled="!newKey">添加</el-button>
      </div>
      <el-table :data="keys" size="small">
        <el-table-column label="名称" width="140">
          <template #default="{ row }">
            <span :class="{ 'name-empty': !row.name }">{{ row.name || '未命名' }}</span>
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
            <el-button size="small" :type="row.status === 'disabled' ? 'success' : 'warning'" @click="toggleKey(row)">
              {{ row.status === 'disabled' ? '启用' : '禁用' }}
            </el-button>
            <el-popconfirm title="确认删除？" @confirm="removeKey(row)">
              <template #reference><el-button size="small" type="danger">删除</el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <!-- 上游模型列表 -->
    <el-dialog v-model="modelsVisible" :title="`上游模型 - ${current?.name}`" width="720px" @opened="ensureModelsLoaded">
      <div style="display:flex;gap:8px;margin-bottom:10px;align-items:center">
        <el-input v-model="modelFilter" placeholder="按模型 id 过滤" clearable style="flex:1" />
        <el-button :loading="modelsLoading" @click="loadUpstreamModels">重新拉取</el-button>
      </div>
      <el-table :data="filteredModels" v-loading="modelsLoading" size="small" max-height="480">
        <el-table-column label="模型 id" min-width="200">
          <template #default="{ row }">
            <span style="font-family:ui-monospace,monospace">{{ row.id }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="120" align="center">
          <template #default="{ row }">
            <template v-if="testStates[row.id]">
              <el-tag v-if="testStates[row.id].ok" size="small" type="success">{{ testStates[row.id].status }} · {{ testStates[row.id].latency_ms }}ms</el-tag>
              <el-tag v-else size="small" type="danger" :title="testStates[row.id].detail">{{ testStates[row.id].status || '×' }}</el-tag>
            </template>
            <span v-else style="color:#666">—</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="280" fixed="right">
          <template #default="{ row }">
            <div style="display:flex;gap:6px;align-items:center;white-space:nowrap">
              <el-button size="small" @click="testModel(row)" :loading="testingModel === row.id">测试</el-button>
              <el-button size="small" type="primary" :loading="creatingModel === row.id" @click="createAsNewModel(row)">设为新模型</el-button>
              <el-button size="small" @click="openAttach(row)">添加到模型</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <div v-if="!modelsLoading && !modelsError && filteredModels.length === 0" style="text-align:center;color:#999;padding:20px">
        未拉取到模型
      </div>
      <div v-if="modelsError" style="color:#e6a23c;padding:8px 0">{{ modelsError }}</div>
    </el-dialog>

    <!-- 添加到模型：选择目标下游模型 -->
    <el-dialog v-model="attachVisible" title="添加到模型" width="440px" append-to-body>
      <el-form label-width="100px">
        <el-form-item label="上游模型">
          <el-input :model-value="attachModelId" disabled />
        </el-form-item>
        <el-form-item label="目标模型">
          <el-select v-model="attachTargetId" filterable placeholder="搜索并选择已有下游模型" style="width:100%">
            <el-option v-for="m in allModels" :key="m.id" :value="m.id" :label="`${m.name}${m.display_name ? ' · ' + m.display_name : ''}`" />
          </el-select>
        </el-form-item>
        <el-form-item label="优先级">
          <el-input-number v-model="attachPriority" :min="0" />
          <span style="margin-left:8px;color:#999">数字越小越优先</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="attachVisible = false">取消</el-button>
        <el-button type="primary" :loading="attaching" :disabled="!attachTargetId" @click="confirmAttach">添加</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
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

const modelsVisible = ref(false)
const modelsLoading = ref(false)
const modelsError = ref('')
const upstreamModels = ref([])
const modelFilter = ref('')
const creatingModel = ref(null)
const testingModel = ref(null)
const testStates = ref({}) // model id -> {ok, status, latency_ms, detail?}

const allModels = ref([])
const attachVisible = ref(false)
const attachModelId = ref('')
const attachTargetId = ref(null)
const attachPriority = ref(1)
const attaching = ref(false)

const filteredModels = computed(() => {
  const q = modelFilter.value.trim().toLowerCase()
  if (!q) return upstreamModels.value
  return upstreamModels.value.filter(m => m.id.toLowerCase().includes(q))
})

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
  const next = row.status === 'disabled' ? 'active' : 'disabled'
  await patch(`/api/admin/provider-keys/${row.id}`, { status: next })
  await load()
  keys.value = providers.value.find(p => p.id === current.value.id)?.keys || []
}

async function removeKey(row) {
  await del(`/api/admin/provider-keys/${row.id}`)
  await load()
  keys.value = providers.value.find(p => p.id === current.value.id)?.keys || []
}

function openModels(row) {
  current.value = row
  upstreamModels.value = []
  modelFilter.value = ''
  modelsError.value = ''
  modelsVisible.value = true
}

async function ensureModelsLoaded() {
  if (upstreamModels.value.length === 0 && !modelsError.value) {
    await loadUpstreamModels()
  }
}

async function loadUpstreamModels() {
  if (!current.value) return
  modelsLoading.value = true
  modelsError.value = ''
  try {
    const r = await get(`/api/admin/providers/${current.value.id}/models`)
    upstreamModels.value = (r?.models || []).map(m => ({ id: m.id }))
  } catch (e) {
    upstreamModels.value = []
    modelsError.value = '拉取失败：' + e.message
  } finally {
    modelsLoading.value = false
  }
}

async function testModel(row) {
  if (testingModel.value) return
  testingModel.value = row.id
  // 立即清掉旧状态，避免 UI 残留
  delete testStates.value[row.id]
  try {
    const r = await post(`/api/admin/providers/${current.value.id}/models/test`, { model: row.id })
    testStates.value = { ...testStates.value, [row.id]: r }
    if (r.ok) {
      ElMessage.success(`${row.id} 可用 (${r.latency_ms}ms)`)
    } else {
      ElMessage.warning(`${row.id} 不可用 (${r.status || '连接失败'})`)
    }
  } catch (e) {
    const status = e.status || 0
    testStates.value = { ...testStates.value, [row.id]: { ok: false, status, detail: e.message } }
    ElMessage.error(`${row.id} 测试失败：${e.message}`)
  } finally {
    testingModel.value = null
  }
}

async function createAsNewModel(row) {
  if (creatingModel.value) return
  creatingModel.value = row.id
  try {
    const m = await post('/api/admin/models', {
      name: row.id,
      display_name: row.id,
      description: '',
      context_length: 128000,
      support_vision: false,
      support_tools: true,
      support_reasoning: false,
      input_price: 0,
      output_price: 0,
      cache_read_price: 0,
      cache_write_price: 0,
      currency: 'USD'
    })
    ElMessage.success(`已创建下游模型 ${m.name}`)
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    creatingModel.value = null
  }
}

async function ensureAllModels() {
  if (allModels.value.length === 0) {
    allModels.value = await get('/api/admin/models')
  }
}

function openAttach(row) {
  attachModelId.value = row.id
  attachTargetId.value = null
  attachPriority.value = 1
  attachVisible.value = true
  ensureAllModels()
}

async function confirmAttach() {
  if (!attachTargetId.value) return
  attaching.value = true
  try {
    await post(`/api/admin/models/${attachTargetId.value}/channels`, {
      provider_id: current.value.id,
      upstream_model: attachModelId.value,
      priority: attachPriority.value,
      weight: 1,
      disabled: false
    })
    ElMessage.success('已添加渠道')
    attachVisible.value = false
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    attaching.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.name-empty { color: var(--el-text-color-secondary); font-style: italic; }
</style>
