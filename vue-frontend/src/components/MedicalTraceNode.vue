<template>
  <li :class="['trace-node', `trace-node--${kind}`]">
    <article class="node-card">
      <div class="node-heading">
        <span :class="['node-kind', kind]">{{ kindLabel }}</span>
        <span :class="['node-status', node.status]">
          {{ statusIcon(node.status) }} {{ statusText(node.status) }}
        </span>
      </div>
      <h4 class="node-title">{{ title }}</h4>
      <div v-if="kind !== 'tool' || node.durationMs != null" class="node-meta">
        <span v-if="kind !== 'tool'">{{ taskName(node) }}</span>
        <span v-if="node.durationMs != null">耗时 {{ node.durationMs }} ms</span>
      </div>
      <p v-if="kind === 'tool'" class="node-owner">
        {{ agentName(node.agent) }} 调用 · <code>{{ node.tool }}</code>
      </p>
      <p v-if="kind === 'tool' && node.summary" class="node-preview">{{ node.summary }}</p>
      <details v-if="node.summary || node.input || node.tool" class="node-details">
        <summary>{{ kind === 'tool' ? '调用详情' : '任务与执行结果' }}</summary>
        <template v-if="node.tool">
          <p>{{ kind === 'subagent' ? 'Agent' : '工具' }}：<code>{{ node.tool }}</code></p>
        </template>
        <template v-if="node.input">
          <p>{{ kind === 'tool' ? '调用参数摘要' : '任务摘要' }}</p>
          <pre>{{ node.input }}</pre>
        </template>
        <template v-if="node.summary">
          <p>执行结果摘要</p>
          <pre>{{ node.summary }}</pre>
        </template>
      </details>
      <button
        v-if="node.children.length"
        type="button"
        class="children-toggle"
        :aria-expanded="expanded"
        @click="expanded = !expanded"
      >
        <span aria-hidden="true">{{ expanded ? '▾' : '▸' }}</span>
        {{ expanded ? '收起' : '展开' }}{{ kind === 'main' ? '任务分工与工具' : '工具调用' }}
        <span>{{ node.children.length }}</span>
      </button>
    </article>
    <ol v-if="node.children.length" v-show="expanded" class="node-children">
      <MedicalTraceNode v-for="child in node.children" :key="child.key" :node="child" />
    </ol>
  </li>
</template>
<script>
import { computed, ref } from 'vue'
import { agentName, taskName, statusText, statusIcon } from '../utils/medicalTrace.mjs'
export default {
  name: 'MedicalTraceNode',
  props: { node: { type: Object, required: true } },
  setup(props) {
    const expanded = ref(true)
    const kind = computed(() => {
      if (props.node.type === 'run_start') return 'main'
      if (props.node.type.startsWith('subagent_')) return 'subagent'
      if (props.node.type.startsWith('tool_')) return 'tool'
      return 'event'
    })
    const kindLabel = computed(
      () => ({ main: '主 Agent', subagent: '子 Agent', tool: '工具', event: '运行事件' })[kind.value]
    )
    const title = computed(() => {
      if (kind.value === 'main') return '统筹分析与回答'
      if (kind.value === 'subagent') return agentName(props.node.tool || props.node.agent)
      return taskName(props.node)
    })
    return { expanded, kind, kindLabel, title, agentName, taskName, statusText, statusIcon }
  },
}
</script>
<style scoped>
.trace-node {
  list-style: none;
  padding: 0 0 12px;
  min-width: 0;
}
.node-card {
  padding: 14px;
  border: 1px solid var(--app-border);
  border-radius: 10px;
  background: var(--app-surface);
}
.trace-node--main > .node-card {
  border-color: var(--app-primary);
  background: var(--app-primary-soft);
}
.trace-node--subagent > .node-card {
  border-left: 3px solid var(--app-primary);
}
.trace-node--tool > .node-card {
  background: var(--app-surface-subtle);
  padding: 12px;
}
.node-heading {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
}
.node-kind {
  font-size: 11px;
  font-weight: 600;
  padding: 2px 7px;
  border-radius: 4px;
  color: var(--app-text-muted);
  background: var(--app-surface-subtle);
}
.node-kind.main,
.node-kind.subagent {
  color: var(--app-primary);
  background: var(--app-surface);
}
.node-title {
  margin: 8px 0 4px;
  color: var(--app-text);
  font-size: 14px;
  overflow-wrap: anywhere;
}
.node-status,
.node-meta,
.node-owner {
  font-size: 12px;
  color: var(--app-text-muted);
}
.node-status {
  flex-shrink: 0;
}
.node-status.completed {
  color: var(--app-success);
}
.node-status.running {
  color: var(--app-primary);
}
.node-status.failed {
  color: var(--app-danger);
}
.node-status.partial,
.node-status.unknown,
.node-status.rejected {
  color: var(--app-warning);
}
.node-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
}
.node-owner {
  margin: 6px 0 0;
  overflow-wrap: anywhere;
}
.node-preview {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  font-size: 12px;
  line-height: 1.7;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin: 8px 0;
}
.node-details {
  font-size: 12px;
  margin-top: 10px;
  color: var(--app-text-muted);
}
.node-details p {
  margin: 8px 0 4px;
}
summary {
  cursor: pointer;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  background: var(--app-surface);
  border: 1px solid var(--app-border-soft);
  border-radius: 6px;
  padding: 10px;
  max-height: 240px;
  overflow: auto;
  margin: 4px 0 10px;
}
.children-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 12px;
  padding: 4px 0;
  border: 0;
  font: inherit;
  font-size: 12px;
  color: var(--app-primary);
  background: transparent;
  cursor: pointer;
}
.children-toggle > span:last-child {
  color: var(--app-text-muted);
}
.children-toggle:focus-visible,
summary:focus-visible {
  outline: 2px solid var(--app-primary);
  outline-offset: 3px;
}
.node-children {
  border-left: 2px solid var(--app-border);
  padding: 12px 0 0 16px;
  margin: 0 0 0 16px;
}
code {
  overflow-wrap: anywhere;
}
@media (max-width: 480px) {
  .node-card {
    padding: 10px;
  }
  .node-children {
    padding-left: 8px;
    margin-left: 6px;
  }
  .node-heading {
    flex-wrap: wrap;
    gap: 4px;
  }
}
</style>
