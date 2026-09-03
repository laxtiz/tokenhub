<template>
  <div>
    <el-card>
      <template #header>
        <div class="card-head-flex">
          <span>我的 API Key</span>
          <el-button type="primary" @click="createKey">新建 Key</el-button>
        </div>
      </template>

      <el-alert v-if="newPlain" type="success" :closable="false" style="margin-bottom:16px">
        <p style="margin:0 0 6px">
          <template v-if="newPlainTitle">{{ newPlainTitle }}</template>
          <template v-else>Key 创建成功，<b>仅此一次显示</b>，请立即复制保存：</template>
        </p>
        <el-input :model-value="newPlain" readonly>
          <template #append>
            <el-button @click="copy">复制</el-button>
          </template>
        </el-input>
      </el-alert>

      <el-table :data="keys" stripe>
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="name" label="名称" min-width="160" />
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
        <el-table-column label="操作" width="240" fixed="right">
          <template #default="{ row }">
            <div style="display:flex;gap:6px;align-items:center;white-space:nowrap">
              <el-button size="small" @click="renameKey(row)">重命名</el-button>
              <el-popconfirm title="撤销将立即作废旧 Key，确认？" @confirm="revokeKey(row)">
                <template #reference><el-button size="small" type="warning">撤销</el-button></template>
              </el-popconfirm>
              <el-popconfirm title="删除后不可恢复，确认？" @confirm="removeKey(row)">
                <template #reference><el-button size="small" type="danger">删除</el-button></template>
              </el-popconfirm>
            </div>
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
            <el-button size="small" @click="renameKey(row)">重命名</el-button>
            <el-popconfirm title="撤销将立即作废旧 Key，确认？" @confirm="revokeKey(row)">
              <template #reference><el-button size="small" type="warning">撤销</el-button></template>
            </el-popconfirm>
            <el-popconfirm title="删除后不可恢复，确认？" @confirm="removeKey(row)">
              <template #reference><el-button size="small" type="danger">删除</el-button></template>
            </el-popconfirm>
          </div>
        </div>
      </div>
    </el-card>

    <el-card>
      <template #header>
        <div class="card-head-flex">
          <span>调用示例</span>
          <span class="card-head-meta">用 Key 接入 TokenHub 网关</span>
        </div>
      </template>
      <div class="examples">
        <div class="ex-block">
          <div class="ex-title">OpenAI 接入地址</div>
          <pre class="ex-code">{{ examples.openai }}</pre>
        </div>
        <div class="ex-block">
          <div class="ex-title">Anthropic 接入地址</div>
          <pre class="ex-code">{{ examples.anthropic }}</pre>
        </div>
        <div class="ex-block">
          <div class="ex-title">查看模型列表</div>
          <pre class="ex-code">{{ examples.models }}</pre>
        </div>
      </div>
    </el-card>

    <el-card>
      <template #header>
        <div class="card-head-flex">
          <span>MCP 接入指南</span>
          <span class="card-head-meta">让 AI Agent 查询你的数据</span>
        </div>
      </template>
      <div class="examples">
        <div class="ex-block">
          <div class="ex-title">MCP 接入地址</div>
          <pre class="ex-code">{{ host }}/mcp</pre>
        </div>
        <div class="ex-block">
          <div class="ex-title">Claude Desktop / Cline / Cursor 等</div>
          <pre class="ex-code">{{ examples.mcpClient }}</pre>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { get, post, patch, del, fmtTime } from '../api'

const keys = ref([])
const newPlain = ref('')
const newPlainTitle = ref('')

// 后端 RFC3339 字符串带服务器时区偏移，按浏览器本地时区渲染
const host = window.location.origin

const examples = computed(() => {
  const key = newPlain.value || '<TOKENHUB_API_KEY>'
  return {
    openai: `${host}/v1`,

    anthropic: `${host}`,

    models: `curl -H "Authorization: Bearer ${key}" ${host}/v1/models`,

    mcpClient: `{
  "mcpServers": {
    "tokenhub": {
      "url": "${host}/mcp",
      "headers": {
        "Authorization": "Bearer ${key}"
      }
    }
  }
}`
  }
})

async function load() {
  keys.value = await get('/api/user/keys')
}

async function createKey() {
  const { value } = await ElMessageBox.prompt('请输入 Key 名称', '新建 Key', { inputValue: 'default' })
  const data = await post('/api/user/keys', { name: value })
  newPlainTitle.value = ''
  newPlain.value = data.plain_key
  load()
}

async function renameKey(row) {
  const { value } = await ElMessageBox.prompt('请输入新名称', '重命名 Key', { inputValue: row.name })
  if (!value || value === row.name) return
  await patch(`/api/user/keys/${row.id}`, { name: value })
  ElMessage.success('已重命名')
  load()
}

async function revokeKey(row) {
  const data = await post(`/api/user/keys/${row.id}/revoke`)
  newPlainTitle.value = 'Key 已撤销，旧值立即失效。明文仅此一次显示，请立即复制保存：'
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

onMounted(load)
</script>

<style scoped>
.examples { display: flex; flex-direction: column; gap: 14px; }
.ex-title {
  font-size: 12px;
  color: var(--th-fg-dim);
  margin-bottom: 6px;
}
.ex-title::before { content: '# '; color: var(--th-fg-dim); margin-right: 2px; }
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
