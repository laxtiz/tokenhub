<template>
  <div>
    <el-card>
      <template #header>
        <div class="card-head-flex">
          <span>{{ isAdmin ? '模型与渠道管理' : '模型列表' }}</span>
          <el-button v-if="isAdmin" type="primary" @click="openEdit(null)">新建模型</el-button>
        </div>
      </template>
      <el-table :data="models" stripe class="models-table">
        <el-table-column label="模型" min-width="200">
          <template #default="{ row }">
            <div class="model-name-cell">
              <div class="model-name">{{ row.name }}</div>
              <div v-if="row.display_name" class="model-display-name">{{ row.display_name }}</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="context_length" label="上下文" width="110" align="right" header-align="left">
          <template #default="{ row }">{{ row.context_length?.toLocaleString() }}</template>
        </el-table-column>
        <el-table-column label="能力" min-width="170">
          <template #default="{ row }">
            <div class="cap-tags">
              <el-tag v-if="row.support_vision" size="small">视觉</el-tag>
              <el-tag v-if="row.support_tools" size="small" type="success">工具</el-tag>
              <el-tag v-if="row.support_reasoning" size="small" type="warning">推理</el-tag>
              <el-tag v-if="row.disabled" size="small" type="danger">停用</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="价格 / 1M tokens" min-width="240">
          <template #default="{ row }">
            <div class="price-cell">
              <div class="price-row">
                <span><span class="k">输入</span>${{ row.input_price }}</span>
                <span><span class="k">输出</span>${{ row.output_price }}</span>
              </div>
              <div class="price-row price-row-cache">
                <span><span class="k">缓存读</span>${{ row.cache_read_price }}</span>
                <span><span class="k">缓存写</span>${{ row.cache_write_price }}</span>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column v-if="isAdmin" label="上游渠道" width="90" align="right" header-align="left">
          <template #default="{ row }">{{ (row.channels || []).length }} 个</template>
        </el-table-column>
        <el-table-column v-if="isAdmin" label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" @click="openChannel(row)">渠道</el-button>
            <el-popconfirm title="确认删除？" @confirm="removeModel(row)">
              <template #reference><el-button size="small" type="danger">删除</el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
      <div class="card-list">
        <div v-for="row in models" :key="row.id" class="card-row">
          <div class="row-head">
            <div>
              <span class="row-title model-name">{{ row.name }}</span>
              <span v-if="row.display_name" class="model-display-name-mobile">· {{ row.display_name }}</span>
            </div>
            <span style="display:flex;gap:4px;flex-wrap:wrap">
              <el-tag v-if="row.support_vision" size="small">视觉</el-tag>
              <el-tag v-if="row.support_tools" size="small" type="success">工具</el-tag>
              <el-tag v-if="row.support_reasoning" size="small" type="warning">推理</el-tag>
              <el-tag v-if="row.disabled" size="small" type="danger">停用</el-tag>
            </span>
          </div>
          <div v-if="row.context_length" class="field"><span class="k">上下文</span><span class="v">{{ row.context_length?.toLocaleString() }}</span></div>
          <div class="field"><span class="k">输入/输出</span><span class="v">${{ row.input_price }} / ${{ row.output_price }}</span></div>
          <div class="field"><span class="k">缓存读/写</span><span class="v">${{ row.cache_read_price }} / ${{ row.cache_write_price }}</span></div>
          <div v-if="isAdmin" class="field"><span class="k">上游渠道</span><span class="v">{{ (row.channels || []).length }} 个</span></div>
          <div v-if="isAdmin" class="row-actions">
            <el-button size="small" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" @click="openChannel(row)">渠道</el-button>
            <el-popconfirm title="确认删除？" @confirm="removeModel(row)">
              <template #reference><el-button size="small" type="danger">删除</el-button></template>
            </el-popconfirm>
          </div>
        </div>
      </div>
    </el-card>

    <!-- 模型编辑 -->
    <el-dialog v-model="editVisible" :title="form.id ? '编辑模型' : '新建模型'" width="560px">
      <el-form :model="form" label-width="110px">
        <el-form-item label="模型名"><el-input v-model="form.name" :disabled="!!form.id" placeholder="下游调用的模型名" /></el-form-item>
        <el-form-item label="显示名"><el-input v-model="form.display_name" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="form.description" /></el-form-item>
        <el-form-item label="上下文长度"><el-input-number v-model="form.context_length" :min="0" /></el-form-item>
        <el-form-item label="能力">
          <el-checkbox v-model="form.support_vision">视觉</el-checkbox>
          <el-checkbox v-model="form.support_tools">工具调用</el-checkbox>
          <el-checkbox v-model="form.support_reasoning">推理</el-checkbox>
          <el-checkbox v-model="form.disabled">停用</el-checkbox>
        </el-form-item>
        <el-form-item label="输入价格"><el-input-number v-model="form.input_price" :step="0.1" :precision="4" /></el-form-item>
        <el-form-item label="输出价格"><el-input-number v-model="form.output_price" :step="0.1" :precision="4" /></el-form-item>
        <el-form-item label="缓存读价格"><el-input-number v-model="form.cache_read_price" :step="0.05" :precision="4" /></el-form-item>
        <el-form-item label="缓存写价格"><el-input-number v-model="form.cache_write_price" :step="0.05" :precision="4" /></el-form-item>
        <el-form-item label="币种"><el-input v-model="form.currency" style="width:100px" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" @click="saveModel">保存</el-button>
      </template>
    </el-dialog>

    <!-- 渠道管理 -->
    <el-dialog v-model="channelVisible" :title="`上游渠道 - ${current?.name}`" width="720px">
      <el-button type="primary" size="small" @click="openChannelForm(null)" style="margin-bottom:10px">添加渠道</el-button>
      <el-table :data="channels" size="small">
        <el-table-column label="供应商" width="150">
          <template #default="{ row }">{{ providerName(row.provider_id) }}</template>
        </el-table-column>
        <el-table-column prop="upstream_model" label="上游模型名" />
        <el-table-column prop="priority" label="优先级" width="90" />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.disabled ? 'info' : 'success'">{{ row.disabled ? '停用' : '启用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button size="small" @click="openChannelForm(row)">编辑</el-button>
            <el-popconfirm title="确认删除？" @confirm="removeChannel(row)">
              <template #reference><el-button size="small" type="danger">删除</el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <el-dialog v-model="channelFormVisible" :title="chForm.id ? '编辑渠道' : '添加渠道'" width="440px" append-to-body>
        <el-form :model="chForm" label-width="100px">
          <el-form-item label="供应商">
            <el-select v-model="chForm.provider_id">
              <el-option v-for="p in providers" :key="p.id" :value="p.id" :label="`${p.name} (${p.type})`" />
            </el-select>
          </el-form-item>
          <el-form-item label="上游模型名">
            <el-select v-model="chForm.upstream_model" filterable allow-create default-first-option
              :loading="loadingModels" placeholder="选择或输入上游模型 id" style="flex:1">
              <el-option v-for="id in upstreamModels" :key="id" :value="id" :label="id" />
            </el-select>
            <el-button size="small" :loading="loadingModels" @click="fetchUpstreamModels(chForm.provider_id)" style="margin-left:8px">
              刷新
            </el-button>
          </el-form-item>
          <el-form-item label="优先级"><el-input-number v-model="chForm.priority" :min="0" /><span style="margin-left:8px;color:#999">数字越小越优先</span></el-form-item>
          <el-form-item label="停用"><el-switch v-model="chForm.disabled" /></el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="channelFormVisible = false">取消</el-button>
          <el-button type="primary" @click="saveChannel">保存</el-button>
        </template>
      </el-dialog>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { get, post, patch, del } from '../api'
import { getToken } from '../api'

const isAdmin = computed(() => {
  try {
    const payload = JSON.parse(atob(getToken().split('.')[1]))
    return payload.role === 'admin'
  } catch { return false }
})

const models = ref([])
const providers = ref([])
const editVisible = ref(false)
const form = ref({})
const channelVisible = ref(false)
const channels = ref([])
const current = ref(null)
const channelFormVisible = ref(false)
const chForm = ref({})
const upstreamModels = ref([])
const loadingModels = ref(false)

function emptyModel() {
  return { name: '', display_name: '', description: '', context_length: 128000,
    support_vision: false, support_tools: true, support_reasoning: false,
    input_price: 0, output_price: 0, cache_read_price: 0, cache_write_price: 0, currency: 'USD' }
}

const providerName = (id) => providers.value.find(p => p.id === id)?.name || id

async function load() {
  if (isAdmin.value) {
    models.value = await get('/api/admin/models')
    providers.value = await get('/api/admin/providers')
  } else {
    models.value = await get('/api/user/models')
  }
}

function openEdit(row) {
  form.value = row ? { ...row } : emptyModel()
  editVisible.value = true
}

async function saveModel() {
  try {
    if (form.value.id) {
      await patch(`/api/admin/models/${form.value.id}`, form.value)
    } else {
      await post('/api/admin/models', form.value)
    }
    editVisible.value = false
    ElMessage.success('已保存')
    load()
  } catch (e) { ElMessage.error(e.message) }
}

async function removeModel(row) {
  await del(`/api/admin/models/${row.id}`)
  load()
}

async function openChannel(row) {
  current.value = row
  channels.value = row.channels || []
  channelVisible.value = true
}

function openChannelForm(row) {
  chForm.value = row ? { ...row } : { provider_id: providers.value[0]?.id, upstream_model: '', priority: 1, disabled: false }
  channelFormVisible.value = true
  fetchUpstreamModels(chForm.value.provider_id)
}

async function fetchUpstreamModels(providerId) {
  if (!providerId) { upstreamModels.value = []; return }
  loadingModels.value = true
  try {
    const r = await get(`/api/admin/providers/${providerId}/models`)
    upstreamModels.value = (r?.models || []).map(m => m.id)
    const cur = chForm.value?.upstream_model
    if (cur && !upstreamModels.value.includes(cur)) {
      // 切换 provider 后清空旧模型名，避免误指到别家
      chForm.value.upstream_model = ''
    }
  } catch (e) {
    upstreamModels.value = []
    ElMessage.warning('拉取模型失败：' + e.message + '，可手填')
  } finally {
    loadingModels.value = false
  }
}

watch(() => chForm.value?.provider_id, (newId, oldId) => {
  if (newId && newId !== oldId) fetchUpstreamModels(newId)
})

async function saveChannel() {
  try {
    if (chForm.value.id) {
      await patch(`/api/admin/channels/${chForm.value.id}`, chForm.value)
    } else {
      await post(`/api/admin/models/${current.value.id}/channels`, chForm.value)
    }
    channelFormVisible.value = false
    ElMessage.success('已保存')
    load()
    channels.value = (models.value.find(m => m.id === current.value.id)?.channels) || []
  } catch (e) { ElMessage.error(e.message) }
}

async function removeChannel(row) {
  await del(`/api/admin/channels/${row.id}`)
  load()
  channels.value = (models.value.find(m => m.id === current.value.id)?.channels) || []
}

onMounted(load)
</script>

<style scoped>
.models-table :deep(.el-table__cell) {
  padding: 10px 0;
}
.models-table :deep(th.el-table__cell) {
  padding: 12px 0;
}
.model-name-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
  line-height: 1.4;
}
.model-name {
  color: var(--th-green);
  font-family: ui-monospace, monospace;
  font-weight: 600;
}
.model-display-name {
  color: var(--th-fg-dim);
  font-size: 12px;
}
.model-display-name-mobile {
  color: var(--th-fg-dim);
  font-size: 12px;
  font-weight: 400;
}
.cap-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.price-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-family: ui-monospace, monospace;
  font-size: 12px;
  line-height: 1.5;
}
.price-row {
  display: flex;
  gap: 14px;
}
.price-row-cache {
  color: var(--th-fg-dim);
  font-size: 11px;
}
.price-cell .k {
  color: var(--th-fg-dim);
  margin-right: 4px;
}
</style>
