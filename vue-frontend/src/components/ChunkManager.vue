<template>
  <el-drawer
    class="chunk-drawer" :model-value="modelValue" size="min(1200px, 96vw)"
    :before-close="beforeClose" @open="drawerReady = false" @opened="drawerReady = true"
    @closed="drawerReady = false" @update:model-value="$emit('update:modelValue', $event)"
  >
    <template #header="{ titleId, titleClass }">
      <div class="manager-heading">
        <span class="document-mark"><el-icon><Document /></el-icon></span>
        <div class="heading-copy">
          <span class="heading-eyebrow">医学知识库 · 内容管理</span>
          <h2 :id="titleId" :class="titleClass">切片管理 <span class="heading-divider">/</span> <span class="heading-filename" :title="file?.original_name">{{ file?.original_name }}</span></h2>
        </div>
      </div>
    </template>
    <div class="chunk-manager">
      <div class="shared-notice"><el-icon><InfoFilled /></el-icon><p>修改将同步到所有引用此文件的知识库。<span>原始文件保持不变，禁用的切片可随时恢复。</span></p></div>
      <div v-if="drawerReady" class="chunk-workspace">
        <section class="chunk-sidebar" aria-label="切片列表">
          <div class="sidebar-toolbar">
            <div class="section-heading"><h3>文档切片</h3><el-button :icon="Plus" type="primary" size="small" :disabled="busy || detailLoading" @click="newChunk">新增切片</el-button></div>
            <form class="chunk-search" @submit.prevent="search">
              <el-input v-model="keyword" placeholder="搜索正文内容" clearable :maxlength="200" :prefix-icon="Search" aria-label="搜索切片正文" />
              <el-button native-type="submit" :disabled="busy || loading">搜索</el-button>
            </form>
            <div class="filter-toolbar">
              <div class="status-filters" role="group" aria-label="切片状态筛选">
                <button v-for="filter in filters" :key="filter.value" type="button" :class="{ active: enabledFilter === filter.value }" :aria-pressed="enabledFilter === filter.value" :disabled="busy || loading" @click="chooseFilter(filter.value)">{{ filter.label }}</button>
              </div>
              <el-button :icon="Refresh" text circle aria-label="刷新切片列表" title="刷新切片列表" :disabled="busy || loading" @click="loadList" />
            </div>
            <p class="result-count" aria-live="polite">{{ loading ? '正在获取切片…' : `筛选结果 ${total} 个切片` }}</p>
          </div>
          <div class="chunk-list" :aria-busy="loading">
            <div v-if="loading" class="list-skeleton"><el-skeleton :rows="6" animated /></div>
            <div v-else-if="listError" class="list-empty"><el-icon><Warning /></el-icon><h4>暂时无法加载切片</h4><p>{{ listError }}</p><el-button @click="loadList">重新加载</el-button></div>
            <template v-else-if="chunks.length">
              <button v-for="row in chunks" :key="row.id" type="button" class="chunk-card" :class="{ selected: selected?.id === row.id, 'is-disabled': !row.enabled }" :aria-pressed="selected?.id === row.id" :aria-label="`查看切片 ${row.chunk_idx}，${row.enabled ? '已启用' : '已禁用'}`" :disabled="busy || detailLoading" @click="openChunk(row)">
                <span class="card-heading"><span class="chunk-number"># {{ String(row.chunk_idx).padStart(2, '0') }}</span><span class="chunk-status" :class="{ enabled: row.enabled }"><i />{{ row.enabled ? '已启用' : '已禁用' }}</span></span>
                <span class="chunk-summary">{{ row.summary || row.content }}</span>
                <span class="card-footer"><span>{{ row.char_count }} 字<span class="meta-dot">·</span>{{ row.origin === 'manual' ? '人工维护' : '自动切分' }}</span><el-icon class="card-arrow"><ArrowRight /></el-icon></span>
              </button>
            </template>
            <div v-else class="list-empty"><el-icon><Search /></el-icon><h4>暂无切片结果</h4><p>尝试调整关键词或筛选条件，也可以新增切片。</p></div>
          </div>
          <div class="list-pagination"><el-pagination :current-page="page" :page-size="20" :total="total" :pager-count="5" small layout="prev, pager, next" :disabled="busy || loading" @current-change="changePage" /></div>
        </section>

        <section class="chunk-detail" aria-label="切片正文工作区" :aria-busy="detailLoading">
          <div v-if="!mode && !detailLoading" class="detail-empty">
            <span class="empty-mark"><el-icon><Document /></el-icon></span>
            <h3>让每一段知识清晰可用</h3><p>选择左侧切片，查看完整正文并维护内容。<br>也可以添加一段新的知识。</p>
            <el-button type="primary" :icon="Plus" :disabled="busy" @click="newChunk">新增切片</el-button>
            <span class="empty-caption">查看内容 · 精准编辑 · 控制检索范围</span>
            <el-alert v-if="actionError" :title="actionError" type="error" :closable="false" />
          </div>
          <div v-else-if="detailLoading" class="detail-skeleton"><el-skeleton :rows="12" animated /><p>正在加载完整正文…</p></div>
          <template v-else>
            <div class="detail-heading">
              <div><span class="heading-eyebrow">{{ mode === 'new' ? '添加知识' : mode === 'edit' ? '编辑内容' : '正文预览' }}</span><h3>{{ mode === 'new' ? '新增切片' : `切片 ${String(selected.chunk_idx).padStart(2, '0')}` }}<span v-if="selected" class="chunk-status" :class="{ enabled: selected.enabled }"><i />{{ selected.enabled ? '已启用' : '已禁用' }}</span></h3></div>
              <div v-if="mode === 'view'" class="detail-actions">
                <el-button :disabled="busy" @click="setEnabled(selected)">{{ selected.enabled ? '禁用' : '恢复启用' }}</el-button>
                <el-button type="primary" :icon="EditPen" :disabled="busy" @click="mode = 'edit'">编辑正文</el-button>
                <el-dropdown trigger="click" :disabled="busy" @command="removeChunk(selected)"><el-button :icon="MoreFilled" :disabled="busy" aria-label="更多切片操作" title="更多操作" /><template #dropdown><el-dropdown-menu><el-dropdown-item command="delete" :icon="Delete" class="chunk-delete-action">永久删除</el-dropdown-item></el-dropdown-menu></template></el-dropdown>
              </div>
            </div>
            <template v-if="mode === 'view'">
              <div class="reading-body">
                <p class="reading-meta">{{ selected.char_count }} 字<span class="meta-dot">·</span>{{ selected.origin === 'manual' ? '人工维护' : '自动切分' }}<span class="meta-dot">·</span>{{ selected.enabled ? '参与知识检索' : '暂不参与检索' }}</p>
                <pre class="chunk-content">{{ selected.content }}</pre>
                <details class="source-details"><summary>来源标识</summary><code>{{ selected.source_id }}</code></details>
              </div>
              <div class="detail-footer"><span><el-icon><InfoFilled /></el-icon>{{ selected.enabled ? '此切片可用于后续问答检索' : '此切片已禁用，恢复后可参与检索' }}</span></div>
            </template>
            <template v-else>
              <div class="writing-body"><label for="chunk-content-editor">切片正文</label><p>一段完整、清晰的内容，有助于准确检索。</p><el-input id="chunk-content-editor" v-model="content" class="content-input" type="textarea" resize="none" :disabled="busy" placeholder="在这里输入或粘贴切片正文…" aria-label="切片正文" /></div>
              <div v-if="mode === 'edit' && !selected.enabled" class="disabled-note">当前切片已禁用，保存后仍保持禁用。</div>
              <div class="editor-footer"><div class="editor-count"><span :class="{ invalid: charCount > 10000 }">{{ charCount.toLocaleString() }} / 10,000 字</span><span class="draft-state">{{ dirty ? '有未保存的修改' : '正文准备就绪' }}</span></div><div class="editor-buttons"><el-button v-if="mode === 'edit'" text :disabled="busy" @click="openChunk(selected)">重新加载</el-button><el-button :disabled="busy" @click="cancelEdit">取消</el-button><el-button type="primary" :loading="busy" :disabled="!content.trim() || charCount > 10000" @click="save">{{ busy ? '正在保存…' : '保存切片' }}</el-button></div></div>
            </template>
            <el-alert v-if="actionError" class="action-error" :title="actionError" type="error" :closable="false" />
          </template>
        </section>
      </div>
      <div v-else class="opening-placeholder"><el-skeleton :rows="8" animated /></div>
    </div>
  </el-drawer>
</template>

<script>
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Document, InfoFilled, Plus, Search, Refresh, Warning, ArrowRight, EditPen, MoreFilled, Delete } from '@element-plus/icons-vue'
import api, { getApiErrorMessage } from '../utils/api'

export default {
  name: 'ChunkManager',
  components: { Document, InfoFilled, Warning, Search, ArrowRight },
  props: { modelValue: Boolean, file: { type: Object, default: null } },
  emits: ['update:modelValue'],
  setup(props) {
    const filters = [{ label: '全部', value: 'all' }, { label: '启用', value: 'true' }, { label: '禁用', value: 'false' }]
    const drawerReady = ref(false)
    const chunks = ref([]), total = ref(0), page = ref(1)
    const keyword = ref(''), enabledFilter = ref('all')
    const loading = ref(false), busy = ref(false), detailLoading = ref(false)
    const listError = ref(''), actionError = ref('')
    const selected = ref(null), mode = ref(''), content = ref('')
    let generation = 0, listRequest = 0, detailRequest = 0
    // Filters are applied on Search, so paging uses the last submitted search.
    let appliedKeyword = '', appliedEnabled = 'all'
    const endpoint = () => `/enterprise/file/${encodeURIComponent(props.file.id)}/chunks`
    const charCount = computed(() => Array.from(content.value).length)
    const dirty = computed(() => mode.value === 'new' ? content.value.length > 0 : mode.value === 'edit' && content.value !== selected.value?.content)
    const unwrap = response => {
      if (response.data?.status_code !== 1000) throw new Error(response.data?.status_msg || '切片操作失败')
      return response.data
    }
    const resetEditor = () => { detailRequest++; detailLoading.value = false; selected.value = null; mode.value = ''; content.value = ''; actionError.value = '' }
    const canLeave = async () => {
      if (busy.value) { ElMessage.info('正在保存，请稍候'); return false }
      if (!dirty.value) return true
      try { await ElMessageBox.confirm('尚有未保存的正文，确定放弃修改吗？', '未保存修改', { confirmButtonText: '放弃修改', cancelButtonText: '继续编辑', type: 'warning' }); return true } catch { return false }
    }
    const beforeClose = async done => { const gen = generation; if (await canLeave() && gen === generation) done() }
    const loadList = async () => {
      if (!props.modelValue || !props.file) return
      const gen = generation, request = ++listRequest
      loading.value = true; listError.value = ''
      try {
        const params = { page: page.value, page_size: 20, keyword: appliedKeyword }
        if (appliedEnabled !== 'all') params.enabled = appliedEnabled
        const data = unwrap(await api.get(endpoint(), { params }))
        if (gen !== generation || request !== listRequest) return
        chunks.value = data.chunks; total.value = data.total
        const lastPage = Math.max(1, Math.ceil(total.value / 20))
        if (page.value > lastPage) { page.value = lastPage; await loadList() }
      } catch (error) { if (gen === generation && request === listRequest) { chunks.value = []; total.value = 0; listError.value = getApiErrorMessage(error, '获取切片失败') } }
      finally { if (gen === generation && request === listRequest) loading.value = false }
    }
    const search = () => { appliedKeyword = keyword.value; appliedEnabled = enabledFilter.value; page.value = 1; loadList() }
    const chooseFilter = value => { if (busy.value || loading.value) return; enabledFilter.value = value; search() }
    const changePage = value => { page.value = value; loadList() }
    const openChunk = async row => {
      const gen = generation
      if (!(await canLeave()) || gen !== generation) return
      const request = ++detailRequest
      detailLoading.value = true; actionError.value = ''
      try {
        const data = unwrap(await api.get(`${endpoint()}/${row.id}`))
        if (gen !== generation || request !== detailRequest) return
        selected.value = data.chunk; content.value = data.chunk.content; mode.value = 'view'
      } catch (error) { if (gen === generation && request === detailRequest) actionError.value = getApiErrorMessage(error, '获取切片失败') }
      finally { if (gen === generation && request === detailRequest) detailLoading.value = false }
    }
    const newChunk = async () => { const gen = generation; if (!(await canLeave()) || gen !== generation) return; resetEditor(); mode.value = 'new' }
    const cancelEdit = async () => { const gen = generation; if (!(await canLeave()) || gen !== generation) return; if (selected.value) { content.value = selected.value.content; mode.value = 'view'; actionError.value = '' } else resetEditor() }
    const mutate = async (request, onSuccess) => {
      if (busy.value) return
      const gen = generation
      busy.value = true; actionError.value = ''
      try {
        const data = unwrap(await request())
        if (gen !== generation) return
        onSuccess(data); ElMessage.success('操作成功'); await loadList()
      } catch (error) { if (gen === generation) actionError.value = getApiErrorMessage(error, '操作失败，请刷新后重试') }
      finally { if (gen === generation) busy.value = false }
    }
    const save = () => {
      if (!content.value.trim() || charCount.value > 10000) return
      const body = { content: content.value, version: selected.value?.version || 0 }
      return mutate(() => mode.value === 'new' ? api.post(endpoint(), body) : api.put(`${endpoint()}/${selected.value.id}`, body), data => { selected.value = data.chunk; content.value = data.chunk.content; mode.value = 'view' })
    }
    const setEnabled = async row => {
      const gen = generation
      if (!(await canLeave()) || gen !== generation) return
      await mutate(() => api.patch(`${endpoint()}/${row.id}`, { enabled: !row.enabled, version: row.version }), data => { selected.value = data.chunk; content.value = data.chunk.content; mode.value = 'view' })
    }
    const removeChunk = async row => {
      const gen = generation
      if (!(await canLeave()) || gen !== generation) return
      try { await ElMessageBox.confirm(`永久删除切片 ${row.chunk_idx}？所有引用此文件的知识库将不再检索此切片，此操作不可恢复。`, '删除切片', { confirmButtonText: '永久删除', cancelButtonText: '取消', type: 'warning' }) } catch { return }
      if (gen !== generation) return
      await mutate(() => api.delete(`${endpoint()}/${row.id}`, { data: { version: row.version } }), () => resetEditor())
    }
    watch(() => [props.modelValue, props.file?.id], () => {
      generation++; listRequest++; resetEditor(); loading.value = false; busy.value = false
      chunks.value = []; total.value = 0; page.value = 1; keyword.value = ''; enabledFilter.value = 'all'; appliedKeyword = ''; appliedEnabled = 'all'; listError.value = ''
      if (props.modelValue && props.file) loadList()
    }, { immediate: true })
    return { Plus, Search, Refresh, EditPen, MoreFilled, Delete, filters, chooseFilter, dirty, drawerReady, chunks, total, page, keyword, enabledFilter, loading, busy, detailLoading, listError, actionError, selected, mode, content, charCount, beforeClose, loadList, search, changePage, openChunk, newChunk, cancelEdit, save, setEnabled, removeChunk }
  }
}
</script>

<style scoped>
.manager-heading { display: flex; align-items: center; gap: 14px; min-width: 0; }
.document-mark, .empty-mark { display: grid; place-items: center; color: var(--app-primary); background: var(--app-primary-soft); flex-shrink: 0; }
.document-mark { width: 46px; height: 46px; border: 1px solid #dbeafe; border-radius: 12px; font-size: 24px; }
.heading-copy { min-width: 0; }
.heading-eyebrow { display: block; color: var(--app-text-muted); font-size: 12px; line-height: 1.5; margin-bottom: 5px; }
.heading-copy h2 { display: flex; align-items: center; gap: 10px; font-size: 20px; font-weight: 650; color: var(--app-text); margin: 0; white-space: nowrap; }
.heading-divider { color: var(--app-border); font-weight: 400; }
.heading-filename { min-width: 0; overflow: hidden; text-overflow: ellipsis; font-size: 15px; font-weight: 400; color: var(--app-text-muted); }
.chunk-manager { display: flex; flex-direction: column; width: 100%; min-width: 0; min-height: 0; background: var(--app-surface-subtle); }
.shared-notice { display: flex; align-items: flex-start; gap: 9px; padding: 12px 28px; color: #6b5a32; background: #fffbef; border-bottom: 1px solid #f3ead2; font-size: 12px; line-height: 1.7; flex-shrink: 0; }
.shared-notice > .el-icon { margin-top: 3px; font-size: 15px; flex-shrink: 0; }
.shared-notice p { margin: 0; }.shared-notice p span { margin-left: 8px; color: #8a7c60; }
.chunk-workspace { display: grid; grid-template-columns: minmax(300px, 38fr) minmax(0, 62fr); gap: 20px; padding: 20px; flex: 1; min-height: 0; min-width: 0; }
.chunk-sidebar, .chunk-detail { min-height: 0; min-width: 0; display: flex; flex-direction: column; overflow: hidden; background: var(--app-surface); border: 1px solid var(--app-border-soft); border-radius: 12px; }
.sidebar-toolbar { padding: 18px 16px 10px; flex-shrink: 0; }
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 16px; }
.section-heading h3 { margin: 0; font-size: 15px; font-weight: 650; }
.chunk-search { display: flex; gap: 8px; }.chunk-search .el-input { flex: 1; min-width: 0; }
.filter-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-top: 14px; }
.status-filters { display: flex; gap: 3px; background: var(--app-surface-subtle); border-radius: 8px; padding: 3px; }
.status-filters button { border: 0; background: transparent; color: var(--app-text-muted); padding: 6px 13px; border-radius: 6px; font-size: 12px; cursor: pointer; }
.status-filters button.active { color: var(--app-primary); background: var(--app-surface); box-shadow: 0 1px 4px #0f172a12; font-weight: 600; }
.status-filters button:disabled { cursor: default; opacity: .6; }
.result-count { margin: 12px 0 0; font-size: 12px; color: var(--app-text-muted); }
.chunk-list { flex: 1; min-height: 0; overflow-y: auto; scrollbar-gutter: stable; padding: 4px 12px 12px 16px; }
.chunk-card { display: flex; flex-direction: column; gap: 12px; width: 100%; min-width: 0; padding: 15px; margin: 0 0 10px; text-align: left; font: inherit; background: var(--app-surface); border: 1px solid var(--app-border); border-radius: 10px; color: var(--app-text); cursor: pointer; transition: background .15s ease, border-color .15s ease; }
.chunk-card:hover { border-color: #a5bff2; background: #fafcff; }
.chunk-card.selected { border-color: var(--app-primary); background: var(--app-primary-soft); box-shadow: inset 3px 0 var(--app-primary); }
.chunk-card.is-disabled:not(.selected) { background: var(--app-surface-subtle); }
.chunk-card:disabled { cursor: default; }
.chunk-card:focus-visible, .status-filters button:focus-visible { outline: 2px solid var(--app-primary); outline-offset: 2px; }
.card-heading, .card-footer { display: flex; justify-content: space-between; align-items: center; gap: 10px; }
.chunk-number { font-size: 12px; font-weight: 650; color: var(--app-primary); font-variant-numeric: tabular-nums; }
.chunk-status { display: inline-flex; align-items: center; gap: 5px; white-space: nowrap; font-size: 11px; font-weight: 400; color: #6b7280; background: #f1f3f5; border-radius: 5px; padding: 3px 7px; line-height: 1.5; }
.chunk-status i { width: 5px; height: 5px; border-radius: 50%; background: currentColor; }.chunk-status.enabled { color: #15803d; background: #effaf2; }
.chunk-summary { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 3; overflow: hidden; overflow-wrap: anywhere; white-space: pre-wrap; font-size: 13px; line-height: 1.8; }
.card-footer { font-size: 11px; color: var(--app-text-muted); }.meta-dot { margin: 0 8px; color: #b0bac8; }.card-arrow { color: #94a3b8; }.selected .card-arrow { color: var(--app-primary); }
.list-pagination { display: flex; justify-content: center; border-top: 1px solid var(--app-border-soft); padding: 12px 6px; flex-shrink: 0; overflow-x: auto; }
.list-empty { text-align: center; padding: 36px 12px; color: var(--app-text-muted); }.list-empty > .el-icon { font-size: 28px; color: #94a3b8; }.list-empty h4 { margin: 14px 0 8px; font-size: 14px; color: var(--app-text); }.list-empty p { font-size: 12px; line-height: 1.8; margin-bottom: 14px; overflow-wrap: anywhere; }
.list-skeleton { padding: 18px 4px; }
.detail-empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; text-align: center; padding: 36px 24px; overflow-y: auto; }
.empty-mark { width: 76px; height: 76px; border-radius: 22px; font-size: 36px; margin-bottom: 24px; border: 1px solid #dbeafe; }
.detail-empty h3 { font-size: 20px; font-weight: 600; margin: 0 0 12px; }.detail-empty p { color: var(--app-text-muted); font-size: 13px; line-height: 1.9; margin-bottom: 24px; }.empty-caption { margin-top: 26px; font-size: 11px; color: var(--app-text-muted); }
.detail-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; padding: 20px 24px; border-bottom: 1px solid var(--app-border-soft); flex-shrink: 0; }
.detail-heading h3 { display: flex; gap: 10px; align-items: center; margin: 0; font-size: 19px; font-weight: 650; }.detail-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }.detail-actions > .el-button + .el-button { margin-left: 0; }
.reading-body { flex: 1; min-height: 0; overflow-y: auto; scrollbar-gutter: stable; padding: 22px 24px; }.reading-meta { font-size: 12px; color: var(--app-text-muted); margin: 0 0 24px; }
.chunk-content { margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; font: inherit; font-size: 14px; line-height: 2; color: var(--app-text); }
.source-details { margin-top: 30px; padding: 14px 0 0; border-top: 1px dashed var(--app-border); color: var(--app-text-muted); font-size: 12px; }.source-details summary { cursor: pointer; }.source-details code { display: block; overflow-wrap: anywhere; margin-top: 12px; line-height: 1.8; }
.detail-footer, .editor-footer { flex-shrink: 0; border-top: 1px solid var(--app-border-soft); padding: 16px 24px; background: var(--app-surface); }.detail-footer > span { display: flex; gap: 7px; align-items: center; font-size: 12px; color: var(--app-text-muted); }
.writing-body { flex: 1; min-height: 0; display: flex; flex-direction: column; padding: 20px 24px; }.writing-body label { font-size: 13px; font-weight: 600; }.writing-body p { font-size: 12px; line-height: 1.6; color: var(--app-text-muted); margin: 8px 0 14px; }
.content-input { flex: 1; min-height: 120px; display: flex; }.content-input :deep(.el-textarea__inner) { flex: 1; height: 100%; font-family: inherit; font-size: 14px; line-height: 1.9; padding: 14px 16px; }
.editor-footer { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }.editor-count { display: flex; flex-direction: column; gap: 5px; font-size: 12px; color: var(--app-text-muted); }.draft-state { font-size: 11px; }.invalid { color: var(--app-danger); }.editor-buttons { display: flex; gap: 8px; flex-wrap: wrap; }.editor-buttons > .el-button + .el-button { margin-left: 0; }
.disabled-note { padding: 0 24px 14px; font-size: 12px; color: var(--app-warning); }.action-error { border-radius: 0; flex-shrink: 0; }.detail-empty .el-alert { margin-top: 20px; }.detail-skeleton { flex: 1; padding: 30px 24px; overflow: hidden; }.detail-skeleton p { margin-top: 20px; color: var(--app-text-muted); font-size: 13px; }.opening-placeholder { padding: 30px; }
@media (max-width: 1023px) {
  .chunk-manager { min-height: 100%; flex: none; }
  .chunk-workspace { display: flex; flex-direction: column; flex: none; }
  .chunk-sidebar { flex: none; }.chunk-list { flex: none; max-height: 32vh; min-height: 120px; }
  .chunk-detail { flex: none; min-height: 360px; }.reading-body { flex: none; }.writing-body { flex: none; }.content-input { height: 320px; flex: none; }
  .detail-empty { min-height: 340px; box-sizing: border-box; }.editor-footer { position: sticky; bottom: 0; z-index: 1; }
}
@media (max-width: 600px) {
 .document-mark { width: 38px; height: 38px; font-size: 20px; }.heading-copy h2 { flex-wrap: wrap; gap: 3px 8px; font-size: 17px; }.heading-filename { flex-basis: 100%; font-size: 12px; }.heading-divider { display: none; }
  .shared-notice { padding: 10px 16px; }.shared-notice p span { display: block; margin-left: 0; }
  .chunk-workspace { padding: 12px; gap: 12px; }.detail-heading, .reading-body, .writing-body { padding: 18px 16px; }.detail-footer, .editor-footer { padding: 14px 16px; }.editor-buttons { margin-left: auto; }.detail-actions { width: 100%; }
}
</style>
<style>
/* Drawer is teleported by Element Plus and does not receive this SFC's scope
   attribute. Keep third-party container overrides explicitly namespaced. */
.chunk-drawer { color: var(--app-text); }
.chunk-drawer .el-drawer__header { margin: 0; padding: 24px 28px; border-bottom: 1px solid var(--app-border-soft); }
.chunk-drawer .el-drawer__body { padding: 0; overflow: hidden; min-height: 0; display: flex; }
@media (max-width: 1023px) {
  .chunk-drawer .el-drawer__body { overflow-y: auto; scrollbar-gutter: stable; }
}
@media (max-width: 600px) {
  .chunk-drawer { width: 100vw !important; }
  .chunk-drawer .el-drawer__header { padding: 16px; }
}
.chunk-delete-action { color: var(--app-danger) !important; }
</style>
