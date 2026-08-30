<template>
  <div>
    <el-card>
      <template #header>
        <div style="display:flex;justify-content:space-between;align-items:center">
          <span>{{ isAdmin ? '模型与渠道管理' : '模型列表' }}</span>
          <el-button v-if="isAdmin" type="primary" @click="openEdit(null)">新建模型</el-button>
        </div>
      </template>
      <el-table :data="models" stripe>
        <el-table-column prop="name" label="模型名" width="220" />
        <el-table-column prop="display_name" label="显示名" width="140" />
        <el-table-column prop="context_length" label="上下文" width="100" />
        <el-table-column label="能力" width="180">
          <template #default="{ row }">
            <el-tag v-if="row.support_vision" size="small">视觉</el-tag>
            <el-tag v-if="row.support_tools" size="small" type="success">工具</el-tag>
            <el-tag v-if="row.support_reasoning" size="small" type="warning">推理</el-tag>
            <el-tag v-if="row.disabled" size="small" type="danger">停用</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="价格 / 1M tokens">
          <template #default="{ row }">
            输入 ${{ row.input_price }} · 输出 ${{ row.output_price }} · 缓存读 ${{ row.cache_read_price }} · 缓存写 ${{ row.cache_write_price }}
          </template>
        </el-table-column>
        <el-table-column v-if="isAdmin" label="上游渠道" width="100">
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
          <el-form-item label="上游模型名"><el-input v-model="chForm.upstream_model" /></el-form-item>
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
import { ref, computed, onMounted } from 'vue'
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
}

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
