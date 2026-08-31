<template>
  <div>
    <el-card>
      <template #header>
        <div class="card-head-flex">
          <span>{{ isAdmin ? '全站请求日志' : '我的请求日志' }}</span>
          <div style="display:flex;gap:8px;flex-wrap:wrap">
            <el-input v-model="q.trace_id" placeholder="Trace ID" clearable style="width:220px" @keyup.enter="load" />
            <el-input v-model="q.model" placeholder="模型" clearable style="width:140px" @keyup.enter="load" />
            <el-select v-model="q.status" placeholder="状态" clearable style="width:110px">
              <el-option :value="200" label="成功" />
              <el-option :value="401" label="401" />
              <el-option :value="429" label="429" />
              <el-option :value="500" label="5xx" />
              <el-option :value="502" label="502" />
            </el-select>
            <el-button type="primary" @click="load">查询</el-button>
          </div>
        </div>
      </template>
      <el-table :data="items" size="small" stripe>
        <el-table-column prop="trace_id" label="Trace ID" width="220">
          <template #default="{ row }">
            <el-link type="primary" :title="row.trace_id" @click="openTrace(row.trace_id)">{{ shortTrace(row.trace_id) }}</el-link>
          </template>
        </el-table-column>
        <el-table-column v-if="isAdmin" prop="username" label="用户" width="100" />
        <el-table-column prop="model" label="模型" min-width="140" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag size="small" :type="row.status === 200 ? 'success' : 'danger'">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="输入/输出" width="120">
          <template #default="{ row }">{{ row.prompt_tokens }} / {{ row.completion_tokens }}</template>
        </el-table-column>
        <el-table-column label="费用" width="120">
          <template #default="{ row }">${{ (row.cost || 0).toFixed(6) }}</template>
        </el-table-column>
        <el-table-column label="时间" width="150">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
      </el-table>
      <div class="card-list">
        <div v-for="row in items" :key="row.trace_id" class="card-row">
          <div class="row-head">
            <span class="row-title mono" :title="row.trace_id" @click="openTrace(row.trace_id)" style="cursor:pointer">{{ shortTrace(row.trace_id) }}</span>
            <el-tag size="small" :type="row.status === 200 ? 'success' : 'danger'">{{ row.status }}</el-tag>
          </div>
          <div v-if="isAdmin" class="field"><span class="k">用户</span><span class="v">{{ row.username }}</span></div>
          <div class="field"><span class="k">模型</span><span class="v mono">{{ row.model }}</span></div>
          <div class="field"><span class="k">输入 / 输出</span><span class="v">{{ row.prompt_tokens }} / {{ row.completion_tokens }}</span></div>
          <div class="field"><span class="k">费用</span><span class="v">${{ (row.cost || 0).toFixed(6) }}</span></div>
          <div class="field"><span class="k">时间</span><span class="v">{{ fmtTime(row.created_at) }}</span></div>
        </div>
      </div>
      <el-pagination style="margin-top:12px" layout="prev, pager, next, total" :total="total"
        :page-size="q.size" v-model:current-page="q.page" @current-change="load" />
    </el-card>

    <el-dialog v-model="traceVisible" title="调用链" width="820px">
      <template v-if="trace">
        <h4>下游请求</h4>
        <el-descriptions :column="3" size="small" border>
          <el-descriptions-item label="Trace ID">{{ trace.request.trace_id }}</el-descriptions-item>
          <el-descriptions-item label="模型">{{ trace.request.model }}</el-descriptions-item>
          <el-descriptions-item label="协议">{{ trace.request.downstream_format }}</el-descriptions-item>
          <el-descriptions-item label="流式">{{ trace.request.stream ? '是' : '否' }}</el-descriptions-item>
          <el-descriptions-item label="状态">{{ trace.request.status }}</el-descriptions-item>
          <el-descriptions-item label="尝试次数">{{ trace.request.attempt_count }}</el-descriptions-item>
          <el-descriptions-item label="tokens">
            {{ trace.request.prompt_tokens }} / {{ trace.request.completion_tokens }}
            (cache 读 {{ trace.request.cache_read_tokens }} / 写 {{ trace.request.cache_write_tokens }})
          </el-descriptions-item>
          <el-descriptions-item label="费用">${{ (trace.request.cost || 0).toFixed(6) }}</el-descriptions-item>
          <el-descriptions-item label="耗时">{{ trace.request.latency_ms }}ms（首字 {{ trace.request.first_token_ms || '-' }}ms）</el-descriptions-item>
          <el-descriptions-item label="时间">{{ fmtTime(trace.request.created_at) }}</el-descriptions-item>
        </el-descriptions>
        <h4>上游调用（{{ trace.upstream.length }} 次）</h4>
        <el-table :data="trace.upstream" size="small" border>
          <el-table-column prop="attempt" label="#" width="50" />
          <el-table-column prop="provider_name" label="供应商" width="130" />
          <el-table-column prop="provider_type" label="协议" width="90" />
          <el-table-column prop="upstream_model" label="上游模型" width="150" />
          <el-table-column label="状态" width="80">
            <template #default="{ row }">
              <el-tag size="small" :type="row.status_code === 200 ? 'success' : 'danger'">{{ row.status_code }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="err_type" label="错误类型" width="100" />
          <el-table-column label="tokens" width="110">
            <template #default="{ row }">{{ row.prompt_tokens }} / {{ row.completion_tokens }}</template>
          </el-table-column>
          <el-table-column prop="latency_ms" label="耗时" width="80" />
          <el-table-column prop="error" label="错误" min-width="120" show-overflow-tooltip />
        </el-table>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { get, getToken } from '../api'

const isAdmin = computed(() => {
  try {
    const payload = JSON.parse(atob(getToken().split('.')[1]))
    return payload.role === 'admin'
  } catch { return false }
})

const items = ref([])
const total = ref(0)

// 列表里只显示完整随机后缀（去掉 - 前缀），悬停可见完整 Trace ID；详情用全量
const shortTrace = (tid) => (tid && tid.includes('-') ? tid.slice(tid.indexOf('-') + 1) : tid)

// 简化时间戳：2026-08-31T03:26:20.123+08:00 → 2026-08-31 03:26
const fmtTime = (t) => (t ? t.slice(0, 16).replace('T', ' ') : '-')
const q = ref({ page: 1, size: 20, model: '', status: null, trace_id: '' })
const traceVisible = ref(false)
const trace = ref(null)

const base = isAdmin.value ? '/api/admin' : '/api/user'

async function load() {
  const params = new URLSearchParams()
  params.set('page', q.value.page)
  params.set('size', q.value.size)
  if (q.value.model) params.set('model', q.value.model)
  if (q.value.status) params.set('status', q.value.status)
  if (q.value.trace_id) params.set('trace_id', q.value.trace_id)
  const data = await get(`${base}/logs?${params}`)
  items.value = data.items || []
  total.value = data.total || 0
}

async function openTrace(tid) {
  trace.value = await get(`${base}/logs/${tid}`)
  traceVisible.value = true
}

onMounted(load)
</script>
