import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { setImmediate } from 'node:timers/promises'
import { createRequire } from 'node:module'

const require = createRequire(import.meta.url)
const { ref, nextTick, createRenderer } = require('vue')
const { compile } = require('@vue/compiler-dom')
const Vue = require('vue')
const markdownSource = readFileSync(new URL('../src/utils/markdown.js', import.meta.url), 'utf8')
  .replace(/^import .*$/gm, '').replace('export function', 'function')
const renderMarkdown = new Function('MarkdownIt', `${markdownSource}; return renderMarkdown`)(require('markdown-it'))

// Exercise the real request handler with a controlled SSE connection.
function medicalChat(isStreaming = true, responseData) {
  const source = readFileSync(new URL('../src/views/AIChat.vue', import.meta.url), 'utf8')
  const handler = source.slice(source.indexOf('async function handleMedical('), source.indexOf('async function handleStreaming('))
  const errors = []
  let controller
  const body = new ReadableStream({ start(value) { controller = value } })
  const state = {
    pendingMedicalMessage: ref(null), currentMessages: ref([]), medicalEvents: ref([]),
    medicalQuestion: ref(''), medicalRequestStatus: ref(''), medicalReportId: ref(''),
    tempSession: ref(false), currentSessionId: ref('session'),
    sessions: ref({ session: { messages: [] } }), isStreaming: ref(isStreaming),
    nextTick, scrollToBottom: () => {},
    localStorage: { getItem: () => 'test-token' },
    fetch: async () => ({ ok: true, body }),
    api: { post: async () => ({ data: responseData }) },
    ElMessage: { error: value => errors.push(value) }, getApiErrorMessage: e => e.message,
  }
  const handleMedical = new Function(...Object.keys(state), `let medicalAbort; ${handler}; return handleMedical;`)(...Object.values(state))
  return {
    state, errors, run: () => handleMedical('解释指标'),
    send: text => controller.enqueue(new TextEncoder().encode(text)),
    event: value => controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(value)}\n\n`)),
    close: () => controller.close(),
  }
}

function mountMessages(state) {
  const source = readFileSync(new URL('../src/views/AIChat.vue', import.meta.url), 'utf8')
  const template = source.slice(source.indexOf('<div class="chat-messages"'), source.indexOf('<div class="chat-input"'))
  const html = []
  const node = (tag, text = '') => ({ tag, text, children: [], props: {} })
  const renderer = createRenderer({
    createElement: node, createText: text => node('text', text), createComment: text => node('comment', text),
    setText: (n, text) => { n.text = text },
    setElementText: (n, text) => { n.text = text; n.children = [] },
    parentNode: n => n.parent, nextSibling: () => null,
    insert: (n, parent) => { n.parent = parent; parent.children.push(n) },
    remove: n => { if (n.parent) n.parent.children = n.parent.children.filter(child => child !== n) },
    patchProp: (n, key, previous, value) => {
      n.props[key] = value
      if (key === 'innerHTML') html.push(value)
    },
  })
  const app = renderer.createApp({
    setup: () => ({ ...state, loading: ref(true), messagesRef: ref(null), playTTS: () => {}, renderMarkdown }),
    render: new Function('Vue', compile(template, { mode: 'function', prefixIdentifiers: true }).code)(Vue),
  })
  app.mount({ children: [] })
  return { html, unmount: () => app.unmount() }
}

test('真实 Vue 消息模板在请求结束前渲染正文分片', async t => {
  const chat = medicalChat()
  const view = mountMessages(chat.state)
  t.after(() => view.unmount())
  const done = chat.run()
  chat.event({ type: 'run_start' })
  await setImmediate()
  chat.event({ type: 'answer_delta', delta: '第一片' })
  await setImmediate()
  await nextTick()
  assert.equal(view.html.at(-1), '<p>第一片</p>\n')
  assert.equal(chat.state.medicalRequestStatus.value, 'running')
  chat.event({ type: 'answer_delta', delta: '第二片' })
  await setImmediate()
  await nextTick()
  assert.equal(view.html.at(-1), '<p>第一片第二片</p>\n')
  chat.event({ type: 'answer_final', delta: '最终答案' })
  chat.event({ type: 'run_end', status: 'completed' })
  chat.send('data: [DONE]\n\n')
  chat.close()
  await done
  await nextTick()
  assert.equal(view.html.at(-1), '<p>最终答案</p>\n')
  assert.deepEqual(chat.errors, [])
})

test('SSE 分片立即追加，最终答案替换并与会话缓存一致', async () => {
  const chat = medicalChat()
  const done = chat.run()
  chat.event({ type: 'run_start' })
  // Split a frame across network reads as well as splitting the model text.
  chat.send('data: {"type":"answer_delta","delta":"结论[know"}')
  await setImmediate()
  assert.equal(chat.state.currentMessages.value[0].content, '')
  chat.send('\n\n')
  await setImmediate()
  assert.equal(chat.state.currentMessages.value[0].content, '结论[know')
  assert.equal(chat.state.medicalRequestStatus.value, 'running')
  chat.event({ type: 'answer_delta', delta: 'ledge_example]。' })
  await setImmediate()
  assert.equal(chat.state.currentMessages.value[0].content, '结论[knowledge_example]。')
  chat.event({ type: 'answer_final', delta: '结论。' })
  chat.event({ type: 'run_end', status: 'completed' })
  chat.send('data: [DONE]\n\n')
  chat.close()
  await done
  assert.equal(chat.state.currentMessages.value[0].content, '结论。')
  assert.equal(chat.state.sessions.value.session.messages[0].content, '结论。')
  assert.equal(chat.state.medicalRequestStatus.value, 'completed')
  assert.deepEqual(chat.errors, [])
})

test('部分失败的最终兜底替换已显示正文', async () => {
  const chat = medicalChat()
  const done = chat.run()
  chat.event({ type: 'answer_delta', delta: '未完成的正文' })
  await setImmediate()
  assert.equal(chat.state.currentMessages.value[0].content, '未完成的正文')
  chat.event({ type: 'answer_final', delta: '本次执行未完成，请重试。' })
  chat.event({ type: 'run_end', status: 'partial' })
  chat.send('data: [DONE]\n\n')
  chat.close()
  await done
  assert.equal(chat.state.currentMessages.value[0].content, '本次执行未完成，请重试。')
  assert.equal(chat.state.currentMessages.value[0].meta.status, 'partial')
  assert.equal(chat.state.medicalRequestStatus.value, 'partial')
  assert.deepEqual(chat.errors, [])
})

test('关闭流式时仍直接显示完整响应', async () => {
  const chat = medicalChat(false, {
    status_code: 1000, sessionId: 'session', Information: '完整回答',
    run: { status: 'completed', eventsJson: '[{"type":"answer_final","delta":"完整回答"}]' },
  })
  await chat.run()
  assert.equal(chat.state.currentMessages.value[0].content, '完整回答')
  assert.equal(chat.state.medicalRequestStatus.value, 'completed')
  assert.deepEqual(chat.errors, [])
  chat.close()
})
