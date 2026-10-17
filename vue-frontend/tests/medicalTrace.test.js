import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { buildTrace, parseEvents, stepCount, traceSources } from '../src/utils/medicalTrace.mjs'
import * as trace from '../src/utils/medicalTrace.mjs'
const require = createRequire(import.meta.url)
const { reactive, ref, computed, watch, onUnmounted, createRenderer, h, nextTick } = require('vue')
const events = [
  { id: 'run', type: 'run_start' },
  { id: 'child', type: 'subagent_start', tool: 'report_analyst' },
  { id: 'tool', parentId: 'child', type: 'tool_start', tool: 'read_report', summary: '{}' },
  {
    id: 'end-tool',
    parentId: 'tool',
    type: 'tool_end',
    status: 'failed',
    summary: '读取失败',
    durationMs: 0,
  },
  { id: 'end-child', parentId: 'child', type: 'subagent_end', status: 'completed', durationMs: 20 },
  { id: 'end', type: 'run_end', status: 'partial' },
]
test('合并调用与返回，保留分工、失败和零耗时，统计不包含主运行', () => {
  const tree = buildTrace(events, 'partial')
  assert.equal(tree.length, 1)
  assert.equal(tree[0].status, 'partial')
  const child = tree[0].children[0],
    tool = child.children[0]
  assert.equal(child.durationMs, 20)
  assert.equal(tool.status, 'failed')
  assert.equal(tool.summary, '读取失败')
  assert.equal(tool.durationMs, 0)
  assert.equal(stepCount(events), 2)
})
test('未返回节点只在实时运行时显示运行中', () => {
  assert.equal(buildTrace(events.slice(0, 3), 'running')[0].children[0].status, 'running')
  for (const status of ['stopped', 'failed', 'unknown', 'completed'])
    assert.equal(buildTrace(events.slice(0, 3), status)[0].children[0].status, 'unknown')
})
test('主 Agent 直接调用的工具与子 Agent 同属主节点，子 Agent 工具保持原归属', () => {
  const input = [
    { id: 'run', type: 'run_start', agent: 'medical_assistant' },
    { id: 'memory', type: 'tool_start', agent: 'medical_assistant', tool: 'read_user_memory' },
    { id: 'child', type: 'subagent_start', agent: 'medical_assistant', tool: 'medical_researcher' },
    { id: 'search', parentId: 'child', type: 'tool_start', agent: 'medical_researcher', tool: 'search_medical_knowledge' },
    { parentId: 'memory', type: 'tool_end', status: 'completed', summary: '已读取个人档案' },
    { id: 'denied', type: 'tool_rejected', agent: 'medical_assistant', tool: 'update_user_memory', status: 'rejected' },
    { id: 'failure', type: 'error', summary: '运行异常' },
  ]
  const tree = buildTrace(input, 'running')
  assert.equal(tree.length, 1)
  assert.deepEqual(tree[0].children.map((node) => node.id), ['memory', 'child', 'denied', 'failure'])
  assert.equal(tree[0].children[0].summary, '已读取个人档案')
  assert.equal(tree[0].children[1].children[0].id, 'search')
  assert.equal(tree[0].children[2].status, 'rejected')
  assert.equal(trace.statusText('rejected'), '未执行')
})
test('明确但无效的父节点不误归到其他 Agent，同名子 Agent 按调用 ID 区分', () => {
  const tree = buildTrace([
    { id: 'run', type: 'run_start' },
    { id: 'child-a', type: 'subagent_start', tool: 'medical_researcher' },
    { id: 'child-b', type: 'subagent_start', tool: 'medical_researcher' },
    { id: 'search-b', parentId: 'child-b', type: 'tool_start' },
    { id: 'orphan', parentId: 'missing', type: 'tool_start' },
  ], 'completed')
  assert.equal(tree.length, 2)
  assert.equal(tree[1].id, 'orphan')
  assert.equal(tree[0].children[0].children.length, 0)
  assert.equal(tree[0].children[1].children[0].id, 'search-b')
})
test('缺失父节点、未知事件和循环关联不会丢失或无限递归', () => {
  const tree = buildTrace(
    [
      { id: 'a', parentId: 'b', type: 'tool_start' },
      { id: 'b', parentId: 'a', type: 'tool_start' },
      { type: 'custom', summary: '信息' },
      { parentId: 'missing', type: 'tool_end' },
    ],
    'failed'
  )
  assert.equal(tree.length, 4)
})
test('损坏事件安全降级，来源去重且不显示回答片段为步骤', () => {
  for (const value of ['{', '{}', '[null]', '[{"type":2}]']) assert.ok(parseEvents(value).error)
  assert.deepEqual(parseEvents('[]').events, [])
  const input = [
    { type: 'sources', sources: [{ id: 's', title: '资料' }, null] },
    { type: 'answer_delta', sources: [{ id: 's', title: '更新资料' }] },
    { type: 'answer_final', delta: '最终回答' },
  ]
  assert.equal(traceSources(input).length, 1)
  assert.equal(buildTrace(input, 'completed').length, 0)
})
// Mount the real component setup with Vue's renderer; no DOM or network required.
function mountPanel(initial = {}) {
  const requests = []
  const api = { get: () => new Promise((resolve, reject) => requests.push({ resolve, reject })) }
  const source = readFileSync(
    new URL('../src/components/MedicalRunPanel.vue', import.meta.url),
    'utf8'
  )
    .split('<script>')[1]
    .split('</script>')[0]
    .replace(/^import [\s\S]*? from ['"][^'"]+['"]$/gm, '')
    .replace('export default', 'return')
  const names = {
    ref,
    computed,
    watch,
    onUnmounted,
    api,
    getApiErrorMessage: (e) => e.message,
    MedicalTraceNode: {},
    ...trace,
  }
  const component = new Function(...Object.keys(names), source)(...Object.values(names))
  const props = reactive({
    sessionId: 'session-a',
    events: [],
    busy: false,
    question: '',
    requestStatus: '',
    ...initial,
  })
  let state
  const renderer = createRenderer({
    createElement: () => ({}),
    insert: () => {},
    remove: () => {},
    patchProp: () => {},
    setElementText: () => {},
    createText: () => ({}),
    createComment: () => ({}),
    setText: () => {},
    setComment: () => {},
    parentNode: () => null,
    nextSibling: () => null,
  })
  const app = renderer.createApp({
    setup() {
      state = component.setup(props)
      return () => h('div')
    },
  })
  app.mount({})
  return { props, state, requests, unmount: () => app.unmount() }
}
const finish = async (request, runs) => {
  request.resolve({ data: { status_code: 1000, runs } })
  await nextTick()
  await nextTick()
}
test('历史选择独立于实时事件，并可返回本次运行', async () => {
  const panel = mountPanel({ busy: true, events, question: '本次问题', requestStatus: 'running' })
  const old = {
    runId: 'old',
    question: '历史问题',
    status: 'completed',
    eventsJson: '[{"type":"custom","summary":"历史内容"}]',
  }
  await finish(panel.requests[0], [old])
  panel.state.selectRun(old)
  assert.equal(panel.state.displayQuestion.value, '历史问题')
  panel.props.events = [...events, { type: 'custom', summary: '实时内容' }]
  assert.equal(panel.state.displayEvents.value[0].summary, '历史内容')
  panel.state.openCurrent()
  assert.equal(panel.state.displayQuestion.value, '本次问题')
  panel.unmount()
})
test('切换会话后旧请求不能覆盖新历史，筛选有效', async () => {
  const panel = mountPanel()
  panel.props.sessionId = 'session-b'
  await nextTick()
  await finish(panel.requests[1], [{ runId: 'b', question: '报告解读', status: 'completed' }])
  await finish(panel.requests[0], [{ runId: 'a', question: '旧会话', status: 'failed' }])
  assert.equal(panel.state.runs.value[0].runId, 'b')
  panel.state.query.value = '报告'
  assert.equal(panel.state.filteredRuns.value.length, 1)
  panel.state.filter.value = 'failed'
  assert.equal(panel.state.filteredRuns.value.length, 0)
  panel.unmount()
})
test('结束时刷新，断线与主动停止不显示运行中', async () => {
  const panel = mountPanel({ busy: true, events: events.slice(0, 3), requestStatus: 'running' })
  panel.props.busy = false
  panel.props.requestStatus = 'stopped'
  await nextTick()
  assert.equal(panel.requests.length, 2)
  assert.equal(panel.state.currentStatus.value, 'stopped')
  panel.props.requestStatus = 'unknown'
  assert.equal(panel.state.currentStatus.value, 'unknown')
  panel.unmount()
})
test('新会话绑定真实 ID 保持抽屉打开，失败可重试', async () => {
  const panel = mountPanel({ sessionId: 'temp', busy: true })
  panel.state.openCurrent()
  panel.props.sessionId = 'real'
  await nextTick()
  assert.equal(panel.state.opened.value, true)
  panel.requests[0].reject(new Error('网络异常'))
  await nextTick()
  await nextTick()
  assert.equal(panel.state.historyError.value, '网络异常')
  const retry = panel.state.loadRuns()
  await finish(panel.requests[1], [])
  await retry
  assert.equal(panel.state.historyError.value, '')
  panel.unmount()
})

test('检索来源保留类型和前缀，危险链接不可打开', () => {
  const source = { id: 'web_test', kind: 'web', url: 'https://example.org/evidence', title: '资料', content: '片段' }
  const stored = JSON.stringify([{ type: 'sources', sources: [source, { id: 'knowledge_' + 'a'.repeat(64) + '_1', kind: 'knowledge', title: '知识库资料' }] }])
  const restored = traceSources(parseEvents(stored).events)
  assert.deepEqual(restored, [source, { id: 'knowledge_' + 'a'.repeat(64) + '_1', kind: 'knowledge', title: '知识库资料' }])
  assert.equal(trace.safeSourceURL(source.url), source.url)
  for (const url of [undefined, 'javascript:alert(1)', 'data:text/html,test', 'https://user:pass@example.org', '/relative']) {
    assert.equal(trace.safeSourceURL(url), '')
  }
  assert.equal(trace.taskName({ tool: 'search_web' }), '联网搜索')
})
