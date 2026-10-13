// Exercise the actual component setup; HTTP is mocked, DOM layout is not tested.
const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const vue = require('vue')
const { parse, compileScript } = require('@vue/compiler-sfc')
const { transformSync } = require('@babel/core')
const descriptor = parse(fs.readFileSync(path.join(__dirname, '../src/views/Memory.vue'), 'utf8')).descriptor
const compiled = transformSync(compileScript(descriptor, { id: 'memory-test' }).content, {
  babelrc: false, configFile: false, plugins: [require.resolve('@babel/plugin-transform-modules-commonjs')]
}).code
function fixture(overrides = {}, confirm = async () => {}) {
  const requests = []
  const api = {
    get: async () => ({ data: { contentMarkdown: '原文', version: 1, updatedAt: '2026-09-20T00:00:00Z' } }),
    put: async (url, body) => { requests.push(body); return { data: { contentMarkdown: body.contentMarkdown, version: body.expectedVersion + 1 } } },
    ...overrides
  }
  const exports = {}
  vm.runInNewContext(compiled, { exports, require(name) {
    if (name === 'vue') return { ...vue, onMounted() {} }
    if (name === '../utils/api') return { __esModule: true, default: api, getApiErrorMessage: (e, fallback) => e.message || fallback }
    if (name === '../utils/markdown') return { renderMarkdown: String }
    if (name === 'element-plus') return { ElMessage: { success() {} }, ElMessageBox: { confirm } }
    throw new Error(name)
  } })
  return { state: exports.default.setup({}, { expose() {} }), requests }
}
test('cancel leaves saved document unchanged; save sends original version', async () => {
  const { state: s, requests } = fixture()
  await s.load(); s.openEditor(); s.draft.value = '新文档'
  assert.equal(s.doc.value.contentMarkdown, '原文')
  s.editing.value = false; s.openEditor(); assert.equal(s.draft.value, '原文')
  s.draft.value = '新文档'; await s.save(s.draft.value)
  assert.equal(requests[0].expectedVersion, 1)
  assert.equal(s.doc.value.contentMarkdown, '新文档'); assert.equal(s.editing.value, false)
})
test('conflict preserves draft and requires loading and merging latest document', async () => {
  let calls = 0, puts = 0
  const { state: s } = fixture({
    get: async () => ({ data: { contentMarkdown: ++calls === 1 ? '原文' : '其他会话的新文档', version: calls } }),
    put: async (url, body) => { if (++puts === 1) throw { response: { status: 409 }, message: '版本冲突' }; return { data: { contentMarkdown: body.contentMarkdown, version: body.expectedVersion + 1 } } }
  })
  await s.load(); s.openEditor(); s.draft.value = '我的草稿'; await s.save(s.draft.value)
  assert.equal(s.draft.value, '我的草稿'); assert.equal(s.conflict.value, true)
  await s.save('不允许覆盖'); assert.equal(puts, 1)
  await s.reloadForMerge()
  assert.equal(s.doc.value.contentMarkdown, '其他会话的新文档')
  assert.equal(s.draft.value, '我的草稿'); assert.equal(s.expectedVersion.value, 2)
  await s.save(s.draft.value); assert.equal(puts, 1)
  s.draft.value += '\n其他会话的新文档'; s.merged.value = true
  await s.save(s.draft.value); assert.equal(s.doc.value.version, 3)
})
test('failed saves retain input; Unicode character limit rejects excess; retry works', async () => {
  let fail = true
  const { state: s } = fixture({ put: async (url, body) => { if (fail) throw Error('服务不可用'); return { data: { contentMarkdown: body.contentMarkdown, version: 2 } } } })
  await s.load(); s.openEditor(); s.draft.value = '😀'.repeat(8001)
  assert.equal(s.count.value, 8001); await s.save(s.draft.value); assert.equal(s.saveError.value, '')
  s.draft.value = '😀'.repeat(8000); await s.save(s.draft.value)
  assert.equal(s.saveError.value, '服务不可用'); assert.equal(s.editing.value, true)
  assert.equal(s.expectedVersion.value, 1); fail = false
  await s.save(s.draft.value); assert.equal(s.doc.value.version, 2)
})
test('clear requires confirmation and saves an empty document with a version', async () => {
  let cancel = true
  const { state: s, requests } = fixture({}, async () => { if (cancel) throw Error('cancel') })
  await s.load(); s.openEditor(); await s.clear(); assert.equal(requests.length, 0)
  cancel = false; await s.clear()
  assert.equal(requests[0].contentMarkdown, ''); assert.equal(requests[0].expectedVersion, 1)
  assert.equal(s.doc.value.version, 2)
})
test('failed load can retry and an empty document can be edited', async () => {
  let fail = true
  const { state: s } = fixture({ get: async () => { if (fail) throw Error('offline'); return { data: { contentMarkdown: '', version: 0 } } } })
  await s.load(); assert.equal(s.loaded.value, false); assert.equal(s.error.value, 'offline')
  fail = false; await s.load(); s.openEditor()
  assert.equal(s.loaded.value, true); assert.equal(s.expectedVersion.value, 0)
})
