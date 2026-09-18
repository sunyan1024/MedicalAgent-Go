export const statusText = (status) =>
  ({
    running: '运行中',
    completed: '已完成',
    partial: '部分完成',
    failed: '失败',
    interrupted: '已中断',
    stopped: '已停止接收',
    unknown: '结果待确认',
    idle: '等待提问',
  }[status] || '结果待确认')
export const statusIcon = (status) =>
  ({
    running: '◌',
    completed: '✓',
    partial: '!',
    failed: '×',
    interrupted: '■',
    stopped: '■',
    idle: '○',
  }[status] || '?')
export const agentName = (name) =>
  ({
    medical_assistant: '主 Agent',
    report_analyst: '报告分析 Agent',
    medical_researcher: '医学检索 Agent',
  }[name] ||
  name ||
  '系统')
export const taskName = (event) =>
  ({
    report_analyst: '分析报告指标',
    medical_researcher: '查找医学证据',
    read_report: '读取已确认报告',
    compare_reference_ranges: '核对报告参考范围',
    search_medical_knowledge: '检索医学知识',
    search_web: '联网搜索',
    read_medical_source: '读取证据片段',
  }[event.tool] ||
  { run_start: '主 Agent 处理问题', run_end: '本次运行结束', error: '运行异常' }[event.type] ||
  event.tool ||
  event.type ||
  '执行事件')
export function parseEvents(value) {
  try {
    const events = JSON.parse(value || '[]')
    if (
      !Array.isArray(events) ||
      events.some((e) => !e || typeof e !== 'object' || typeof e.type !== 'string')
    )
      throw new Error('invalid')
    return { events, error: '' }
  } catch {
    return { events: [], error: '这条记录的轨迹数据无法读取，请刷新后重试。' }
  }
}
export function buildTrace(events, status) {
  const visible = events.filter(
    (e) => e && typeof e.type === 'string' && !['answer_delta', 'sources'].includes(e.type)
  )
  const starts = new Map(),
    nodes = []
  for (const [index, event] of visible.entries()) {
    if (['tool_end', 'subagent_end', 'run_end'].includes(event.type)) continue
    const node = {
      ...event,
      key: `${event.id || 'event'}-${index}`,
      input: event.summary || '',
      summary: '',
      children: [],
    }
    if (event.type === 'error' || !event.type.endsWith('_start')) {
      node.summary = event.summary || ''
      node.input = ''
    }
    if (event.type === 'run_start') node.status = status
    else if (event.type.endsWith('_start'))
      node.status = status === 'running' ? 'running' : 'unknown'
    else if (event.type === 'error') node.status = 'failed'
    nodes.push(node)
    if (event.id && event.type.endsWith('_start')) starts.set(event.id, node)
  }
  for (const [index, event] of visible.entries()) {
    if (!['tool_end', 'subagent_end', 'run_end'].includes(event.type)) continue
    const start =
      event.type === 'run_end'
        ? nodes.find((n) => n.type === 'run_start')
        : starts.get(event.parentId)
    if (start)
      Object.assign(start, {
        status: event.type === 'run_end' ? status : event.status || 'unknown',
        summary: event.summary || '',
        durationMs: event.durationMs,
      })
    else nodes.push({ ...event, key: `unmatched-${index}`, children: [], input: '' })
  }
  const roots = [],
    main = nodes.find((n) => n.type === 'run_start')
  for (const node of nodes) {
    let parent = starts.get(node.parentId)
    const seen = new Set([node])
    let ancestor = parent
    while (ancestor) {
      if (seen.has(ancestor)) {
        parent = null
        break
      }
      seen.add(ancestor)
      ancestor = starts.get(ancestor.parentId)
    }
    if (!parent && !node.parentId && node !== main && node.type === 'subagent_start') parent = main
    if (parent) parent.children.push(node)
    else roots.push(node)
  }
  return roots
}
export const stepCount = (events) =>
  events.filter((e) => ['tool_start', 'subagent_start'].includes(e.type)).length
export const traceSources = (events) => [
  ...new Map(
    events
      .flatMap((e) => (Array.isArray(e.sources) ? e.sources : []))
      .filter((s) => s && s.id)
      .map((s) => [s.id, s])
  ).values(),
]

export function safeSourceURL(value) {
  try {
    const url = new URL(value)
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password ? url.href : ''
  } catch {
    return ''
  }
}
