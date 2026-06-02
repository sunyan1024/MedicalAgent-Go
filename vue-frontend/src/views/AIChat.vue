<template>
  <div class="ai-chat-container">
    <!-- 左侧会话列表 -->
    <div class="session-list">
      <div class="session-list-header">
        <span>会话列表</span>
        <button class="new-chat-btn" @click="createNewSession">＋ 新聊天</button>
      </div>
      <ul class="session-list-ul">
        <li v-for="session in sortedSessions" :key="session.id"
          :class="['session-item', { active: currentSessionId === session.id }]" @click="switchSession(session.id)">
          {{ session.title || `会话 ${session.id}` }}
        </li>
      </ul>
    </div>

    <!-- 右侧聊天区域 -->
    <div class="chat-section">
      <div class="top-bar">
        <button class="back-btn" @click="$router.push('/menu')">← 返回</button>
        <button class="sync-btn" @click="syncHistory" :disabled="!currentSessionId || tempSession">同步历史数据</button>
        <label for="modelType">选择模型：</label>
        <select id="modelType" v-model="selectedModel" class="model-select">
          <option value="qwen">阿里百炼</option>
          <option value="ollama">Ollama</option>
          <option value="rag">阿里百炼 RAG</option>
          <option value="mcp">阿里百炼 MCP</option>
        </select>
        <label for="streamingMode" style="margin-left: 20px;">
          <input type="checkbox" id="streamingMode" v-model="isStreaming" />
          流式响应
        </label>
        <button class="upload-btn" @click="triggerFileUpload" :disabled="uploading">📎 上传文档(.md/.txt/.pdf)</button>
        <input
          ref="fileInput"
          type="file"
          accept=".md,.txt,.pdf,text/markdown,text/plain,application/pdf"
          style="display: none"
          @change="handleFileUpload"
        />
      </div>

      <div class="chat-messages" ref="messagesRef">
        <div v-for="(message, index) in currentMessages" :key="index"
          :class="['message', message.role === 'user' ? 'user-message' : 'ai-message']">
          <div class="message-header">
            <b>{{ message.role === 'user' ? '你' : 'AI' }}:</b>
            <button v-if="message.role === 'assistant'" class="tts-btn" @click="playTTS(message.content)">🔊</button>
            <span v-if="message.meta && message.meta.status === 'streaming'" class="streaming-indicator"> ··</span>
          </div>
          <div class="message-content" v-html="renderMarkdown(message.content)"></div>
        </div>
      </div>

      <div class="chat-input">
        <textarea v-model="inputMessage" placeholder="请输入你的问题..." @keydown.enter.exact.prevent="sendMessage"
          :disabled="loading" ref="messageInput" rows="1"></textarea>
        <button type="button" :disabled="!inputMessage.trim() || loading" @click="sendMessage" class="send-btn">
          {{ loading ? '发送中...' : '发送' }}
        </button>
      </div>
    </div>
  </div>
</template>

<script>


import { ref, nextTick, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import api, { getApiErrorMessage } from '../utils/api'
import { calculateFileSHA256 } from '../utils/hash'

export default {
  name: 'AIChat',
  setup() {

    const sessions = ref({})
    const currentSessionId = ref(null)
    const tempSession = ref(false)
    const currentMessages = ref([])
    const inputMessage = ref('')
    const loading = ref(false)
    const messagesRef = ref(null)
    const messageInput = ref(null)
    const selectedModel = ref('qwen')
    const isStreaming = ref(false)
    const uploading = ref(false)
    const fileInput = ref(null)


    const renderMarkdown = (text) => {
      if (!text && text !== '') return ''
      return String(text)
        .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>')
        .replace(/\*(.*?)\*/g, '<em>$1</em>')
        .replace(/`(.*?)`/g, '<code>$1</code>')
        .replace(/\n/g, '<br>')
    }

    const playTTS = async (text) => {
      try {
        // 创建TTS任务
        const createResponse = await api.post('/AI/chat/tts', { text })
        if (createResponse.data && createResponse.data.status_code === 1000 && createResponse.data.task_id) {
          const taskId = createResponse.data.task_id
          
          // 先等待5秒钟再开始轮询
          await new Promise(resolve => setTimeout(resolve, 5000))
          
          // 轮询查询任务结果
          const maxAttempts = 30
          const pollInterval = 2000
          let attempts = 0
          
          const pollResult = async () => {
            const queryResponse = await api.get('/AI/chat/tts/query', { params: { task_id: taskId } })
            
            if (queryResponse.data && queryResponse.data.status_code === 1000) {
              const taskStatus = queryResponse.data.task_status
                
              if (taskStatus === 'Success' && queryResponse.data.task_result) {
                // 任务完成，播放音频
                // 后端返回的 task_result 是直接的 URL 字符串
                const audio = new Audio(queryResponse.data.task_result)
                audio.play()
                return true
              } else if (taskStatus === 'Running' || taskStatus === 'Created' ) {
                // 任务进行中，继续轮询
                attempts++
                if (attempts < maxAttempts) {
                  await new Promise(resolve => setTimeout(resolve, pollInterval))
                  return await pollResult()
                } else {
                  ElMessage.error('语音合成超时')
                  return true
                }
              } else {
                // 其他状态（如失败）
                ElMessage.error('语音合成失败')
                return true
              }
            }
            
            attempts++
            if (attempts < maxAttempts) {
              await new Promise(resolve => setTimeout(resolve, pollInterval))
              return await pollResult()
            } else {
              ElMessage.error('语音合成超时')
              return true
            }
          }
          
          await pollResult()
        } else {
          ElMessage.error('无法创建语音合成任务')
        }
      } catch (error) {
        console.error('TTS error:', error)
        ElMessage.error('请求语音接口失败')
      }
    }

    const loadSessions = async () => {
      try {
        const response = await api.get('/AI/chat/sessions')
        if (response.data && response.data.status_code === 1000 && Array.isArray(response.data.sessions)) {
          const sessionMap = {}
          response.data.sessions.forEach(s => {
            const sid = String(s.sessionId)
            sessionMap[sid] = {
              id: sid,
              // name: s.name || `会话 ${sid}`, // 已废弃，统一使用 title 字段
              title: s.title || `会话 ${sid}`, // 添加 title 字段
              updated_at: s.updated_at ? new Date(s.updated_at) : new Date(0), // 添加更新时间字段
              messages: [] // lazy load
            }
          })
          sessions.value = sessionMap
        }
      } catch (error) {
        console.error('Load sessions error:', error)
      }
    }

    // 添加一个新的计算属性，按更新时间降序排列会话
    const sortedSessions = computed(() => {
      return Object.values(sessions.value).sort((a, b) => {
        // 将新会话放在最前面
        if (a.id === 'temp') return -1
        if (b.id === 'temp') return 1

        // 按更新时间降序排列
        return b.updated_at - a.updated_at
      })
    })

    const createNewSession = () => {
      currentSessionId.value = 'temp'
      tempSession.value = true
      currentMessages.value = []
      // focus input
      nextTick(() => {
        if (messageInput.value) messageInput.value.focus()
      })
    }

    const switchSession = async (sessionId) => {
      if (!sessionId) return
      currentSessionId.value = String(sessionId)
      tempSession.value = false

      // lazy load history if not present
      if (!sessions.value[sessionId].messages || sessions.value[sessionId].messages.length === 0) {
        try {
          const response = await api.post('/AI/chat/history', { sessionId: currentSessionId.value })
          if (response.data && response.data.status_code === 1000 && Array.isArray(response.data.history)) {
            const messages = response.data.history.map(item => ({
              role: item.is_user ? 'user' : 'assistant',
              content: item.content
            }))
            sessions.value[sessionId].messages = messages
          }
        } catch (err) {
          console.error('Load history error:', err)
        }
      }


      currentMessages.value = [...(sessions.value[sessionId].messages || [])]
      await nextTick()
      scrollToBottom()
    }

    const syncHistory = async () => {
      if (!currentSessionId.value || tempSession.value) {
        ElMessage.warning('请选择已有会话进行同步')
        return
      }
      try {
        const response = await api.post('/AI/chat/history', { sessionId: currentSessionId.value })
        if (response.data && response.data.status_code === 1000 && Array.isArray(response.data.history)) {
          const messages = response.data.history.map(item => ({
            role: item.is_user ? 'user' : 'assistant',
            content: item.content
          }))
          sessions.value[currentSessionId.value].messages = messages
          currentMessages.value = [...messages]
          await nextTick()
          scrollToBottom()
        } else {
          ElMessage.error('无法获取历史数据')
        }
      } catch (err) {
        console.error('Sync history error:', err)
        ElMessage.error('请求历史数据失败')
      }
    }


    const sendMessage = async () => {
      if (!inputMessage.value || !inputMessage.value.trim()) {
        ElMessage.warning('请输入消息内容')
        return
      }

      const userMessage = {
        role: 'user',
        content: inputMessage.value
      }
      const currentInput = inputMessage.value
      inputMessage.value = ''


      currentMessages.value.push(userMessage)
      await nextTick()
      scrollToBottom()

      try {
        loading.value = true
        if (isStreaming.value) {

          await handleStreaming(currentInput)
        } else {

          await handleNormal(currentInput)
        }
      } catch (err) {
        console.error('Send message error:', err)
        ElMessage.error('发送失败，请重试')

        if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value] && sessions.value[currentSessionId.value].messages) {

          const sessionArr = sessions.value[currentSessionId.value].messages
          if (sessionArr && sessionArr.length) sessionArr.pop()
        }
        currentMessages.value.pop()
      } finally {
        if (!isStreaming.value) {
          loading.value = false
        }
        await nextTick()
        scrollToBottom()
      }
    }


    async function handleStreaming(question) {

      const aiMessage = {
        role: 'assistant',
        content: '',
        meta: { status: 'streaming' } // mark streaming
      }


      const aiMessageIndex = currentMessages.value.length
      currentMessages.value.push(aiMessage)

      if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value]) {
        if (!sessions.value[currentSessionId.value].messages) sessions.value[currentSessionId.value].messages = []
        sessions.value[currentSessionId.value].messages.push({ role: 'assistant', content: '' })
      }


      const url = tempSession.value
        ? '/api/AI/chat/send-stream-new-session'
        : '/api/AI/chat/send-stream'


      const headers = {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${localStorage.getItem('token') || ''}`
      }

      const body = tempSession.value
        ? { question: question, modelType: selectedModel.value }
        : { question: question, modelType: selectedModel.value, sessionId: currentSessionId.value }

      try {
        // 创建 fetch 连接读取 SSE 流
        const response = await fetch(url, {
          method: 'POST',
          headers,
          body: JSON.stringify(body)
        })

        if (!response.ok) {
          loading.value = false
          throw new Error('Network response was not ok')
        }

        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''

        // 读取流数据
        // eslint-disable-next-line no-constant-condition
        while (true) {
          const { done, value } = await reader.read()
          if (done) break

          const chunk = decoder.decode(value, { stream: true })
          buffer += chunk

          // 按行分割
          const lines = buffer.split('\n')
          buffer = lines.pop() || '' // 保留未完成的行

          for (const line of lines) {
            const trimmedLine = line.trim()
            if (!trimmedLine) continue

            // 处理 SSE 格式：data: <content>
            if (trimmedLine.startsWith('data:')) {
              const data = trimmedLine.slice(5).trim()
              console.log('[SSE] Received:', data) // 调试日志

              if (data === '[DONE]') {
                // 流结束
                console.log('[SSE] Stream done')
                loading.value = false

                if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value]) {
                  // 更新会话的更新时间
                  sessions.value[currentSessionId.value].updated_at = new Date()
                }

                currentMessages.value[aiMessageIndex].meta = { status: 'done' }
              } else if (data.startsWith('{')) {
                // 尝试解析 JSON（如 sessionId）
                try {
                  const parsed = JSON.parse(data)
                  if (parsed.sessionId) {
                    const newSid = String(parsed.sessionId)
                    console.log('[SSE] Session ID:', newSid)
                    if (tempSession.value) {
                      sessions.value[newSid] = {
                        id: newSid,
                        title: '新会话',
                        messages: [...currentMessages.value]
                      }
                      currentSessionId.value = newSid
                      tempSession.value = false
                    }
                  }
                } catch (e) {
                  // 不是 JSON，当作普通文本处理
                  currentMessages.value[aiMessageIndex].content += data
                  console.log('[SSE] Content updated:', currentMessages.value[aiMessageIndex].content.length)
                }
              } else {
                // 普通文本数据，直接追加
                // 使用数组索引直接更新，强制 Vue 响应式系统检测变化
                currentMessages.value[aiMessageIndex].content += data
                console.log('[SSE] Content updated:', currentMessages.value[aiMessageIndex].content.length)
              }
              // 页面隐藏时跳过 DOM 更新，避免触发浏览器窗口恢复
              if (!document.hidden) {
                currentMessages.value = [...currentMessages.value]
                await new Promise(resolve => {
                  requestAnimationFrame(() => {
                    scrollToBottom()
                    resolve()
                  })
                })
              }
            }
          }
        }

        // 流读取完成后的处理
        loading.value = false
        currentMessages.value[aiMessageIndex].meta = { status: 'done' }
        if (!document.hidden) {
          currentMessages.value = [...currentMessages.value]
        }

        // 同步到 sessions 存储
        if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value]) {
          const sessMsgs = sessions.value[currentSessionId.value].messages
          if (Array.isArray(sessMsgs) && sessMsgs.length) {
            const lastIndex = sessMsgs.length - 1
            if (sessMsgs[lastIndex] && sessMsgs[lastIndex].role === 'assistant') {
              sessMsgs[lastIndex].content = currentMessages.value[aiMessageIndex].content
            }
          }
        }
      } catch (err) {
        console.error('Stream error:', err)
        loading.value = false
        currentMessages.value[aiMessageIndex].meta = { status: 'error' }
        if (!document.hidden) {
          currentMessages.value = [...currentMessages.value]
        }
        ElMessage.error('流式传输出错')
      }
    }

    // 修改发送消息相关的函数，更新会话的更新时间戳
    async function handleNormal(question) {
      if (tempSession.value) {

        const response = await api.post('/AI/chat/send-new-session', {
          question: question,
          modelType: selectedModel.value
        })
        if (response.data && response.data.status_code === 1000) {
          const sessionId = String(response.data.sessionId)
          const aiMessage = {
            role: 'assistant',
            content: response.data.Information || ''
          }

          sessions.value[sessionId] = {
            id: sessionId,
            title: '新会话',
            messages: [ { role: 'user', content: question }, aiMessage ]
          }
          currentSessionId.value = sessionId
          tempSession.value = false
          currentMessages.value = [...sessions.value[sessionId].messages]
          // await loadSessions()  // 创建新会话后刷新会话列表
        } else {
          ElMessage.error(response.data?.status_msg || '发送失败')

          currentMessages.value.pop()
        }
      } else {

        const sessionMsgs = sessions.value[currentSessionId.value].messages

        sessionMsgs.push({ role: 'user', content: question })

        const response = await api.post('/AI/chat/send', {
          question: question,
          modelType: selectedModel.value,
          sessionId: currentSessionId.value
        })
        if (response.data && response.data.status_code === 1000) {
          const aiMessage = { role: 'assistant', content: response.data.Information || '' }
          sessionMsgs.push(aiMessage)

          // 更新会话的更新时间
          sessions.value[currentSessionId.value].updated_at = new Date()
          currentMessages.value = [...sessionMsgs]
        } else {
          ElMessage.error(response.data?.status_msg || '发送失败')
          sessionMsgs.pop() // rollback
          currentMessages.value.pop()
        }
      }
    }


    const scrollToBottom = () => {
      if (document.hidden) return
      if (messagesRef.value) {
        try {
          messagesRef.value.scrollTop = messagesRef.value.scrollHeight
        } catch (e) {
          // ignore
        }
      }
    }

    const triggerFileUpload = () => {
      if (fileInput.value) {
        fileInput.value.click()
      }
    }

    const handleFileUpload = async (event) => {
      const file = event.target.files[0]
      if (!file) return

      // 前端校验：只允许 .md、.txt 或 .pdf 文件
      const fileName = file.name.toLowerCase()
      if (!fileName.endsWith('.md') && !fileName.endsWith('.txt') && !fileName.endsWith('.pdf')) {
        ElMessage.error('只允许上传 .md、.txt 或 .pdf 文件')
        // 清空文件输入
        if (fileInput.value) {
          fileInput.value.value = ''
        }
        return
      }

      try {
        uploading.value = true
        const fileSHA256 = await calculateFileSHA256(file)
        const precheck = await api.post('/file/precheck', {
          original_name: file.name,
          file_sha256: fileSHA256,
          size: file.size
        })
        if (!precheck.data || precheck.data.status_code !== 1000) {
          ElMessage.error(precheck.data?.status_msg || '上传预检失败')
          return
        }
        if (precheck.data.reused) {
          ElMessage.success('秒传成功')
          return
        }
        if (!precheck.data.need_upload) {
          ElMessage.error('上传预检失败')
          return
        }

        const formData = new FormData()
        formData.append('file', file)
        formData.append('file_sha256', fileSHA256)

        // 使用axios的baseURL配置，无需手动添加/api前缀
        const response = await api.post('/file/upload', formData, 
          // 移除 headers 配置，让 axios 自动处理
          // {
          //   headers: {
          //     'Content-Type': 'multipart/form-data'
          //   }
          // }
        )

        if (response.data && response.data.status_code === 1000) {
          ElMessage.success(`文件上传成功`)
        } else {
          ElMessage.error(response.data?.status_msg || '上传失败')
        }
      } catch (error) {
        console.error('File upload error:', error)
        ElMessage.error(getApiErrorMessage(error, '文件上传失败'))
      } finally {
        uploading.value = false
        // 清空文件输入
        if (fileInput.value) {
          fileInput.value.value = ''
        }
      }
    }

    onMounted(() => {
      loadSessions()
    })

    // expose to template
    return {
      // sessions: computed(() => Object.values(sessions.value)),
      sortedSessions,
      currentSessionId,
      tempSession,
      currentMessages,
      inputMessage,
      loading,
      messagesRef,
      messageInput,
      selectedModel,
      isStreaming,
      uploading,
      fileInput,
      renderMarkdown,
      playTTS,
      createNewSession,
      switchSession,
      syncHistory,
      sendMessage,
      triggerFileUpload,
      handleFileUpload
    }
  }
}
</script>

<style scoped>
.ai-chat-container {
  height: 100vh;
  display: flex;
  background: var(--app-bg);
  overflow: hidden;
  color: var(--app-text);
}

.session-list {
  width: 280px;
  height: 100vh;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  background: var(--app-surface);
  border-right: 1px solid var(--app-border-soft);
}

.session-list-header {
  padding: 16px;
  font-weight: 700;
  border-bottom: 1px solid var(--app-border-soft);
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.new-chat-btn {
  width: 100%;
  height: 36px;
  cursor: pointer;
  background: var(--app-primary);
  color: white;
  border: none;
  border-radius: var(--app-radius);
  font-size: 14px;
  font-weight: 600;
  transition: background 0.18s ease;
}

.new-chat-btn:hover {
  background: var(--app-primary-hover);
}

.session-list-ul {
  list-style: none;
  padding: 0;
  margin: 0;
  flex: 1;
  overflow-y: auto;
}

.session-item {
  padding: 12px 16px;
  cursor: pointer;
  border-bottom: 1px solid var(--app-border-soft);
  transition: background 0.18s ease, color 0.18s ease;
  color: var(--app-text);
  font-size: 14px;
}

.session-item.active {
  background: var(--app-primary-soft);
  color: var(--app-primary);
  font-weight: 600;
}

.session-item:hover {
  background: var(--app-surface-subtle);
}

/* chat section */
.chat-section {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.top-bar {
  min-height: 64px;
  background: var(--app-surface);
  color: var(--app-text);
  display: flex;
  align-items: center;
  padding: 12px 20px;
  border-bottom: 1px solid var(--app-border-soft);
  gap: 12px;
  flex-wrap: wrap;
  font-size: 14px;
}

.back-btn,
.sync-btn,
.upload-btn {
  height: 34px;
  padding: 0 12px;
  border-radius: var(--app-radius);
  cursor: pointer;
  font-weight: 600;
  transition: background 0.18s ease, border-color 0.18s ease, color 0.18s ease;
}

.back-btn {
  background: var(--app-surface);
  border: 1px solid var(--app-border);
  color: var(--app-text);
}

.sync-btn {
  background: var(--app-success);
  color: white;
  border: none;
}

.upload-btn {
  background: var(--app-primary);
  color: white;
  border: none;
}

.back-btn:hover {
  border-color: #bfdbfe;
  color: var(--app-primary);
  background: var(--app-primary-soft);
}

.sync-btn:hover:not(:disabled) {
  background: #15803d;
}

.upload-btn:hover:not(:disabled) {
  background: var(--app-primary-hover);
}

.sync-btn:disabled,
.upload-btn:disabled {
  background: #cbd5e1;
  cursor: not-allowed;
}

.model-select {
  height: 34px;
  padding: 0 10px;
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  background: var(--app-surface);
  color: var(--app-text);
  cursor: pointer;
}

.chat-messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.message {
  max-width: 70%;
  padding: 12px 14px;
  border-radius: var(--app-radius);
  line-height: 1.6;
  word-wrap: break-word;
  font-size: 15px;
  box-sizing: border-box;
}

.user-message {
  align-self: flex-end;
  background: var(--app-primary);
  color: white;
}

.ai-message {
  align-self: flex-start;
  background: var(--app-surface);
  color: var(--app-text);
  border: 1px solid var(--app-border-soft);
}

.message-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.message-header b {
  font-weight: 600;
}

.tts-btn {
  width: 30px;
  height: 28px;
  border-radius: var(--app-radius);
  font-size: 12px;
  cursor: pointer;
  background: var(--app-surface-subtle);
  color: var(--app-text);
  border: 1px solid var(--app-border);
}

.tts-btn:hover {
  color: var(--app-primary);
  border-color: #bfdbfe;
}

.streaming-indicator {
  color: var(--app-text-muted);
  font-weight: 600;
  margin-left: 6px;
}

/* message content */
.message-content {
  white-space: pre-wrap;
  word-break: break-word;
}

/* input area */
.chat-input {
  padding: 16px 20px;
  background: var(--app-surface);
  border-top: 1px solid var(--app-border-soft);
  position: relative;
}

.chat-input textarea {
  width: 100%;
  resize: none;
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  padding: 12px 92px 12px 14px;
  font-size: 15px;
  outline: none;
  background: var(--app-surface);
  color: var(--app-text);
  transition: border-color 0.18s ease, box-shadow 0.18s ease;
  min-height: 48px;
  max-height: 160px;
}

.chat-input textarea:focus {
  border-color: #93c5fd;
  box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.08);
}

.send-btn {
  position: absolute;
  right: 30px;
  bottom: 26px;
  height: 32px;
  padding: 0 16px;
  border: none;
  border-radius: var(--app-radius);
  background: var(--app-primary);
  color: white;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.18s ease;
}

.send-btn:hover:not(:disabled) {
  background: var(--app-primary-hover);
}

.send-btn:disabled {
  background: #cbd5e1;
  cursor: not-allowed;
}

@media (max-width: 820px) {
  .ai-chat-container {
    flex-direction: column;
  }

  .session-list {
    width: 100%;
    height: auto;
    max-height: 220px;
    border-right: none;
    border-bottom: 1px solid var(--app-border-soft);
  }

  .top-bar {
    align-items: flex-start;
  }

  .message {
    max-width: 88%;
  }
}
</style>
