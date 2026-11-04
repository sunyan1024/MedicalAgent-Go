// Parse complete SSE events; network chunks may split UTF-8 characters or frames.
export async function consumeChatStream(body, { onSession, onDelta }) {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let completed = false
  const accept = frame => {
    let event = 'message'
    const lines = []
    for (const line of frame.split(/\r?\n/)) {
      if (line.startsWith('event:')) event = line.slice(6).trim()
      if (line.startsWith('data:')) {
        const value = line.slice(5)
        lines.push(value.startsWith(' ') ? value.slice(1) : value)
      }
    }
    if (!lines.length) return
    const data = lines.join('\n')
    if (event === 'error') {
      let message = data
      try { message = JSON.parse(data).message || data } catch { /* plain error */ }
      throw new Error(message)
    }
    if (event === 'delta') {
      const payload = JSON.parse(data)
      if (typeof payload.delta !== 'string') throw new Error('无效的流式文本')
      onDelta(payload.delta)
    } else if (data === '[DONE]') {
      completed = true
    } else {
      const payload = JSON.parse(data)
      if (payload.sessionId) onSession(payload.sessionId)
    }
  }
  try {
    while (!completed) {
      const { done, value } = await reader.read()
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
      let boundary
      while ((boundary = /\r?\n\r?\n/.exec(buffer))) {
        const frame = buffer.slice(0, boundary.index)
        buffer = buffer.slice(boundary.index + boundary[0].length)
        accept(frame)
        if (completed) break
      }
      if (done) break
    }
    if (!completed) throw new Error('流式连接提前结束，请重试')
  } finally {
    try { await reader.cancel() } finally { reader.releaseLock() }
  }
}
