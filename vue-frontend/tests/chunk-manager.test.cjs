// Exercise the actual component setup with Vue reactivity and mocked HTTP.
// This covers async state, not DOM rendering or browser layout.
const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const vue = require('vue')
const { parse } = require('@vue/compiler-sfc')
const { transformSync } = require('@babel/core')

const source = fs.readFileSync(path.join(__dirname, '../src/components/ChunkManager.vue'), 'utf8')
const script = parse(source).descriptor.script.content
const compiled = transformSync(script, { babelrc: false, configFile: false, plugins: [require.resolve('@babel/plugin-transform-modules-commonjs')] }).code
const response = data => ({ data: { status_code: 1000, ...data } })
const row = { id: 'doc_1', chunk_idx: 1, content: '原始正文', version: 1, enabled: true }
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
const flush = async () => { await new Promise(resolve => setImmediate(resolve)); await vue.nextTick() }

function fixture(t, overrides = {}, confirm = async () => {}) {
  const requests = []
  const api = {
    get: async url => response(url.endsWith('/doc_1') ? { chunk: { ...row } } : { chunks: [row], total: 1 }),
    post: async (url, body) => { requests.push({ method: 'POST', url, body }); return response({ chunk: { ...row, id: 'doc_2', content: body.content } }) },
    put: async (url, body) => { requests.push({ method: 'PUT', url, body }); return response({ chunk: { ...row, content: body.content, version: 2 } }) },
    patch: async (url, body) => { requests.push({ method: 'PATCH', url, body }); return response({ chunk: { ...row, enabled: body.enabled, version: 2 } }) },
    delete: async (url, body) => { requests.push({ method: 'DELETE', url, body }); return response({}) },
    ...overrides
  }
  const exports = {}
  const sandbox = { exports, require: name => {
    if (name === 'vue') return vue
    if (name === '@element-plus/icons-vue') return require(name)
    if (name === 'element-plus') return { ElMessage: { success() {}, info() {} }, ElMessageBox: { confirm } }
    if (name === '../utils/api') return { __esModule: true, default: api, getApiErrorMessage: (e, fallback) => e.response?.data?.status_msg || e.message || fallback }
    throw new Error(`Unexpected dependency: ${name}`)
  } }
  vm.runInNewContext(compiled, sandbox)
  const props = vue.reactive({ modelValue: true, file: { id: 'file-a', original_name: 'A.txt' } })
  const scope = vue.effectScope()
  const state = scope.run(() => exports.default.setup(props))
  t.after(() => scope.stop())
  return { state, props, requests }
}

test('list and detail responses from the previous file cannot overwrite current state', async t => {
  const oldList = deferred(), oldDetail = deferred()
  const { state, props } = fixture(t, { get: url => {
    if (url.includes('/file-a/') && url.endsWith('/doc_1')) return oldDetail.promise
    if (url.includes('/file-a/')) return oldList.promise
    return Promise.resolve(response({ chunks: [{ ...row, id: 'doc_8' }], total: 1 }))
  } })
  const opening = state.openChunk(row)
  await flush()
  props.file = { id: 'file-b', original_name: 'B.txt' }
  await flush()
  oldList.resolve(response({ chunks: [row], total: 99 }))
  oldDetail.resolve(response({ chunk: row }))
  await opening; await flush()
  assert.equal(state.chunks.value[0].id, 'doc_8')
  assert.equal(state.total.value, 1)
  assert.equal(state.mode.value, '')
  assert.equal(state.selected.value, null)
  assert.equal(state.loading.value, false)
})

test('failed saves preserve input and version; successful edits submit that version', async t => {
  let failed = true
  const payloads = []
  const { state } = fixture(t, { put: async (url, body) => {
    payloads.push(body)
    if (failed) throw { response: { status: 409, data: { status_msg: '切片已变更，请重新加载' } } }
    return response({ chunk: { ...row, content: body.content, version: 2 } })
  } })
  await flush(); await state.openChunk(row)
  state.mode.value = 'edit'; state.content.value = '尚未提交的修改'
  await state.save()
  assert.equal(state.mode.value, 'edit')
  assert.equal(state.content.value, '尚未提交的修改')
  assert.equal(state.selected.value.version, 1)
  assert.match(state.actionError.value, /切片已变更/)
  assert.equal(state.busy.value, false)
  failed = false
  await state.save()
  assert.equal(payloads[1].version, 1)
  assert.equal(state.selected.value.version, 2)
  assert.equal(state.mode.value, 'view')
})

test('unsaved close can be cancelled, and pending saves prevent closure', async t => {
  let discard = false
  const save = deferred()
  const { state } = fixture(t, { post: () => save.promise }, async () => { if (!discard) throw new Error('cancel') })
  await flush(); await state.newChunk(); state.content.value = '新正文'
  let closed = false
  await state.beforeClose(() => { closed = true })
  assert.equal(closed, false)
  assert.equal(state.content.value, '新正文')
  const pending = state.save(); await flush()
  discard = true
  await state.beforeClose(() => { closed = true })
  assert.equal(closed, false)
  save.resolve(response({ chunk: { ...row, content: '新正文' } }))
  await pending
  await state.beforeClose(() => { closed = true })
  assert.equal(closed, true)
})

test('delete confirmation cannot apply an old file action to a newly opened file', async t => {
  const confirmation = deferred()
  const { state, props, requests } = fixture(t, {}, () => confirmation.promise)
  await flush()
  const removal = state.removeChunk(row); await flush()
  props.file = { id: 'file-b', original_name: 'B.txt' }; await flush()
  confirmation.resolve(); await removal
  assert.equal(requests.length, 0)
})

test('new content uses Unicode character limits and state changes send current version', async t => {
  const { state, requests } = fixture(t)
  await flush(); await state.newChunk()
  state.content.value = '医'.repeat(10001)
  await state.save(); assert.equal(requests.length, 0)
  state.content.value = '😀'.repeat(10000)
  assert.equal(state.charCount.value, 10000)
  await state.save(); assert.equal(requests[0].method, 'POST')
  await state.setEnabled(row)
  assert.equal(requests[1].body.version, 1)
  assert.equal(requests[1].body.enabled, false)
})


test('dev overlay ignores only the exact ResizeObserver notification', () => {
  const showError = require('../vue.config.js').devServer.client.overlay.runtimeErrors
  assert.equal(showError(new Error('ResizeObserver loop completed with undelivered notifications.')), false)
  assert.equal(showError(new TypeError('Cannot read properties of undefined')), true)
  assert.equal(showError(new Error('ResizeObserver application failure')), true)
  assert.equal(showError(new Error('Network Error')), true)
})

test('search, filters and pagination preserve the open editor and its draft', async t => {
  const params = []
  const { state } = fixture(t, { get: async (url, config) => {
    if (url.endsWith('/doc_1')) return response({ chunk: { ...row } })
    params.push(config.params)
    return response({ chunks: [row], total: 60 })
  } })
  await flush(); await state.openChunk(row)
  state.mode.value = 'edit'; state.content.value = '需要保留的草稿'
  state.keyword.value = '医学'
  state.search(); await flush()
  state.chooseFilter('false'); await flush()
  state.changePage(2); await flush()
  assert.equal(params.at(-1).page, 2)
  assert.equal(params.at(-1).keyword, '医学')
  assert.equal(params.at(-1).enabled, 'false')
  assert.equal(state.mode.value, 'edit')
  assert.equal(state.selected.value.id, 'doc_1')
  assert.equal(state.content.value, '需要保留的草稿')
})

test('disabling a selected chunk keeps its refreshed detail open', async t => {
  const { state } = fixture(t)
  await flush(); await state.openChunk(row)
  await state.setEnabled(state.selected.value)
  assert.equal(state.mode.value, 'view')
  assert.equal(state.selected.value.id, 'doc_1')
  assert.equal(state.selected.value.enabled, false)
  assert.equal(state.selected.value.version, 2)
  assert.equal(state.content.value, row.content)
})
