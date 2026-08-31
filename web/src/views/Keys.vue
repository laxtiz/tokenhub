<template>
  <div>
    <el-card>
      <template #header>
        <div style="display:flex;justify-content:space-between;align-items:center">
          <span>我的 API Key</span>
          <el-button type="primary" @click="createKey">新建 Key</el-button>
        </div>
      </template>

      <el-alert v-if="newPlain" type="success" :closable="false" style="margin-bottom:16px">
        <p style="margin:0 0 6px">Key 创建成功，<b>仅此一次显示</b>，请立即复制保存：</p>
        <el-input :model-value="newPlain" readonly>
          <template #append>
            <el-button @click="copy">复制</el-button>
          </template>
        </el-input>
      </el-alert>

      <el-table :data="keys" stripe>
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="name" label="名称" width="180" />
        <el-table-column label="Key" width="160">
          <template #default="{ row }"><span style="font-family:monospace">{{ row.key_prefix }}****</span></template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.disabled ? 'danger' : 'success'">{{ row.disabled ? '禁用' : '正常' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最后使用" width="150">
          <template #default="{ row }">{{ fmtTime(row.last_used_at) }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="150">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" min-width="120">
          <template #default="{ row }">
            <el-popconfirm title="删除后不可恢复，确认？" @confirm="removeKey(row)">
              <template #reference><el-button size="small" type="danger">删除</el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
      <div class="card-list">
        <div v-for="row in keys" :key="row.id" class="card-row">
          <div class="row-head">
            <span class="row-title">{{ row.name }} <span style="color:var(--th-fg-dim);font-weight:400">#{{ row.id }}</span></span>
            <el-tag size="small" :type="row.disabled ? 'danger' : 'success'">{{ row.disabled ? '禁用' : '正常' }}</el-tag>
          </div>
          <div class="field"><span class="k">Key</span><span class="v mono">{{ row.key_prefix }}****</span></div>
          <div class="field"><span class="k">最后使用</span><span class="v">{{ fmtTime(row.last_used_at) }}</span></div>
          <div class="field"><span class="k">创建时间</span><span class="v">{{ fmtTime(row.created_at) }}</span></div>
          <div class="row-actions">
            <el-popconfirm title="删除后不可恢复，确认？" @confirm="removeKey(row)">
              <template #reference><el-button size="small" type="danger">删除</el-button></template>
            </el-popconfirm>
          </div>
        </div>
      </div>

      <el-divider />
      <h4>调用示例</h4>
      <div class="examples">
        <div class="ex-block">
          <div class="ex-title">OpenAI 兼容</div>
          <pre class="ex-code">{{ examples.openai }}</pre>
        </div>
        <div class="ex-block">
          <div class="ex-title">Anthropic 兼容</div>
          <pre class="ex-code">{{ examples.anthropic }}</pre>
        </div>
        <div class="ex-block">
          <div class="ex-title">查看模型列表</div>
          <pre class="ex-code">{{ examples.models }}</pre>
        </div>
      </div>

      <el-divider />
      <h4>MCP 接入指南（让 AI Agent 查询你的数据）</h4>
      <p style="color:var(--th-fg-dim);font-size:12px;margin:0 0 12px">
        TokenHub 提供 6 个只读工具：<code>list_models</code>、<code>list_my_keys</code>、<code>list_my_logs</code>、<code>get_trace_detail</code>、<code>get_my_stats</code>、<code>get_my_account</code>。
        Agent 通过 <code>Authorization: Bearer th-xxx</code> 接入，只能看到该 Key 所属用户的数据。
      </p>
      <div class="examples">
        <div class="ex-block">
          <div class="ex-title">Claude Desktop / Cline / Cursor 等</div>
          <pre class="ex-code">{{ examples.mcpClient }}</pre>
        </div>
        <div class="ex-block">
          <div class="ex-title">用 curl 验证握手</div>
          <pre class="ex-code">{{ examples.mcpCurl }}</pre>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { get, post, del } from '../api'

const keys = ref([])
const newPlain = ref('')

// 简化时间戳：2026-08-31T03:02:33.228+08:00 → 2026-08-31 03:02
const fmtTime = (t) => (t ? t.slice(0, 16).replace('T', ' ') : '-')

const host = window.location.origin

const examples = computed(() => {
  const key = newPlain.value || 'th-你的key'
  return {
    openai: `base_url = ${host}/v1`,

    anthropic: `base_url = ${host}`,

    models: `curl ${host}/v1/models \
  -H "Authorization: Bearer ${key}"`,

    mcpClient: `{
  "mcpServers": {
    "tokenhub": {
      "url": "${host}/mcp",
      "headers": {
        "Authorization": "Bearer ${key}"
      }
    }
  }
}`,

    mcpCurl: `# initialize 握手
curl -X POST ${host}/mcp \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -H "Accept: application/json, text/event-stream" \\
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'

# 列出可用工具
curl -X POST ${host}/mcp \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -H "Accept: application/json, text/event-stream" \\
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'`
  }
})

async function load() {
  keys.value = await get('/api/user/keys')
}

async function createKey() {
  const { value } = await ElMessageBox.prompt('请输入 Key 名称', '新建 Key', { inputValue: 'default' })
  const data = await post('/api/user/keys', { name: value })
  newPlain.value = data.plain_key
  load()
}

async function removeKey(row) {
  await del(`/api/user/keys/${row.id}`)
  load()
}

async function copy() {
  await navigator.clipboard.writeText(newPlain.value)
  ElMessage.success('已复制')
}

import { ElMessageBox } from 'element-plus'
onMounted(load)
</script>

<style scoped>
.examples { display: flex; flex-direction: column; gap: 14px; }
.ex-title {
  font-size: 12px;
  color: var(--th-fg-dim);
  margin-bottom: 6px;
}
.ex-title::before { content: '▍'; color: var(--th-green); margin-right: 6px; }
.ex-code {
  margin: 0;
  padding: 12px 14px;
  background: #0a0d10;
  border: 1px solid var(--th-border);
  border-radius: 6px;
  font-family: inherit;
  font-size: 12px;
  line-height: 1.7;
  color: var(--th-fg);
  overflow-x: auto;
  white-space: pre;
}
</style>
