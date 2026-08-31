<template>
  <div class="page">
    <el-card class="page-head-card">
      <template #header>
        <div class="page-head">
          <span class="page-title">用量统计</span>
          <div class="page-head-right">
            <el-select v-model="days" style="width:140px" @change="load">
              <el-option :value="7" label="近 7 天" />
              <el-option :value="30" label="近 30 天" />
            </el-select>
          </div>
        </div>
      </template>

      <div class="summary">
        <div class="summary-item">
          <span class="summary-label">请求数</span>
          <span class="summary-value num">{{ fmt(totals.requests) }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">总费用 (USD)</span>
          <span class="summary-value num">${{ totals.cost.toFixed(6) }}</span>
        </div>
      </div>
    </el-card>

    <div class="charts">
      <el-card class="chart-card">
        <template #header>
          <div class="card-head-flex">
            <span>输入构成</span>
            <span class="card-head-meta">合计 {{ fmt(totals.input) }} tokens</span>
          </div>
        </template>
        <div class="chart-body">
          <div class="pie-wrap">
            <v-chart
              v-if="inputBreakdown.total > 0"
              :option="inputPieOption"
              :autoresize="true"
              class="pie"
            />
            <div v-else class="empty">暂无数据</div>
          </div>
          <ul class="legend">
            <li
              v-for="seg in inputBreakdown.segments"
              :key="seg.key"
              :class="{ faded: hovered !== null && hovered !== seg.key }"
              @mouseenter="hovered = seg.key"
              @mouseleave="hovered = null"
            >
              <span class="dot" :style="{ background: seg.color }"></span>
              <span class="legend-label">{{ seg.label }}</span>
              <span class="legend-value num">{{ fmt(seg.value) }}</span>
              <span class="legend-pct">{{ seg.percent.toFixed(1) }}%</span>
            </li>
          </ul>
        </div>
      </el-card>

      <el-card class="chart-card">
        <template #header>
          <div class="card-head-flex">
            <span>输入 vs 输出</span>
            <span class="card-head-meta">合计 {{ fmt(totals.input + totals.output) }} tokens</span>
          </div>
        </template>
        <div class="chart-body">
          <div class="pie-wrap">
            <v-chart
              v-if="ioBreakdown.total > 0"
              :option="ioPieOption"
              :autoresize="true"
              class="pie"
            />
            <div v-else class="empty">暂无数据</div>
          </div>
          <ul class="legend">
            <li
              v-for="seg in ioBreakdown.segments"
              :key="seg.key"
              :class="{ faded: hovered !== null && hovered !== seg.key }"
              @mouseenter="hovered = seg.key"
              @mouseleave="hovered = null"
            >
              <span class="dot" :style="{ background: seg.color }"></span>
              <span class="legend-label">{{ seg.label }}</span>
              <span class="legend-value num">{{ fmt(seg.value) }}</span>
              <span class="legend-pct">{{ seg.percent.toFixed(1) }}%</span>
            </li>
          </ul>
        </div>
      </el-card>
    </div>

    <div class="trend-row">
      <el-card>
        <template #header>
          <div class="card-head-flex">
            <span>Token 趋势</span>
            <span class="card-head-meta">{{ daily.length }} 天</span>
          </div>
        </template>
        <div class="trend-wrap">
          <v-chart
            v-if="daily.length > 0"
            :option="tokenChartOption"
            :autoresize="true"
            class="chart"
          />
          <div v-else class="empty">暂无数据</div>
        </div>
      </el-card>

      <el-card>
        <template #header>
          <div class="card-head-flex">
            <span>费用趋势</span>
            <span class="card-head-meta">合计 ${{ totals.cost.toFixed(6) }} · {{ daily.length }} 天</span>
          </div>
        </template>
        <div class="trend-wrap">
          <v-chart
            v-if="daily.length > 0"
            :option="costChartOption"
            :autoresize="true"
            class="chart"
          />
          <div v-else class="empty">暂无数据</div>
        </div>
      </el-card>
    </div>

    <el-card>
      <template #header>模型消费 TOP</template>
      <el-table :data="byModel" size="small">
        <el-table-column prop="model" label="模型">
          <template #default="{ row }"><span class="mono">{{ row.model }}</span></template>
        </el-table-column>
        <el-table-column prop="requests" label="请求数" width="100" align="right" header-align="left" />
        <el-table-column label="总输入" width="120" align="right" header-align="left">
          <template #default="{ row }"><span class="mono">{{ fmt(row.prompt_tokens) }}</span></template>
        </el-table-column>
        <el-table-column label="输出" width="120" align="right" header-align="left">
          <template #default="{ row }"><span class="mono">{{ fmt(row.completion_tokens) }}</span></template>
        </el-table-column>
        <el-table-column label="费用 (USD)" width="160" align="right" header-align="left">
          <template #default="{ row }"><span class="mono">${{ (row.cost || 0).toFixed(6) }}</span></template>
        </el-table-column>
      </el-table>
      <div class="card-list">
        <div v-for="row in byModel" :key="row.model" class="card-row">
          <div class="row-head">
            <span class="row-title mono">{{ row.model }}</span>
            <span class="mono">${{ (row.cost || 0).toFixed(6) }}</span>
          </div>
          <div class="field"><span class="k">请求数</span><span class="v">{{ fmt(row.requests) }}</span></div>
          <div class="field"><span class="k">总输入 / 输出</span><span class="v">{{ fmt(row.prompt_tokens) }} / {{ fmt(row.completion_tokens) }}</span></div>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { get } from '../api'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { PieChart, LineChart, BarChart } from 'echarts/charts'
import {
  TitleComponent,
  TooltipComponent,
  LegendComponent,
  GridComponent
} from 'echarts/components'
import VChart from 'vue-echarts'

use([
  CanvasRenderer,
  PieChart,
  LineChart,
  BarChart,
  TitleComponent,
  TooltipComponent,
  LegendComponent,
  GridComponent
])

const days = ref(7)
const daily = ref([])
const byModel = ref([])
const hovered = ref(null)

const fmt = (n) => (n ?? 0).toLocaleString()

const COLOR = {
  green: '#4ade80',
  amber: '#f59e0b',
  dim: '#9ca3af',
  purple: '#c084fc',
  blue: '#5aa9e6',
  red: '#f87171'
}

const totals = computed(() => {
  let requests = 0, cost = 0, input = 0, output = 0, cache_read = 0, cache_write = 0
  for (const d of daily.value) {
    requests += Number(d.requests) || 0
    cost += Number(d.cost) || 0
    input += Number(d.prompt_tokens) || 0
    output += Number(d.completion_tokens) || 0
    cache_read += Number(d.cache_read_tokens) || 0
    cache_write += Number(d.cache_write_tokens) || 0
  }
  const uncached = Math.max(0, input - cache_read - cache_write)
  return { requests, cost, input, output, cache_read, cache_write, uncached }
})

function buildBreakdown(segments, total) {
  if (total <= 0) return { total: 0, segments: [] }
  const out = []
  for (const seg of segments) {
    const value = Math.max(0, Number(seg.value) || 0)
    const percent = total > 0 ? (value / total) * 100 : 0
    out.push({
      key: seg.key,
      label: seg.label,
      value,
      percent,
      color: seg.color
    })
  }
  return { total, segments: out }
}

const inputBreakdown = computed(() => {
  const t = totals.value
  return buildBreakdown([
    { key: 'cache_write', label: '缓存写', value: t.cache_write, color: COLOR.amber },
    { key: 'cache_read',  label: '缓存读', value: t.cache_read,  color: COLOR.green },
    { key: 'uncached',    label: '未缓存输入', value: t.uncached, color: COLOR.dim }
  ], t.input)
})

const ioBreakdown = computed(() => {
  const t = totals.value
  return buildBreakdown([
    { key: 'input',  label: '输入', value: t.input,  color: COLOR.green },
    { key: 'output', label: '输出', value: t.output, color: COLOR.purple }
  ], t.input + t.output)
})

const basePieOption = computed(() => ({
  tooltip: {
    trigger: 'item',
    confine: true,
    backgroundColor: '#0f1519',
    borderColor: '#1f2937',
    textStyle: { color: '#c9d4cb', fontFamily: 'ui-monospace, monospace', fontSize: 12 },
    formatter: (p) => `${p.marker} ${p.name}: ${fmt(p.value)} (${p.percent}%)`
  },
  legend: { show: false }
}))

const inputPieOption = computed(() => ({
  ...basePieOption.value,
  series: [{
    type: 'pie',
    radius: ['55%', '78%'],
    center: ['50%', '50%'],
    avoidLabelOverlap: false,
    itemStyle: {
      borderColor: '#080b0d',
      borderWidth: 2
    },
    label: {
      show: true,
      position: 'center',
      formatter: () => `{a|${fmt(totals.value.input)}}\n{b|tokens}`,
      rich: {
        a: { color: '#c9d4cb', fontSize: 16, fontWeight: 600, fontFamily: 'ui-monospace, monospace', lineHeight: 20 },
        b: { color: '#6b7280', fontSize: 10, fontFamily: 'ui-monospace, monospace', lineHeight: 14 }
      }
    },
    emphasis: {
      scale: true,
      scaleSize: 6,
      label: { show: false }
    },
    data: inputBreakdown.value.segments.map(s => ({
      name: s.label,
      value: s.value,
      itemStyle: { color: s.color }
    }))
  }]
}))

const ioPieOption = computed(() => ({
  ...basePieOption.value,
  series: [{
    type: 'pie',
    radius: ['55%', '78%'],
    center: ['50%', '50%'],
    avoidLabelOverlap: false,
    itemStyle: {
      borderColor: '#080b0d',
      borderWidth: 2
    },
    label: {
      show: true,
      position: 'center',
      formatter: () => `{a|${fmt(totals.value.input + totals.value.output)}}\n{b|tokens}`,
      rich: {
        a: { color: '#c9d4cb', fontSize: 16, fontWeight: 600, fontFamily: 'ui-monospace, monospace', lineHeight: 20 },
        b: { color: '#6b7280', fontSize: 10, fontFamily: 'ui-monospace, monospace', lineHeight: 14 }
      }
    },
    emphasis: {
      scale: true,
      scaleSize: 6,
      label: { show: false }
    },
    data: ioBreakdown.value.segments.map(s => ({
      name: s.label,
      value: s.value,
      itemStyle: { color: s.color }
    }))
  }]
}))

function formatDay(s) {
  if (!s) return ''
  const m = String(s).match(/^(\d{4})-(\d{2})-(\d{2})/)
  return m ? `${m[2]}-${m[3]}` : s
}

const sortedDaily = computed(() =>
  [...daily.value].sort((a, b) => String(a.day).localeCompare(String(b.day)))
)

const tokenChartOption = computed(() => {
  const ds = sortedDaily.value
  const dates = ds.map(d => formatDay(d.day))
  return {
    tooltip: {
      trigger: 'axis',
      backgroundColor: '#0f1519',
      borderColor: '#1f2937',
      textStyle: { color: '#c9d4cb', fontFamily: 'ui-monospace, monospace', fontSize: 12 },
      axisPointer: { type: 'line', lineStyle: { color: '#4b5563', type: 'dashed' } }
    },
    legend: {
      data: ['总输入', '总输出'],
      textStyle: { color: '#c9d4cb', fontFamily: 'ui-monospace, monospace', fontSize: 11 },
      top: 4,
      right: 8,
      icon: 'circle',
      itemWidth: 8,
      itemHeight: 8
    },
    grid: { left: 50, right: 16, top: 36, bottom: 28 },
    xAxis: {
      type: 'category',
      data: dates,
      axisLine: { lineStyle: { color: '#374151' } },
      axisTick: { show: false },
      axisLabel: { color: '#6b7280', fontFamily: 'ui-monospace, monospace', fontSize: 10 }
    },
    yAxis: {
      type: 'value',
      axisLine: { show: false },
      axisTick: { show: false },
      splitLine: { lineStyle: { color: '#1f2937', type: 'dashed' } },
      axisLabel: {
        color: '#6b7280',
        fontFamily: 'ui-monospace, monospace',
        fontSize: 10,
        formatter: (v) => v >= 1000 ? (v / 1000).toFixed(1) + 'k' : v
      }
    },
    series: [
      {
        name: '总输入',
        type: 'line',
        smooth: true,
        symbol: 'circle',
        symbolSize: 6,
        showSymbol: true,
        lineStyle: { width: 2, color: COLOR.green },
        itemStyle: { color: COLOR.green, borderColor: '#080b0d', borderWidth: 2 },
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(74, 222, 128, 0.35)' },
              { offset: 1, color: 'rgba(74, 222, 128, 0.02)' }
            ]
          }
        },
        data: ds.map(d => Number(d.prompt_tokens) || 0)
      },
      {
        name: '总输出',
        type: 'line',
        smooth: true,
        symbol: 'circle',
        symbolSize: 6,
        showSymbol: true,
        lineStyle: { width: 2, color: COLOR.purple },
        itemStyle: { color: COLOR.purple, borderColor: '#080b0d', borderWidth: 2 },
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(192, 132, 252, 0.30)' },
              { offset: 1, color: 'rgba(192, 132, 252, 0.02)' }
            ]
          }
        },
        data: ds.map(d => Number(d.completion_tokens) || 0)
      }
    ]
  }
})

const costChartOption = computed(() => {
  const ds = sortedDaily.value
  const dates = ds.map(d => formatDay(d.day))
  const costs = ds.map(d => Number(d.cost) || 0)
  return {
    tooltip: {
      trigger: 'axis',
      backgroundColor: '#0f1519',
      borderColor: '#1f2937',
      textStyle: { color: '#c9d4cb', fontFamily: 'ui-monospace, monospace', fontSize: 12 },
      axisPointer: { type: 'shadow', shadowStyle: { color: 'rgba(74, 222, 128, 0.08)' } },
      formatter: (params) => {
        const p = params[0]
        return `${p.axisValue}<br/>${p.marker} ${p.seriesName}: $${p.value.toFixed(6)}`
      }
    },
    grid: { left: 64, right: 16, top: 16, bottom: 28 },
    xAxis: {
      type: 'category',
      data: dates,
      axisLine: { lineStyle: { color: '#374151' } },
      axisTick: { show: false },
      axisLabel: { color: '#6b7280', fontFamily: 'ui-monospace, monospace', fontSize: 10 }
    },
    yAxis: {
      type: 'value',
      axisLine: { show: false },
      axisTick: { show: false },
      splitLine: { lineStyle: { color: '#1f2937', type: 'dashed' } },
      axisLabel: {
        color: '#6b7280',
        fontFamily: 'ui-monospace, monospace',
        fontSize: 10,
        formatter: (v) => {
          if (v === 0) return '$0'
          if (v < 0.0001) return '$' + v.toExponential(1)
          if (v < 1) return '$' + v.toFixed(4)
          return '$' + v.toFixed(2)
        }
      }
    },
    series: [{
      name: '费用',
      type: 'bar',
      data: costs,
      barMaxWidth: 32,
      itemStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: '#4ade80' },
            { offset: 1, color: 'rgba(74, 222, 128, 0.25)' }
          ]
        },
        borderRadius: [3, 3, 0, 0]
      },
      emphasis: {
        itemStyle: { color: '#86efac' }
      }
    }]
  }
})

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

async function load() {
  const data = await get(`/api/user/stats?days=${days.value}`)
  daily.value = padDaily(data.daily, days.value)
  byModel.value = data.by_model || []
}
onMounted(load)
</script>

<style scoped>
.page-head-card { margin-bottom: 0; }

.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  width: 100%;
}
.page-title {
  color: var(--th-fg);
  font-weight: 600;
}
.page-head-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.summary {
  display: flex;
  gap: 28px;
  padding: 4px 2px 2px;
  flex-wrap: wrap;
}
.summary-item { display: flex; flex-direction: column; gap: 4px; }
.summary-label {
  font-size: 11px;
  color: var(--th-fg-dim);
  letter-spacing: 0.04em;
}
.summary-value {
  font-size: 18px;
  font-weight: 600;
  color: var(--th-green);
}

.charts {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
  margin-bottom: 14px;
}
@media (max-width: 900px) {
  .charts { grid-template-columns: 1fr; }
}
.chart-card :deep(.el-card__body) { padding: 16px; }
.card-head-meta {
  font-size: 11px;
  color: var(--th-fg-dim);
  font-family: ui-monospace, monospace;
}

.chart-body {
  display: grid;
  grid-template-columns: 180px 1fr;
  gap: 18px;
  align-items: center;
}
@media (max-width: 520px) {
  .chart-body { grid-template-columns: 1fr; }
}

.pie-wrap {
  width: 180px;
  height: 180px;
  overflow: visible;
}
.pie {
  width: 100%;
  height: 100%;
  overflow: visible;
}

.legend {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}
.legend li {
  display: grid;
  grid-template-columns: 12px 1fr auto auto;
  gap: 8px;
  align-items: center;
  font-size: 12.5px;
  padding: 4px 6px;
  border-radius: 3px;
  cursor: pointer;
  transition: background 0.1s, opacity 0.15s;
}
.legend li:hover { background: rgba(255,255,255,0.04); }
.legend .dot {
  width: 10px;
  height: 10px;
  border-radius: 2px;
  display: inline-block;
}
.legend-label { color: var(--th-fg); }
.legend-value {
  color: var(--th-fg-dim);
  font-family: ui-monospace, monospace;
}
.legend-pct {
  color: var(--th-green);
  font-family: ui-monospace, monospace;
  font-weight: 600;
  min-width: 48px;
  text-align: right;
}

.trend-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
  margin-bottom: 14px;
}
@media (max-width: 900px) {
  .trend-row { grid-template-columns: 1fr; }
}
.trend-wrap {
  width: 100%;
  height: 240px;
}
.chart {
  width: 100%;
  height: 100%;
}

.empty {
  color: var(--th-fg-dim);
  font-size: 12px;
  font-family: ui-monospace, monospace;
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
}
</style>
