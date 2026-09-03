<template>
  <div class="page">
    <el-card class="page-head-card">
      <template #header>
        <div class="page-head">
          <span class="page-title">全站统计</span>
          <div class="page-head-right">
            <el-select v-model="days" style="width:120px" @change="load">
              <el-option :value="1" label="近 1 天" />
              <el-option :value="7" label="近 7 天" />
              <el-option :value="30" label="近 30 天" />
              <el-option :value="90" label="近 90 天" />
            </el-select>
          </div>
        </div>
      </template>

      <div class="filters">
        <el-select v-model="filters.user_id" placeholder="用户" clearable filterable style="width:160px" @change="load">
          <el-option v-for="u in users" :key="u.id" :value="u.id" :label="u.username" />
        </el-select>
        <el-select v-model="filters.provider_id" placeholder="供应商" clearable filterable style="width:180px" @change="load">
          <el-option v-for="p in providers" :key="p.id" :value="p.id" :label="`${p.name} (${p.type})`" />
        </el-select>
        <el-select v-model="filters.model" placeholder="下游模型" clearable filterable allow-create style="width:180px" @change="load">
          <el-option v-for="m in allModels" :key="m.id" :value="m.name" :label="m.name" />
        </el-select>
        <el-select v-model="filters.err_type" placeholder="错误类型" clearable style="width:140px" @change="load">
          <el-option value="auth" label="auth" />
          <el-option value="rate_limit" label="rate_limit" />
          <el-option value="server" label="server" />
          <el-option value="timeout" label="timeout" />
          <el-option value="network" label="network" />
          <el-option value="client" label="client" />
          <el-option value="cancel" label="cancel" />
        </el-select>
        <el-button @click="resetFilters">重置</el-button>
      </div>
    </el-card>

    <div v-if="totals" class="summary">
      <div class="summary-item">
        <span class="summary-label">请求数</span>
        <span class="summary-value num">{{ fmt(totals.requests) }}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">成功率</span>
        <span class="summary-value num">{{ successRateText }}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">总费用 (USD)</span>
        <span class="summary-value num">${{ Number(totals.cost || 0).toFixed(6) }}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">输入 / 输出</span>
        <span class="summary-value num">{{ fmt(totals.prompt_tokens) }} / {{ fmt(totals.completion_tokens) }}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">P50 / P95 延迟</span>
        <span class="summary-value num">{{ Math.round(totals.avg_latency_ms) }} / {{ Math.round(totals.p95_latency_ms) }} ms</span>
      </div>
    </div>

    <el-card class="trend-card">
      <template #header>
        <div class="card-head-flex">
          <span>每日趋势</span>
          <span class="card-head-meta">{{ daily.length }} 天</span>
        </div>
      </template>
      <v-chart
        v-if="daily.length > 0"
        :option="trendOption"
        :autoresize="true"
        class="chart"
      />
      <div v-else class="empty">暂无数据</div>
    </el-card>

    <div class="grid-2">
      <el-card>
        <template #header>
          <div class="card-head-flex">
            <span>模型消费 TOP</span>
            <span class="card-head-meta">{{ data.by_model?.length || 0 }} 个</span>
          </div>
        </template>
        <el-table :data="data.by_model || []" size="small">
          <el-table-column label="模型" min-width="180">
            <template #default="{ row }"><span class="mono">{{ row.model }}</span></template>
          </el-table-column>
          <el-table-column prop="requests" label="请求" width="80" align="right" header-align="left" />
          <el-table-column label="费用" width="140" align="right" header-align="left">
            <template #default="{ row }"><span class="mono">${{ Number(row.cost || 0).toFixed(6) }}</span></template>
          </el-table-column>
        </el-table>
        <div v-if="!data.by_model?.length" class="empty">暂无数据</div>
      </el-card>

      <el-card>
        <template #header>
          <div class="card-head-flex">
            <span>用户消费 TOP</span>
            <span class="card-head-meta">合计 ${{ Number(totals?.cost || 0).toFixed(6) }}</span>
          </div>
        </template>
        <el-table :data="data.by_user || []" size="small">
          <el-table-column label="用户" min-width="140">
            <template #default="{ row }">
              <span class="mono">{{ row.username || ('#' + row.user_id) }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="requests" label="请求" width="90" align="right" header-align="left" />
          <el-table-column label="费用" width="140" align="right" header-align="left">
            <template #default="{ row }"><span class="mono">${{ Number(row.cost || 0).toFixed(6) }}</span></template>
          </el-table-column>
        </el-table>
        <div v-if="!data.by_user?.length" class="empty">暂无数据</div>
      </el-card>
    </div>

    <div class="grid-2">
      <el-card>
        <template #header>
          <div class="card-head-flex">
            <span>供应商分布</span>
            <span class="card-head-meta">{{ data.by_provider?.length || 0 }} 个</span>
          </div>
        </template>
        <el-table :data="data.by_provider || []" size="small">
          <el-table-column label="供应商" min-width="160">
            <template #default="{ row }">
              <span class="mono">{{ row.provider_name }}</span>
              <el-tag size="small" style="margin-left:6px">{{ row.provider_type }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="attempts" label="尝试" width="80" align="right" header-align="left" />
          <el-table-column label="成功率" width="90" align="right" header-align="left">
            <template #default="{ row }">
              <span class="mono">{{ successRateTextOf(row) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="输入/输出" width="160" align="right" header-align="left">
            <template #default="{ row }">
              <span class="mono">{{ fmt(row.prompt_tokens) }} / {{ fmt(row.completion_tokens) }}</span>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="!data.by_provider?.length" class="empty">暂无数据</div>
      </el-card>

      <el-card>
        <template #header>
          <div class="card-head-flex">
            <span>状态与错误分布</span>
            <span class="card-head-meta">HTTP 状态 / 上游错误类型</span>
          </div>
        </template>
        <div class="two-list">
          <div>
            <div class="list-title">HTTP 状态码</div>
            <el-table :data="data.by_status || []" size="small">
              <el-table-column label="状态" width="80">
                <template #default="{ row }">
                  <el-tag size="small" :type="Number(row.status) === 200 ? 'success' : 'danger'">{{ row.status }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="requests" label="次数" align="right" header-align="left">
                <template #default="{ row }"><span class="mono">{{ fmt(row.requests) }}</span></template>
              </el-table-column>
            </el-table>
            <div v-if="!data.by_status?.length" class="empty">暂无数据</div>
          </div>
          <div>
            <div class="list-title">上游错误类型</div>
            <el-table :data="data.by_err_type || []" size="small">
              <el-table-column prop="err_type" label="类型" />
              <el-table-column prop="attempts" label="次数" align="right" header-align="left">
                <template #default="{ row }"><span class="mono">{{ fmt(row.attempts) }}</span></template>
              </el-table-column>
            </el-table>
            <div v-if="!data.by_err_type?.length" class="empty">暂无数据</div>
          </div>
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { get } from '../api'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart, BarChart } from 'echarts/charts'
import {
  TitleComponent,
  TooltipComponent,
  LegendComponent,
  GridComponent
} from 'echarts/components'
import VChart from 'vue-echarts'

use([
  CanvasRenderer, LineChart, BarChart,
  TitleComponent, TooltipComponent, LegendComponent, GridComponent
])

const days = ref(7)
const users = ref([])
const providers = ref([])
const allModels = ref([])
const data = ref({})
const daily = ref([])

const filters = ref({
  user_id: null,
  provider_id: null,
  model: '',
  err_type: ''
})

const fmt = (n) => Number(n ?? 0).toLocaleString()

const totals = computed(() => data.value?.totals)

const successRateText = computed(() => {
  const sr = totals.value?.success_rate || 0
  return (sr * 100).toFixed(1) + '%'
})

function successRateTextOf(row) {
  if (!row.attempts) return '-'
  return ((Number(row.success_count || 0) / Number(row.attempts)) * 100).toFixed(1) + '%'
}

const COLOR = {
  green: '#4ade80', amber: '#f59e0b', dim: '#9ca3af',
  purple: '#c084fc', blue: '#5aa9e6', red: '#f87171'
}

function formatDay(s) {
  const m = String(s || '').match(/^(\d{4})-(\d{2})-(\d{2})/)
  return m ? `${m[2]}-${m[3]}` : s
}

const trendOption = computed(() => {
  const ds = [...daily.value].sort((a, b) => String(a.day).localeCompare(String(b.day)))
  const dates = ds.map(d => formatDay(d.day))
  return {
    tooltip: {
      trigger: 'axis',
      backgroundColor: '#0f1519',
      borderColor: '#1f2937',
      textStyle: { color: '#c9d4cb', fontFamily: 'ui-monospace, monospace', fontSize: 12 }
    },
    legend: {
      data: ['请求数', '输入 tokens', '输出 tokens', '费用'],
      textStyle: { color: '#c9d4cb', fontFamily: 'ui-monospace, monospace', fontSize: 11 },
      top: 4, right: 8, icon: 'circle', itemWidth: 8, itemHeight: 8
    },
    grid: { left: 60, right: 70, top: 36, bottom: 28 },
    xAxis: {
      type: 'category', data: dates,
      axisLine: { lineStyle: { color: '#374151' } },
      axisTick: { show: false },
      axisLabel: { color: '#6b7280', fontFamily: 'ui-monospace, monospace', fontSize: 10 }
    },
    yAxis: [
      {
        type: 'value', name: 'tokens',
        axisLine: { show: false }, axisTick: { show: false },
        splitLine: { lineStyle: { color: '#1f2937', type: 'dashed' } },
        axisLabel: {
          color: '#6b7280', fontFamily: 'ui-monospace, monospace', fontSize: 10,
          formatter: (v) => v >= 1000 ? (v / 1000).toFixed(1) + 'k' : v
        }
      },
      {
        type: 'value', name: 'USD',
        position: 'right',
        axisLine: { show: false }, axisTick: { show: false },
        splitLine: { show: false },
        axisLabel: {
          color: '#6b7280', fontFamily: 'ui-monospace, monospace', fontSize: 10,
          formatter: (v) => v === 0 ? '$0' : (v < 1 ? '$' + v.toFixed(4) : '$' + v.toFixed(2))
        }
      }
    ],
    series: [
      {
        name: '输入 tokens', type: 'line', smooth: true, symbol: 'circle', symbolSize: 5,
        lineStyle: { width: 2, color: COLOR.green },
        itemStyle: { color: COLOR.green, borderColor: '#080b0d', borderWidth: 2 },
        areaStyle: {
          color: {
            type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(74, 222, 128, 0.30)' },
              { offset: 1, color: 'rgba(74, 222, 128, 0.02)' }
            ]
          }
        },
        data: ds.map(d => Number(d.prompt_tokens) || 0)
      },
      {
        name: '输出 tokens', type: 'line', smooth: true, symbol: 'circle', symbolSize: 5,
        lineStyle: { width: 2, color: COLOR.purple },
        itemStyle: { color: COLOR.purple, borderColor: '#080b0d', borderWidth: 2 },
        data: ds.map(d => Number(d.completion_tokens) || 0)
      },
      {
        name: '请求数', type: 'bar', yAxisIndex: 0, barMaxWidth: 12,
        itemStyle: { color: 'rgba(90, 169, 230, 0.25)', borderRadius: [2, 2, 0, 0] },
        data: ds.map(d => Number(d.requests) || 0)
      },
      {
        name: '费用', type: 'line', smooth: true, yAxisIndex: 1,
        symbol: 'circle', symbolSize: 5,
        lineStyle: { width: 2, color: COLOR.amber, type: 'dashed' },
        itemStyle: { color: COLOR.amber, borderColor: '#080b0d', borderWidth: 2 },
        data: ds.map(d => Number(d.cost) || 0)
      }
    ]
  }
})

async function load() {
  const params = new URLSearchParams()
  params.set('days', days.value)
  if (filters.value.user_id) params.set('user_id', filters.value.user_id)
  if (filters.value.provider_id) params.set('provider_id', filters.value.provider_id)
  if (filters.value.model) params.set('model', filters.value.model)
  if (filters.value.err_type) params.set('err_type', filters.value.err_type)

  const d = await get(`/api/admin/analytics?${params}`)
  data.value = d
  daily.value = padDaily(d.daily || [], days.value)
}

// 与 Dashboard.vue 一致：缺失日补零，保证 X 轴连续
function padDaily(rows, n) {
  const byDay = new Map()
  for (const r of rows || []) {
    if (r && r.day) byDay.set(String(r.day), r)
  }
  const out = []
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  for (let i = n - 1; i >= 0; i--) {
    const d = new Date(today)
    d.setDate(today.getDate() - i)
    const key = d.getFullYear() + '-'
      + String(d.getMonth() + 1).padStart(2, '0') + '-'
      + String(d.getDate()).padStart(2, '0')
    const r = byDay.get(key)
    if (r) {
      out.push({
        day: key,
        requests: Number(r.requests) || 0,
        prompt_tokens: Number(r.prompt_tokens) || 0,
        completion_tokens: Number(r.completion_tokens) || 0,
        cache_read_tokens: Number(r.cache_read_tokens) || 0,
        cache_write_tokens: Number(r.cache_write_tokens) || 0,
        cost: Number(r.cost) || 0
      })
    } else {
      out.push({
        day: key,
        requests: 0,
        prompt_tokens: 0,
        completion_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        cost: 0
      })
    }
  }
  return out
}

function resetFilters() {
  filters.value = {
    user_id: null, provider_id: null, model: '', err_type: ''
  }
  load()
}

async function loadAux() {
  const [u, p, m] = await Promise.all([
    get('/api/admin/users'),
    get('/api/admin/providers'),
    get('/api/admin/models')
  ])
  users.value = u || []
  providers.value = p || []
  allModels.value = m || []
}

onMounted(async () => {
  await loadAux()
  await load()
})
</script>

<style scoped>
.page-head-card { margin-bottom: 14px; }
.page-head {
  display: flex; align-items: center; justify-content: space-between;
  gap: 16px; flex-wrap: wrap; width: 100%;
}
.page-title { color: var(--th-fg); font-weight: 600; }
.page-head-right { display: flex; align-items: center; gap: 12px; }

.filters {
  display: flex; gap: 8px; flex-wrap: wrap; align-items: center;
}

.summary {
  display: flex; gap: 32px; padding: 4px 2px 14px; flex-wrap: wrap;
}
.summary-item { display: flex; flex-direction: column; gap: 4px; }
.summary-label {
  font-size: 11px; color: var(--th-fg-dim); letter-spacing: 0.04em;
}
.summary-value {
  font-size: 18px; font-weight: 600; color: var(--th-green);
}

.trend-card { margin-bottom: 14px; }
.chart { width: 100%; height: 320px; }

.grid-2 {
  display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-bottom: 14px;
}
@media (max-width: 900px) {
  .grid-2 { grid-template-columns: 1fr; }
}

.two-list { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
@media (max-width: 600px) {
  .two-list { grid-template-columns: 1fr; }
}
.list-title {
  font-size: 11px; color: var(--th-fg-dim);
  margin-bottom: 6px; letter-spacing: 0.04em;
}

.card-head-meta {
  font-size: 11px; color: var(--th-fg-dim);
  font-family: ui-monospace, monospace;
}

.empty {
  color: var(--th-fg-dim); font-size: 12px;
  font-family: ui-monospace, monospace;
  display: flex; align-items: center; justify-content: center;
  padding: 20px 0;
}
</style>
