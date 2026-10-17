<template>
  <section class="run-card" aria-label="Agent 运行概览">
    <div class="card-top">
      <div class="card-title"><span class="agent-mark">A</span><strong>Agent 执行轨迹</strong></div>
      <span :class="['status-pill', currentStatus]"
        >{{ statusIcon(currentStatus) }} {{ statusText(currentStatus) }}</span
      >
    </div>
    <p class="current-task">{{ currentTask }}</p>
    <p v-if="hasRunningTool" class="waiting-hint" role="status">
      工具执行可能需要一些时间，请耐心等待。
    </p>
    <div class="card-bottom">
      <span
        >{{ stepCount(currentEvents) }} 个调用步骤 <span class="separator">·</span>
        {{ traceSources(currentEvents).length }} 个来源</span
      >
      <div class="card-actions">
        <button @click="openCurrent">查看执行详情 <span aria-hidden="true">↗</span></button
        ><button @click="openHistory">历史记录</button>
      </div>
    </div>
    <el-drawer
      v-model="opened"
      title="Agent 运行详情"
      size="min(680px, 100vw)"
      append-to-body
      class="medical-run-drawer"
    >
      <div class="run-workspace">
        <el-tabs v-model="tab"
          ><el-tab-pane label="执行轨迹" name="trace" /><el-tab-pane
            label="历史记录"
            name="history"
        /></el-tabs>
        <template v-if="tab === 'trace'">
          <div v-if="selectedRun" class="history-banner">
            <span>历史运行 · {{ formatDate(selectedRun.createdAt) }}</span>
            <div>
              <button @click="tab = 'history'">返回历史列表</button
              ><button @click="selectedId = null">查看本次运行</button>
            </div>
          </div>
          <div class="detail-heading">
            <span :class="['status-pill', displayStatus]"
              >{{ statusIcon(displayStatus) }} {{ statusText(displayStatus) }}</span
            >
            <h3>{{ displayQuestion || '发送问题，开启一次 Agent 协作' }}</h3>
            <p>{{ stepCount(displayEvents) }} 个调用步骤 · {{ sources.length }} 个来源</p>
            <p v-if="!selectedRun && hasRunningTool" class="waiting-hint" role="status">
              工具执行可能需要一些时间，请耐心等待。
            </p>
          </div>
          <div v-if="parseError" class="notice error" role="alert">
            {{ parseError }}<button @click="loadRuns">刷新记录</button>
          </div>
          <div v-else-if="!displayEvents.length" class="empty-state">
            <span class="empty-symbol">◎</span
            ><strong>{{ displayStatus === 'running' ? '正在准备任务' : '暂无执行轨迹' }}</strong>
            <p>
              {{
                displayStatus === 'running'
                  ? '收到执行事件后将在这里实时展示。'
                  : '发送问题后，可查看任务分工、工具调用与资料来源。'
              }}
            </p>
          </div>
          <template v-else>
            <div class="trace-guide">
              <strong>主 Agent <span>→</span> 子 Agent <span>→</span> 工具</strong>
              <p>按调用归属分层展示，主 Agent 直接使用的工具列在主 Agent 下。点击节点可收起下级或查看执行详情。</p>
            </div>
            <ol class="trace-tree">
              <MedicalTraceNode v-for="node in nodes" :key="node.key" :node="node" />
            </ol>
          </template>
          <div v-if="['stopped', 'unknown'].includes(displayStatus)" class="notice">
            {{
              displayStatus === 'stopped' ? '已停止接收本次回答。' : '连接已结束。'
            }}最终运行结果请刷新历史记录查看。<button @click="openHistory">查看历史记录</button>
          </div>
          <section v-if="sources.length" class="source-section">
            <h4>
              参考来源 <span>{{ sources.length }}</span>
            </h4>
            <details v-for="source in sources" :key="source.id">
              <summary>
                {{ { web: '网络来源', knowledge: '知识库' }[source.kind] || '未知来源' }} · {{ source.title || '参考资料' }} <span>[{{ source.id }}]</span>
              </summary>
              <a v-if="safeSourceURL(source.url)" :href="safeSourceURL(source.url)" target="_blank" rel="noopener noreferrer">查看原网页</a>
              <pre>{{ source.content }}</pre>
            </details>
          </section>
        </template>
        <template v-else>
          <div class="history-heading">
            <div>
              <h3>历史运行记录</h3>
              <p>当前会话最近 30 次</p>
            </div>
            <button :disabled="historyLoading" @click="loadRuns">
              {{ historyLoading ? '刷新中…' : '刷新' }}
            </button>
          </div>
          <div class="history-filters">
            <input
              v-model="query"
              aria-label="搜索历史问题"
              placeholder="搜索问题关键词"
              type="search"
            /><select v-model="filter" aria-label="筛选运行状态">
              <option value="all">全部状态</option>
              <option
                v-for="status in ['running', 'completed', 'partial', 'failed', 'interrupted']"
                :key="status"
                :value="status"
              >
                {{ statusText(status) }}
              </option>
            </select>
          </div>
          <div v-if="historyError" class="notice error" role="alert">
            {{ historyError }}<button @click="loadRuns">重试</button>
          </div>
          <p v-if="historyLoading" role="status" class="muted">正在读取运行记录…</p>
          <div v-if="!historyLoading && !historyError && !filteredRuns.length" class="empty-state">
            <span class="empty-symbol">◷</span
            ><strong>{{ runs.length ? '没有匹配的记录' : '还没有运行记录' }}</strong>
            <p>
              {{
                runs.length ? '试试其他关键词或状态。' : '本次会话的运行结束后，记录将显示在这里。'
              }}
            </p>
          </div>
          <ul class="history-list">
            <li v-for="run in filteredRuns" :key="run.runId">
              <button
                class="history-item"
                :class="{ selected: selectedId === run.runId }"
                @click="selectRun(run)"
              >
                <span class="history-item-top"
                  ><span :class="['status-pill', run.status]"
                    >{{ statusIcon(run.status) }} {{ statusText(run.status) }}</span
                  ><time>{{ formatDate(run.createdAt) }}</time></span
                ><strong>{{ run.question || '未命名问题' }}</strong
                ><span class="history-item-bottom"
                  >{{ stepCount(parseEvents(run.eventsJson).events) }} 个调用步骤<span
                    >查看轨迹 →</span
                  ></span
                >
              </button>
            </li>
          </ul>
        </template>
      </div>
    </el-drawer>
  </section>
</template>
<script>
import { ref, computed, watch, onUnmounted } from 'vue'
import api, { getApiErrorMessage } from '../utils/api'
import MedicalTraceNode from './MedicalTraceNode.vue'
import {
  statusText,
  statusIcon,
  taskName,
  parseEvents,
  buildTrace,
  stepCount,
  traceSources,
  safeSourceURL,
} from '../utils/medicalTrace.mjs'
export default {
  name: 'MedicalRunPanel',
  components: { MedicalTraceNode },
  props: {
    sessionId: String,
    events: { type: Array, default: () => [] },
    busy: Boolean,
    question: String,
    requestStatus: String,
  },
  setup(props) {
    const opened = ref(false),
      tab = ref('trace'),
      runs = ref([]),
      selectedId = ref(null),
      query = ref(''),
      filter = ref('all'),
      historyLoading = ref(false),
      historyError = ref('')
    let generation = 0,
      disposed = false
    const loadRuns = async () => {
      const id = props.sessionId,
        ticket = ++generation
      if (!id || id === 'temp') {
        runs.value = []
        historyLoading.value = false
        historyError.value = ''
        return
      }
      historyLoading.value = true
      historyError.value = ''
      try {
        const { data } = await api.get('/medical/runs', { params: { sessionId: id } })
        if (disposed || ticket !== generation || props.sessionId !== id) return
        if (data.status_code != null && data.status_code !== 1000)
          throw new Error(data.status_msg || '运行历史读取失败')
        runs.value = Array.isArray(data.runs) ? data.runs : []
        if (selectedId.value && !runs.value.some((r) => r.runId === selectedId.value))
          selectedId.value = null
      } catch (e) {
        if (!disposed && ticket === generation)
          historyError.value = getApiErrorMessage(e, '运行历史读取失败')
      } finally {
        if (!disposed && ticket === generation) historyLoading.value = false
      }
    }
    const hasCurrent = computed(
      () => props.busy || props.events.length > 0 || !!props.requestStatus
    )
    const latest = computed(() => runs.value[0])
    const currentParsed = computed(() =>
      hasCurrent.value ? { events: props.events, error: '' } : parseEvents(latest.value?.eventsJson)
    )
    const currentEvents = computed(() => currentParsed.value.events)
    const currentStatus = computed(() => {
      if (!hasCurrent.value) return latest.value?.status || 'idle'
      const end = [...props.events].reverse().find((e) => e.type === 'run_end')
      if (end) return end.status || 'unknown'
      if (props.busy) return 'running'
      return props.requestStatus && props.requestStatus !== 'running'
        ? props.requestStatus
        : 'unknown'
    })
    const hasRunningTool = computed(() => {
      if (!props.busy || currentStatus.value !== 'running') return false
      const ended = new Set(
        currentEvents.value.filter((event) => event.type === 'tool_end').map((event) => event.parentId)
      )
      return currentEvents.value.some(
        (event) => event.type === 'tool_start' && !ended.has(event.id)
      )
    })
    const selectedRun = computed(() => runs.value.find((r) => r.runId === selectedId.value))
    const displayParsed = computed(() =>
      selectedRun.value ? parseEvents(selectedRun.value.eventsJson) : currentParsed.value
    )
    const displayEvents = computed(() => displayParsed.value.events)
    const parseError = computed(() => displayParsed.value.error)
    const displayStatus = computed(() => selectedRun.value?.status || currentStatus.value)
    const displayQuestion = computed(
      () =>
        selectedRun.value?.question || (hasCurrent.value ? props.question : latest.value?.question)
    )
    const nodes = computed(() => buildTrace(displayEvents.value, displayStatus.value))
    const sources = computed(() => traceSources(displayEvents.value))
    const currentTask = computed(() => {
      if (currentStatus.value === 'idle') return '发送问题后，在这里查看任务分工与执行进度。'
      if (currentStatus.value === 'running') {
        const ended = new Set(
          currentEvents.value.filter((e) => e.type.endsWith('_end')).map((e) => e.parentId)
        )
        const active = [...currentEvents.value]
          .reverse()
          .find((e) => ['tool_start', 'subagent_start'].includes(e.type) && !ended.has(e.id))
        return active ? taskName(active) : '主 Agent 正在处理问题…'
      }
      return (
        (hasCurrent.value ? props.question : latest.value?.question) ||
        '查看详情，了解本次执行过程。'
      )
    })
    const filteredRuns = computed(() =>
      runs.value.filter(
        (r) =>
          (filter.value === 'all' || r.status === filter.value) &&
          (r.question || '').toLowerCase().includes(query.value.trim().toLowerCase())
      )
    )
    const openCurrent = () => {
      selectedId.value = null
      tab.value = 'trace'
      opened.value = true
    }
    const openHistory = () => {
      tab.value = 'history'
      opened.value = true
      loadRuns()
    }
    const selectRun = (run) => {
      selectedId.value = run.runId
      tab.value = 'trace'
    }
    const formatDate = (value) => {
      const date = new Date(value)
      return Number.isNaN(date.getTime())
        ? '时间未知'
        : date.toLocaleString('zh-CN', {
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
          })
    }
    watch(
      () => props.sessionId,
      (id, old) => {
        selectedId.value = null
        runs.value = []
        query.value = ''
        filter.value = 'all'
        if (!(props.busy && (!old || old === 'temp'))) {
          opened.value = false
          tab.value = 'trace'
        }
        loadRuns()
      },
      { immediate: true }
    )
    watch(
      () => props.busy,
      (busy, wasBusy) => {
        if (!busy && wasBusy) loadRuns()
      }
    )
    onUnmounted(() => {
      disposed = true
      ++generation
    })
    return {
      opened,
      tab,
      runs,
      selectedId,
      query,
      filter,
      historyLoading,
      historyError,
      loadRuns,
      currentEvents,
      currentStatus,
      currentTask,
      hasRunningTool,
      selectedRun,
      displayEvents,
      displayStatus,
      displayQuestion,
      nodes,
      sources,
      parseError,
      filteredRuns,
      openCurrent,
      openHistory,
      selectRun,
      formatDate,
      statusText,
      statusIcon,
      stepCount,
      traceSources,
      safeSourceURL,
      parseEvents,
    }
  },
}
</script>
<style scoped>
.run-card {
  margin-top: 16px;
  padding: 16px;
  border: 1px solid var(--app-border);
  border-radius: 12px;
  background: var(--app-surface);
  color: var(--app-text);
}
.card-top,
.card-title,
.card-bottom,
.card-actions,
.history-heading,
.history-item-top,
.history-item-bottom {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.card-title {
  justify-content: flex-start;
}
.agent-mark {
  display: grid;
  place-items: center;
  background: var(--app-primary-soft);
  color: var(--app-primary);
  width: 30px;
  height: 30px;
  border-radius: 9px;
  font-weight: 700;
}
.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  white-space: nowrap;
  font-size: 12px;
  padding: 4px 8px;
  border-radius: 6px;
  background: var(--app-surface-subtle);
  color: var(--app-text-muted);
}
.status-pill.running {
  background: var(--app-primary-soft);
  color: var(--app-primary);
}
.status-pill.completed {
  background: #f0fdf4;
  color: var(--app-success);
}
.status-pill.failed {
  background: #fef2f2;
  color: var(--app-danger);
}
.status-pill.partial,
.status-pill.unknown {
  background: #fffbeb;
  color: var(--app-warning);
}
.current-task {
  margin: 12px 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  color: var(--app-text);
}
.card-bottom {
  font-size: 12px;
  color: var(--app-text-muted);
  flex-wrap: wrap;
}
.waiting-hint {
  margin: 8px 0 12px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--app-text-muted);
}
.separator {
  padding: 0 6px;
}
button {
  font: inherit;
  cursor: pointer;
  border: 1px solid var(--app-border);
  border-radius: 7px;
  background: var(--app-surface);
  color: var(--app-primary);
  padding: 7px 10px;
}
button:hover {
  background: var(--app-primary-soft);
}
button:disabled {
  opacity: 0.55;
  cursor: wait;
}
button:focus-visible,
input:focus-visible,
select:focus-visible,
summary:focus-visible {
  outline: 2px solid var(--app-primary);
  outline-offset: 3px;
}
.run-workspace {
  color: var(--app-text);
  font-size: 13px;
  line-height: 1.6;
}
.detail-heading {
  padding: 10px 0 18px;
  border-bottom: 1px solid var(--app-border-soft);
  margin-bottom: 20px;
}
h3 {
  font-size: 17px;
  margin: 10px 0 6px;
  overflow-wrap: anywhere;
}
.detail-heading p,
.history-heading p,
.muted {
  color: var(--app-text-muted);
  margin: 0;
}
.trace-tree {
  list-style: none;
  padding: 0;
  margin: 0;
}
.trace-guide {
  margin-bottom: 16px;
  font-size: 12px;
}
.trace-guide strong {
  color: var(--app-text);
}
.trace-guide span {
  padding: 0 6px;
  color: var(--app-text-muted);
}
.trace-guide p {
  margin: 6px 0 0;
  color: var(--app-text-muted);
}
.history-banner {
  padding: 12px;
  border-radius: 8px;
  background: var(--app-primary-soft);
  font-size: 12px;
}
.history-banner > div {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
.history-heading {
  margin: 8px 0 16px;
}
.history-heading h3 {
  margin: 0;
}
.history-filters {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
}
.history-filters input,
.history-filters select {
  font: inherit;
  min-width: 0;
  padding: 9px;
  border: 1px solid var(--app-border);
  border-radius: 7px;
  background: var(--app-surface);
  color: var(--app-text);
}
.history-filters input {
  flex: 1;
}
.history-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.history-item {
  display: block;
  width: 100%;
  text-align: left;
  color: var(--app-text);
  padding: 14px;
  margin-bottom: 10px;
}
.history-item.selected {
  border-color: var(--app-primary);
}
.history-item strong {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  overflow-wrap: anywhere;
  font-weight: 600;
  margin: 12px 0;
}
.history-item time,
.history-item-bottom {
  font-size: 12px;
  color: var(--app-text-muted);
}
.history-item-bottom > span {
  color: var(--app-primary);
}
.empty-state {
  text-align: center;
  padding: 44px 16px;
  color: var(--app-text-muted);
}
.empty-state strong {
  display: block;
  color: var(--app-text);
  font-size: 15px;
}
.empty-symbol {
  font-size: 32px;
  color: var(--app-primary);
  display: block;
  margin-bottom: 12px;
}
.notice {
  padding: 12px;
  background: #fffbeb;
  border-radius: 8px;
  margin: 12px 0;
  overflow-wrap: anywhere;
}
.notice button {
  display: block;
  margin-top: 8px;
}
.notice.error {
  color: var(--app-danger);
  background: #fef2f2;
}
.source-section {
  border-top: 1px solid var(--app-border-soft);
  padding-top: 10px;
}
.source-section h4 {
  font-size: 14px;
}
.source-section h4 span {
  color: var(--app-text-muted);
}
.source-section details {
  border: 1px solid var(--app-border);
  border-radius: 8px;
  padding: 10px;
  margin-bottom: 8px;
}
.source-section summary {
  cursor: pointer;
  overflow-wrap: anywhere;
}
.source-section summary span {
  color: var(--app-text-muted);
}
.source-section pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 240px;
  overflow: auto;
  background: var(--app-surface-subtle);
  padding: 10px;
}
@media (max-width: 480px) {
  .run-card {
    padding: 12px;
  }
  .card-top {
    gap: 6px;
  }
  .card-title {
    gap: 6px;
  }
  .card-actions {
    width: 100%;
  }
  .card-actions button {
    flex: 1;
  }
  .history-item-top {
    gap: 6px;
    flex-wrap: wrap;
  }
}
</style>
