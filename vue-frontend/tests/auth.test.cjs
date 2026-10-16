const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const vm = require('node:vm')
const { transformSync } = require('@babel/core')
const source = fs.readFileSync(require('node:path').join(__dirname, '../src/utils/auth.js'), 'utf8')
const compiled = transformSync(source, { babelrc: false, configFile: false, plugins: [require.resolve('@babel/plugin-transform-modules-commonjs')] }).code
const success = uid => ({ data: { status_code: 1000, user_info: { uid, role: 'user' } } })
function fixture() {
  const storage = new Map([['token', 'a']])
  const requests = []
  const exports = {}
  const api = { get: url => new Promise((resolve, reject) => requests.push({ url, resolve, reject })) }
  const localStorage = { getItem: key => storage.get(key) ?? null, setItem: (key, value) => storage.set(key, value), removeItem: key => storage.delete(key) }
  vm.runInNewContext(compiled, { exports, localStorage, require: name => name === 'vue' ? require('vue') : { __esModule: true, default: api } })
  return { ...exports, storage, requests }
}
test('concurrent requests and navigation share cache; reload ignores persisted user', async () => {
  const f = fixture()
  const first = f.loadCurrentUser()
  const second = f.loadCurrentUser()
  assert.equal(f.requests.length, 1)
  assert.equal(f.requests[0].url, '/user/me')
  f.requests[0].resolve(success('a'))
  assert.equal(await first, await second)
  assert.equal(await first, await f.loadCurrentUser())
  assert.equal(f.requests.length, 1)
  const reload = fixture()
  reload.storage.set('userInfo', f.storage.get('userInfo'))
  const pending = reload.loadCurrentUser()
  assert.equal(reload.requests.length, 1)
  reload.requests[0].resolve(success('a'))
  await pending
})
test('network and business failures allow retry', async () => {
  for (const network of [true, false]) {
    const f = fixture()
    const rejected = assert.rejects(f.loadCurrentUser())
    if (network) f.requests[0].reject(new Error('network'))
    else f.requests[0].resolve({ data: { status_code: 2006 } })
    await rejected
    const retry = f.loadCurrentUser()
    assert.equal(f.requests.length, 2)
    f.requests[1].resolve(success('a'))
    await retry
  }
})
test('clear invalidates pending results even if token is unchanged', async () => {
  const f = fixture()
  const rejected = assert.rejects(f.loadCurrentUser(), /登录状态已改变/)
  f.clearUser()
  const next = f.loadCurrentUser()
  f.requests[0].resolve(success('old'))
  await rejected
  assert.equal(f.currentUser.value, null)
  const shared = f.loadCurrentUser()
  assert.equal(f.requests.length, 2)
  f.requests[1].resolve(success('new'))
  await Promise.all([next, shared])
  assert.equal(f.currentUser.value.uid, 'new')
})
test('token changes prevent stale writes; missing token clears cache without HTTP', async () => {
  const f = fixture()
  const rejected = assert.rejects(f.loadCurrentUser(), /登录状态已改变/)
  f.storage.set('token', 'b')
  const next = f.loadCurrentUser()
  f.requests[1].resolve(success('b'))
  await next
  f.requests[0].resolve(success('a'))
  await rejected
  assert.equal(f.currentUser.value.uid, 'b')
  assert.equal(JSON.parse(f.storage.get('userInfo')).uid, 'b')
  f.storage.delete('token')
  await assert.rejects(f.loadCurrentUser(), /请先登录/)
  assert.equal(f.currentUser.value, null)
  assert.equal(f.storage.has('userInfo'), false)
  assert.equal(f.requests.length, 2)
})
