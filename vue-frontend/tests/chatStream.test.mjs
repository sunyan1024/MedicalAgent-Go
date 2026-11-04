import test from 'node:test'
import assert from 'node:assert/strict'
import { consumeChatStream } from '../src/utils/chatStream.mjs'

function stream(text) {
  const bytes = new TextEncoder().encode(text)
  return new ReadableStream({
    start(controller) {
      // Force splits inside frame delimiters and multibyte characters.
      for (const byte of bytes) controller.enqueue(Uint8Array.of(byte))
      controller.close()
    }
  })
}

test('preserves Markdown, whitespace, JSON text and control-like text across chunks', async () => {
  const parts = ['# 标题\n\n', '```js\n', '  const x = {"sessionId":"正文"}\n', '```\n', '[DONE]', '\n  ']
  let answer = ''
  let session
  const wire = 'data: {"sessionId":"new-id"}\r\n\r\n' + parts.map(delta =>
    `event: delta\ndata: ${JSON.stringify({ delta })}\n\n`).join('') + 'data: [DONE]\n\n'
  await consumeChatStream(stream(wire), { onSession: id => { session = id }, onDelta: s => { answer += s } })
  assert.equal(session, 'new-id')
  assert.equal(answer, parts.join(''))
})

test('rejects error events and incomplete streams', async () => {
  const handlers = { onSession() {}, onDelta() {} }
  await assert.rejects(consumeChatStream(stream('event: error\ndata: {"message":"模型失败"}\n\n'), handlers), /模型失败/)
  await assert.rejects(consumeChatStream(stream('event: delta\ndata: {"delta":"partial"}\n\n'), handlers), /提前结束/)
})

test('separates reasoning from answer text', async () => {
  let reasoning = '', answer = ''
  await consumeChatStream(stream('event: reasoning_delta\ndata: {"delta":"思考\\n"}\n\nevent: delta\ndata: {"delta":"回答"}\n\ndata: [DONE]\n\n'), {
    onSession() {}, onReasoning: s => { reasoning += s }, onDelta: s => { answer += s }
  })
  assert.equal(reasoning, '思考\n')
  assert.equal(answer, '回答')
})
