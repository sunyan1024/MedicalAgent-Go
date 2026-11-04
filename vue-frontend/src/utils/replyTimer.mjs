export function formatReplyDuration(elapsedMs) {
  const seconds = Math.floor(Math.max(0, elapsedMs) / 1000)
  if (seconds < 60) return `${seconds} 秒`
  return `${Math.floor(seconds / 60)} 分 ${String(seconds % 60).padStart(2, '0')} 秒`
}

// The monotonic clock measures elapsed time; interval delays never accumulate.
export function createReplyTimer({
  now = () => performance.now(),
  schedule = callback => setInterval(callback, 1000),
  unschedule = id => clearInterval(id),
} = {}) {
  let active = null
  function finish(message) {
    if (!active || active.message !== message) return
    const { startedAt, interval } = active
    message.meta.timing.elapsedMs = Math.max(0, now() - startedAt)
    message.meta.timing.running = false
    if (message.meta.reasoning?.running) {
      message.meta.reasoning.elapsedMs = Math.max(0, now() - message.meta.reasoning.startedAt)
      message.meta.reasoning.running = false
      message.meta.reasoning.interrupted = true
    }
    unschedule(interval)
    active = null
  }
  return {
    start(message) {
      if (active) finish(active.message)
      message.meta.timing = { elapsedMs: 0, running: true }
      const startedAt = now()
      const interval = schedule(() => {
        if (active?.message === message) {
          message.meta.timing.elapsedMs = Math.max(0, now() - startedAt)
          if (message.meta.reasoning?.running) message.meta.reasoning.elapsedMs = Math.max(0, now() - message.meta.reasoning.startedAt)
        }
      })
      active = { message, startedAt, interval }
    },
    startReasoning(message) {
      if (!active || active.message !== message || message.meta.reasoning) return
      message.meta.reasoning = { startedAt: now(), elapsedMs: 0, running: true }
    },
    finishReasoning(message) {
      const r = message.meta.reasoning
      if (!r?.running) return
      r.elapsedMs = Math.max(0, now() - r.startedAt)
      r.running = false
    },
    finish,
    dispose() {
      if (active) finish(active.message)
    },
  }
}

// Historical assistant messages use the persisted server duration.
export function restoreHistoryMessage(item) {
  const message = { role: item.role, content: item.content }
  if (item.role === 'assistant') {
    message.meta = { timing: { elapsedMs: item.duration_ms, running: false } }
  }
  return message
}
