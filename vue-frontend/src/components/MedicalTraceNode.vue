<template>
  <li class="trace-node">
    <div class="node-line">
      <span :class="['node-icon', node.status]">{{ statusIcon(node.status) }}</span>
      <div class="node-body">
        <div class="node-heading">
          <strong>{{ taskName(node) }}</strong
          ><span class="node-status">{{ statusText(node.status) }}</span>
        </div>
        <div class="node-meta">
          {{ agentName(node.type === 'subagent_start' ? node.tool : node.agent)
          }}<span v-if="node.durationMs != null"> · {{ node.durationMs }} ms</span>
        </div>
        <p v-if="node.summary" class="node-summary">{{ node.summary }}</p>
        <details v-if="node.tool || node.input" class="node-details">
          <summary>调用详情</summary>
          <p v-if="node.tool">
            工具：<code>{{ node.tool }}</code>
          </p>
          <pre v-if="node.input">{{ node.input }}</pre>
        </details>
      </div>
    </div>
    <ol v-if="node.children.length" class="node-children">
      <MedicalTraceNode v-for="child in node.children" :key="child.key" :node="child" />
    </ol>
  </li>
</template>
<script>
import { agentName, taskName, statusText, statusIcon } from '../utils/medicalTrace.mjs'
export default {
  name: 'MedicalTraceNode',
  props: { node: { type: Object, required: true } },
  setup: () => ({ agentName, taskName, statusText, statusIcon }),
}
</script>
<style scoped>
.trace-node {
  list-style: none;
  padding: 0 0 18px;
}
.node-line {
  display: flex;
  gap: 10px;
}
.node-icon {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--app-surface-subtle);
  color: var(--app-text-muted);
  border: 1px solid var(--app-border);
}
.node-icon.completed {
  color: var(--app-success);
}
.node-icon.running {
  color: var(--app-primary);
  background: var(--app-primary-soft);
}
.node-icon.failed {
  color: var(--app-danger);
}
.node-icon.partial,
.node-icon.unknown {
  color: var(--app-warning);
}
.node-body {
  min-width: 0;
  flex: 1;
}
.node-heading {
  display: flex;
  gap: 10px;
  align-items: baseline;
  justify-content: space-between;
}
.node-status,
.node-meta {
  font-size: 12px;
  color: var(--app-text-muted);
}
.node-status {
  flex-shrink: 0;
}
.node-meta {
  margin-top: 5px;
}
.node-summary {
  font-size: 13px;
  line-height: 1.7;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 144px;
  overflow: auto;
  margin: 8px 0;
}
.node-details {
  font-size: 12px;
  margin-top: 8px;
  color: var(--app-text-muted);
}
summary {
  cursor: pointer;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  background: var(--app-surface-subtle);
  padding: 10px;
  max-height: 200px;
  overflow: auto;
}
.node-children {
  border-left: 1px solid var(--app-border);
  padding: 16px 0 0 14px;
  margin: 8px 0 0 11px;
}
code {
  overflow-wrap: anywhere;
}
@media (max-width: 480px) {
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
