<template>
  <div>
    <el-card>
      <template #header>
        <div class="card-head-flex">
          <span>用户管理</span>
          <el-button type="primary" @click="openForm(null)">新建用户</el-button>
        </div>
      </template>
      <el-table :data="users" stripe>
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="username" label="用户名" min-width="140" />
        <el-table-column prop="role" label="角色" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="row.role === 'admin' ? 'danger' : ''">{{ row.role }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="累计消费" width="140">
          <template #default="{ row }">${{ (row.spend || 0).toFixed(6) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.disabled ? 'danger' : 'success'">{{ row.disabled ? '禁用' : '正常' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="openForm(row)">编辑</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="card-list">
        <div v-for="row in users" :key="row.id" class="card-row">
          <div class="row-head">
            <span class="row-title">{{ row.username }} <span style="color:var(--th-fg-dim);font-weight:400">#{{ row.id }}</span></span>
            <el-tag size="small" :type="row.role === 'admin' ? 'danger' : ''">{{ row.role }}</el-tag>
          </div>
          <div class="field"><span class="k">累计消费</span><span class="v">${{ (row.spend || 0).toFixed(6) }}</span></div>
          <div class="field"><span class="k">状态</span><span class="v"><el-tag size="small" :type="row.disabled ? 'danger' : 'success'">{{ row.disabled ? '禁用' : '正常' }}</el-tag></span></div>
          <div class="field"><span class="k">创建时间</span><span class="v">{{ fmtTime(row.created_at) }}</span></div>
          <div class="row-actions">
            <el-button size="small" @click="openForm(row)">编辑</el-button>
          </div>
        </div>
      </div>
    </el-card>

    <el-dialog v-model="formVisible" :title="form.id ? `编辑用户 - ${form.username}` : '新建用户'" width="440px">
      <el-form :model="form" label-width="90px">
        <el-form-item v-if="!form.id" label="用户名"><el-input v-model="form.username" /></el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" show-password
            :placeholder="form.id ? '留空则不修改' : ''" />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="form.role">
            <el-option value="user" label="普通用户" />
            <el-option value="admin" label="管理员" />
          </el-select>
        </el-form-item>
        <el-form-item label="禁用"><el-switch v-model="form.disabled" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { get, post, patch } from '../api'

const users = ref([])
const formVisible = ref(false)
const form = ref({})

// 简化时间戳：2026-08-31T03:02:33.228+08:00 → 2026-08-31 03:02
const fmtTime = (t) => (t ? t.slice(0, 16).replace('T', ' ') : '-')

async function load() {
  users.value = await get('/api/admin/users')
}

function openForm(row) {
  form.value = row ? { id: row.id, username: row.username, role: row.role, disabled: row.disabled, password: '' }
    : { username: '', password: '', role: 'user', disabled: false }
  formVisible.value = true
}

async function save() {
  try {
    if (form.value.id) {
      const body = { role: form.value.role, disabled: form.value.disabled }
      if (form.value.password) body.password = form.value.password
      await patch(`/api/admin/users/${form.value.id}`, body)
    } else {
      await post('/api/admin/users', form.value)
    }
    formVisible.value = false
    ElMessage.success('已保存')
    load()
  } catch (e) { ElMessage.error(e.message) }
}

onMounted(load)
</script>
