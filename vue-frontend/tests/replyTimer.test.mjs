import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { setImmediate } from 'node:timers/promises'
import { createReplyTimer, formatReplyDuration, restoreHistoryMessage } from '../src/utils/replyTimer.mjs'

const { ref, reactive, nextTick } = createRequire(import.meta.url)('vue')

function clock() {
  let time = 100
  const callbacks = new Map()
  let id = 0
  const timer = createReplyTimer({
    now: () => time,
    schedule: callback => { callbacks.set(++id, callback); return id },
    unschedule: key => callbacks.delete(key),
  })
  return {
    timer, callbacks,
    advance(ms) { time += ms; for (const callback of callbacks.values()) callback() },
  }
}

test('计时使用实际时间，结束幂等，旧消息结束不会停止新请求，卸载清理', () => {
  const c = clock()
  const first = { meta: { status: 'streaming' } }, second = { meta: {} }
  c.timer.start(first)
  assert.deepEqual(first.meta.timing, { elapsedMs: 0, running: true })
  c.advance(65001)
  assert.equal(first.meta.timing.elapsedMs, 65001)
  c.timer.finish(first)
  c.advance(2000)
  c.timer.finish(first)
  assert.deepEqual(first.meta.timing, { elapsedMs: 65001, running: false })
  assert.equal(c.callbacks.size, 0)
  c.timer.start(second)
  c.timer.finish(first)
  c.advance(3000)
  assert.equal(second.meta.timing.elapsedMs, 3000)
  assert.equal(first.meta.status, 'streaming')
  c.timer.dispose()
  assert.equal(second.meta.timing.running, false)
  assert.equal(c.callbacks.size, 0)
})

test('格式化秒和分钟', () => {
  for (const [ms, text] of [[0, '0 秒'], [999, '0 秒'], [59999, '59 秒'], [60000, '1 分 00 秒'], [65000, '1 分 05 秒']]) {
    assert.equal(formatReplyDuration(ms), text)
  }
})

// Run the actual send flow and all three response handlers with controlled I/O.
function chat(model, streaming, isNew = false) {
  const c = clock()
  let controller, resolvePost, rejectPost
  const stream = new ReadableStream({ start(value) { controller = value } })
  const response = new Promise((resolve, reject) => { resolvePost = resolve; rejectPost = reject })
  const errors = []
  const state = {
    loading: ref(false), savingModel: ref(false), selectedModel: ref(model),
    medicalReportReady: ref(true), isRecording: ref(false), isTranscribing: ref(false),
    inputMessage: ref('问题'), currentMessages: ref([]), tempSession: ref(isNew),
    currentSessionId: ref(isNew ? 'temp' : 'session'),
    sessions: ref(isNew ? {} : { session: { messages: [] } }),
    isStreaming: ref(streaming), medicalEvents: ref([]), medicalReportId: ref(''),
    pendingMedicalMessage: ref(null), medicalQuestion: ref(''), medicalRequestStatus: ref(''),
    replyTimer: c.timer, reactive, nextTick, scrollToBottom: () => {},
    ElMessage: { error: message => errors.push(message), warning: message => errors.push(message) },
    getApiErrorMessage: (error, fallback) => error.message || fallback,
    localStorage: { getItem: () => 'test-token' }, document: { hidden: false },
    requestAnimationFrame: callback => callback(),
    fetch: async () => ({ ok: true, body: stream }),
    api: { post: (url, body, options) => {
      options?.signal.addEventListener('abort', () => rejectPost(new Error('aborted')))
      return response
    } },
  }
  const source = readFileSync(new URL('../src/views/AIChat.vue', import.meta.url), 'utf8')
  const handlers = source.slice(source.indexOf('const sendMessage ='), source.indexOf('    const scrollToBottom'))
  const flow = new Function(...Object.keys(state), `let medicalAbort; ${handlers}; return { sendMessage, cancel: () => medicalAbort?.abort() }`)(...Object.values(state))
  return {
    ...c, state, errors, ...flow,
    resolve: data => resolvePost({ data }), reject: rejectPost,
    send: text => controller.enqueue(new TextEncoder().encode(text)),
    close: () => controller.close(),
  }
}

for (const model of ['qwen', 'rag', 'medical_agent']) {
  for (const isNew of [false, true]) {
    test(`${model} 非流式${isNew ? '新' : '已有'}会话立即占位、定格并保留缓存`, async () => {
      const c = chat(model, false, isNew)
      const request = c.sendMessage()
      await setImmediate()
      assert.equal(c.state.currentMessages.value.length, 2)
      const assistant = c.state.currentMessages.value[1]
      assert.equal(assistant.content, '')
      c.advance(65000)
      assert.equal(assistant.meta.timing.elapsedMs, 65000)
      c.resolve({ status_code: 1000, sessionId: 'session', Information: '回答', run: { status: 'completed' } })
      await request
      assert.equal(assistant.content, '回答')
      assert.deepEqual(assistant.meta.timing, { elapsedMs: 65000, running: false })
      assert.equal(c.callbacks.size, 0)
      assert.equal(c.state.sessions.value.session.messages.length, 2)
      assert.equal(c.state.sessions.value.session.messages[1], assistant)
      assert.deepEqual(c.errors, [])
    })
  }
}

for (const model of ['qwen', 'medical_agent']) {
  test(`${model} 流式在终止事件定格，EOF 不覆盖用时`, async () => {
    const c = chat(model, true)
    const request = c.sendMessage()
    await setImmediate()
    c.advance(12000)
    c.send(model === 'qwen' ? 'data: 回答\n\n' : 'data: {"type":"answer_delta","delta":"回答"}\n\n')
    await setImmediate()
    const assistant = c.state.currentMessages.value[1]
    assert.equal(assistant.content, '回答')
    assert.equal(assistant.meta.timing.running, true)
    c.advance(2000)
    c.send(model === 'qwen' ? 'data: [DONE]\n\n' : 'data: {"type":"run_end","status":"completed"}\n\ndata: [DONE]\n\n')
    await setImmediate()
    assert.equal(assistant.meta.timing.running, false)
    c.advance(9000)
    c.close()
    await request
    assert.equal(assistant.meta.timing.elapsedMs, 14000)
    assert.equal(c.callbacks.size, 0)
  })

  test(`${model} 请求失败保留错误和最终用时，下一次发送独立计时`, async () => {
    const c = chat(model, false)
    const request = c.sendMessage()
    await setImmediate()
    c.advance(4000)
    c.reject(new Error('请求失败'))
    await request
    const first = c.state.currentMessages.value[1]
    assert.ok(first.content)
    assert.deepEqual(first.meta.timing, { elapsedMs: 4000, running: false })
    c.state.inputMessage.value = '再试一次'
    const retry = c.sendMessage()
    assert.equal(c.state.currentMessages.value[3].meta.timing.elapsedMs, 0)
    await retry
    assert.equal(first.meta.timing.elapsedMs, 4000)
    assert.equal(c.callbacks.size, 0)
  })
}

test('Agent 手动停止后保留消息和用时', async () => {
  const c = chat('medical_agent', false)
  const request = c.sendMessage()
  await setImmediate()
  c.advance(5000)
  c.cancel()
  await request
  assert.equal(c.state.medicalRequestStatus.value, 'stopped')
  assert.equal(c.state.currentMessages.value[1].content, '已停止本次回答。')
  assert.deepEqual(c.state.currentMessages.value[1].meta.timing, { elapsedMs: 5000, running: false })
  assert.equal(c.callbacks.size, 0)
})

test('普通流式连接提前结束时也清理计时', async () => {
  const c = chat('qwen', true)
  const request = c.sendMessage()
  await setImmediate()
  c.advance(7000)
  c.close()
  await request
  assert.deepEqual(c.state.currentMessages.value[1].meta.timing, { elapsedMs: 7000, running: false })
  assert.equal(c.callbacks.size, 0)
})


for (const action of ['switchSession', 'syncHistory']) {
  test(`${action} 恢复持久化用时，保留零值并隐藏用户消息用时`, async () => {
    const state = {
      loading: ref(false), savingModel: ref(false), sessionsOpened: ref(false),
      medicalEvents: ref([]), medicalQuestion: ref(''), medicalRequestStatus: ref(''),
      currentSessionId: ref('session'), tempSession: ref(false), selectedModel: ref('qwen'),
      sessions: ref({ session: { messages: [], modelType: 'qwen' } }), currentMessages: ref([]),
      nextTick, scrollToBottom: () => {}, restoreHistoryMessage,
      ElMessage: { warning: assert.fail, error: assert.fail },
      api: { post: async () => ({ data: { status_code: 1000, history: [
        { is_user: true, content: 'question', duration_ms: 500 },
        { is_user: false, content: 'zero', duration_ms: 0 },
        { is_user: false, content: 'answer', duration_ms: 65000 },
      ] } }) },
    }
    const source = readFileSync(new URL('../src/views/AIChat.vue', import.meta.url), 'utf8')
    const handlers = source.slice(source.indexOf('const switchSession ='), source.indexOf('const sendMessage ='))
    const run = new Function(...Object.keys(state), `${handlers}; return ${action}`)(...Object.values(state))
    await run('session')
    const messages = state.currentMessages.value
    assert.equal(messages.length, 3)
    assert.equal(messages[0].meta, undefined)
    assert.deepEqual(messages[1].meta.timing, { elapsedMs: 0, running: false })
    assert.deepEqual(messages[2].meta.timing, { elapsedMs: 65000, running: false })
    assert.equal(formatReplyDuration(messages[2].meta.timing.elapsedMs), '1 分 05 秒')
    assert.equal(state.sessions.value.session.messages[2], messages[2])
  })
}
