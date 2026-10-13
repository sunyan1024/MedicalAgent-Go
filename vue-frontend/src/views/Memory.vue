<template>
  <main class="memory-page">
    <header class="memory-header">
      <div><el-button @click="$router.push('/menu')">返回菜单</el-button><h1>我的长期记忆</h1></div>
      <el-button type="primary" :disabled="!loaded || loading" @click="openEditor">编辑记忆</el-button>
    </header>
    <p class="muted">医疗 Agent 会跨会话使用这份记忆，并在对话中维护你的回答偏好和健康背景。你也可以在这里更正或删除内容。</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <div class="memory-meta">
      <span>更新时间：{{ updatedAt }}</span>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>
    <section v-loading="loading" class="memory-document">
      <article v-if="doc.contentMarkdown" class="markdown-body" v-html="renderMarkdown(doc.contentMarkdown)"></article>
      <el-empty v-else-if="loaded" description="还没有长期记忆，可以在聊天中告诉 Agent，或点击编辑添加。" />
    </section>
    <p class="muted">删除或清空此文档只影响长期记忆，原聊天和执行记录仍然保留。</p>
    <el-drawer v-model="editing" title="编辑长期记忆" size="min(720px, 100%)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" :before-close="closeEditor">
      <p class="muted">请区分本人和家属。建议使用“回答偏好”“本人健康背景”“家属情况”三个章节。</p>
      <el-alert v-if="saveError" :title="saveError" type="error" :closable="false" show-icon />
      <div v-if="conflict" class="conflict-panel">
        <p>本地草稿已保留。请读取最新文档，合并其他会话的修改后再保存。</p>
        <el-button :loading="loading" @click="reloadForMerge">读取最新文档以合并</el-button>
      </div>
      <details v-if="mergeRequired" open class="conflict-panel">
        <summary>最新已保存文档（请与下方草稿合并）</summary>
        <pre>{{ doc.contentMarkdown || '（空文档）' }}</pre>
        <el-checkbox v-model="merged">已核对并合并最新内容</el-checkbox>
      </details>
      <el-input v-model="draft" type="textarea" :rows="20" :disabled="saving" aria-label="记忆 Markdown 文档" />
      <p :class="{ invalid: count > 8000 }">{{ count }} / 8000 字符</p>
      <template #footer>
        <el-button type="danger" plain :disabled="saving || conflict || mergeRequired || loading" @click="clear">清空记忆</el-button>
        <el-button :disabled="saving" @click="editing = false">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="count > 8000 || conflict || (mergeRequired && !merged) || loading" @click="save(draft)">保存</el-button>
      </template>
    </el-drawer>
  </main>
</template>

<script>
export default { name: 'UserMemoryView' }
</script>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api, { getApiErrorMessage } from '../utils/api'
import { renderMarkdown } from '../utils/markdown'

const doc = ref({ contentMarkdown: '', version: 0, updatedAt: null })
const loaded = ref(false)
const loading = ref(false)
const saving = ref(false)
const editing = ref(false)
const error = ref('')
const saveError = ref('')
const draft = ref('')
const expectedVersion = ref(0)
const conflict = ref(false)
const mergeRequired = ref(false)
const merged = ref(false)
const count = computed(() => Array.from(draft.value).length)
const updatedAt = computed(() => doc.value.version > 0 ? new Date(doc.value.updatedAt).toLocaleString() : '尚未保存')

async function load() {
  if (loading.value) return false
  loading.value = true
  error.value = ''
  try {
    doc.value = (await api.get('/memory')).data
    loaded.value = true
    return true
  } catch (e) {
    error.value = getApiErrorMessage(e, '读取记忆失败，请重试')
    return false
  } finally { loading.value = false }
}
function openEditor() {
  draft.value = doc.value.contentMarkdown
  expectedVersion.value = doc.value.version
  saveError.value = ''
  conflict.value = false
  mergeRequired.value = false
  merged.value = false
  editing.value = true
}
function closeEditor(done) { if (!saving.value) done() }
async function reloadForMerge() {
  if (await load()) {
    expectedVersion.value = doc.value.version
    conflict.value = false
    mergeRequired.value = true
    merged.value = false
    saveError.value = ''
  } else { saveError.value = error.value }
}
async function save(content) {
  if (saving.value || conflict.value || (mergeRequired.value && !merged.value) || Array.from(content).length > 8000) return
  saving.value = true
  saveError.value = ''
  try {
    doc.value = (await api.put('/memory', { contentMarkdown: content, expectedVersion: expectedVersion.value })).data
    editing.value = false
    ElMessage.success(content ? '记忆已保存' : '长期记忆已清空')
  } catch (e) {
    conflict.value = e.response?.status === 409
    saveError.value = getApiErrorMessage(e, '保存失败，请重试')
  } finally { saving.value = false }
}
async function clear() {
  try {
    await ElMessageBox.confirm('清空长期记忆？原聊天和执行记录仍会保留。', '清空记忆', { type: 'warning', confirmButtonText: '清空', cancelButtonText: '取消' })
  } catch { return }
  draft.value = ''
  await save('')
}
onMounted(load)
</script>

<style scoped>
.memory-page { max-width: 960px; margin: 0 auto; padding: 28px 24px; color: var(--app-text); }
.memory-header, .memory-meta { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.memory-header h1 { margin-bottom: 8px; font-size: 26px; }
.memory-meta { margin: 24px 0 12px; font-size: 14px; }
.muted { color: var(--app-text-muted); line-height: 1.7; }
.memory-document { min-height: 260px; padding: 24px; background: var(--app-surface); border: 1px solid var(--app-border); border-radius: 12px; overflow-wrap: anywhere; }
.markdown-body { line-height: 1.8; }
.markdown-body :deep(pre), .conflict-panel pre { white-space: pre-wrap; overflow-wrap: anywhere; }
.conflict-panel { padding: 12px; margin: 12px 0; border: 1px solid #e6a23c; border-radius: 8px; }
.invalid { color: #f56c6c; }
</style>
