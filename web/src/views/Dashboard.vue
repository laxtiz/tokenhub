<template>
  <div class="page">
    <div class="stats">
      <div class="stat-card">
        <div class="stat-label">请求数</div>
        <div class="stat-value num">{{ fmt(totals.requests) }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">总费用 (USD)</div>
        <div class="stat-value num">${{ totals.cost.toFixed(6) }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">输入 tokens</div>
        <div class="stat-value num">{{ fmt(totals.input) }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">输出 tokens</div>
        <div class="stat-value num">{{ fmt(totals.output) }}</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">缓存命中</div>
        <div class="stat-value num">{{ fmt(totals.cache) }}</div>
      </div>
    </div>

    <el-card>
      <template #header>
        <div class="card-head-flex">
          <span>每日用量</span>
          <el-select v-model="days" style="width:120px" @change="load">
            <el-option :value="7" label="近 7 天" />
            <el-option :value="30" label="近 30 天" />
            <el-option :value="90" label="近 90 天" />
          </el-select>
        </div>
      </template>
      <el-table :data="daily" size="small">
        <el-table-column prop="day" label="日期" width="120" />
        <el-table-column prop="requests" label="请求数" />
        <el-table-column label="输入"><template #default="{ row }">{{ fmt(row.prompt_tokens) }}</template></el-table-column>
        <el-table-column label="输出"><template #default="{ row }">{{ fmt(row.completion_tokens) }}</template></el-table-column>
        <el-table-column label="缓存读"><template #default="{ row }">{{ fmt(row.cache_read_tokens) }}</template></el-table-column>
        <el-table-column label="缓存写"><template #default="{ row }">{{ fmt(row.cache_write_tokens) }}</template></el-table-column>
        <el-table-column label="费用 (USD)" align="right">
          <template #default="{ row }"><span class="mono">${{ (row.cost || 0).toFixed(6) }}</span></template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card>
      <template #header>模型消费 TOP</template>
      <el-table :data="byModel" size="small">
        <el-table-column prop="model" label="模型">
          <template #default="{ row }"><span class="mono">{{ row.model }}</span></template>
        </el-table-column>
        <el-table-column prop="requests" label="请求数" width="120" />
        <el-table-column label="费用 (USD)" width="160" align="right">
          <template #default="{ row }"><span class="mono">${{ (row.cost || 0).toFixed(6) }}</span></template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { get } from '../api'

const days = ref(7)
const daily = ref([])
const byModel = ref([])

const fmt = (n) => (n ?? 0).toLocaleString()

const totals = computed(() => {
  let requests = 0, cost = 0, input = 0, output = 0, cache = 0
  for (const d of daily.value) {
    requests += Number(d.requests) || 0
    cost += Number(d.cost) || 0
    input += Number(d.prompt_tokens) || 0
    output += Number(d.completion_tokens) || 0
    cache += (Number(d.cache_read_tokens) || 0) + (Number(d.cache_write_tokens) || 0)
  }
  return { requests, cost, input, output, cache }
})

async function load() {
  const data = await get(`/api/user/stats?days=${days.value}`)
  daily.value = data.daily || []
  byModel.value = data.by_model || []
}
onMounted(load)
</script>

<style scoped>
.stats {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 12px;
}
@media (max-width: 1100px) { .stats { grid-template-columns: repeat(2, 1fr); } }
.stat-card {
  border: 1px solid var(--th-border);
  border-radius: 6px;
  background: var(--th-panel);
  padding: 12px 14px;
}
.stat-label {
  font-size: 11px; color: var(--th-fg-dim);
  letter-spacing: 0.04em; margin-bottom: 7px;
}
.stat-value { font-size: 20px; font-weight: 600; color: var(--th-green); }
</style>
