<template>
  <div class="ai-chat-container" :style="viewportStyle">
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

    <el-drawer v-model="sessionsOpened" title="会话列表" direction="ltr" size="min(320px, 90vw)" append-to-body>
      <button class="new-chat-btn" :disabled="loading || savingModel" @click="createNewSession">＋ 新聊天</button>
      <ul class="session-list-ul mobile-session-list">
        <li v-for="session in sortedSessions" :key="session.id">
          <button :class="['session-item', { active: currentSessionId === session.id }]"
            :disabled="loading || savingModel" @click="switchSession(session.id)">
            {{ session.title || `会话 ${session.id}` }}
          </button>
        </li>
      </ul>
    </el-drawer>

    <!-- 右侧聊天区域 -->
    <div class="chat-section">
      <div class="top-bar">
        <button class="back-btn" @click="$router.push('/menu')">← 返回</button>
        <button class="back-btn mobile-control" @click="sessionsOpened = true">会话列表</button>
        <button class="back-btn mobile-control options-toggle" :aria-expanded="optionsOpened" aria-controls="chat-options"
          @click="optionsOpened = !optionsOpened">{{ optionsOpened ? '收起设置' : '聊天设置' }}</button>
        <div id="chat-options" :class="['chat-options', { 'is-open': optionsOpened }]">
          <button class="back-btn" @click="$router.push('/memory')">个人档案</button>
          <button class="sync-btn" @click="syncHistory" :disabled="!currentSessionId || tempSession">同步历史数据</button>
          <label for="modelType">选择模型：</label>
          <select id="modelType" v-model="selectedModel" class="model-select" @change="saveSessionModel" :disabled="loading || savingModel">
            <!-- 暂时隐藏 ollama 本地部署，后端能力保留。 -->
            <!-- <option value="ollama">Ollama</option> -->
            <option value="qwen">AI对话</option>
            <option value="rag">RAG</option>
            <!-- 暂时隐藏 MCP 选项，后端能力保留。 -->
            <!-- <option value="mcp">阿里百炼 MCP</option> -->
            <option value="medical_agent">医疗 Agent</option>
          </select>
          <label for="streamingMode" class="streaming-option">
            <input type="checkbox" id="streamingMode" v-model="isStreaming" />
            流式响应
          </label>
        </div>
      </div>

      <p v-if="selectedModel === 'rag'" style="padding: 0 16px; color: var(--app-text-muted);">检索个人与医学知识库</p>

      <MedicalPanel v-if="selectedModel === 'medical_agent'" :session-id="currentSessionId" :events="medicalEvents" :busy="loading" :question="medicalQuestion" :request-status="medicalRequestStatus"
        @report="medicalReportId = $event" @ready="medicalReportReady = $event" @example="inputMessage = $event" />
      <button v-if="loading && selectedModel === 'medical_agent'" class="cancel-medical" @click="cancelMedical">停止本次回答</button>
      <div class="chat-messages" ref="messagesRef">
        <div v-for="(message, index) in currentMessages" :key="index"
          :class="['message', message.role === 'user' ? 'user-message' : 'ai-message']">
          <div class="message-header">
            <b>{{ message.role === 'user' ? '你' : 'AI' }}:</b>
            <button v-if="message.role === 'assistant'" class="tts-btn" @click="playTTS(message.content)">🔊</button>
            <span v-if="message.meta && message.meta.status === 'streaming'" class="streaming-indicator"> ··</span>
          </div>
          <p v-if="message === pendingMedicalMessage && loading && medicalRequestStatus === 'running' && message.meta?.status === 'streaming' && !message.content"
            class="agent-waiting-hint" role="status">
            Agent 正在处理，分析和工具调用可能需要一些时间，请耐心等待。
          </p>
          <div v-else class="message-content" v-html="renderMarkdown(message.content)"></div>
        </div>
      </div>

      <div class="chat-input">
        <textarea v-model="inputMessage" placeholder="请输入你的问题..." @keydown.enter.exact.prevent="sendMessage"
          :disabled="loading" ref="messageInput" rows="1"></textarea>
        <button
          type="button"
          :class="['voice-btn', { recording: isRecording }]"
          :disabled="loading || isTranscribing"
          @click="toggleVoiceInput"
          :title="voiceButtonTitle"
        >
          {{ voiceButtonText }}
        </button>
        <button type="button" :disabled="!inputMessage.trim() || loading" @click="sendMessage" class="send-btn">
          {{ loading ? '发送中...' : '发送' }}
        </button>
      </div>
    </div>
  </div>
</template>

<script>


import { ref, nextTick, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { renderMarkdown } from '../utils/markdown'
import api, { getApiErrorMessage } from '../utils/api'
import MedicalPanel from '../components/MedicalPanel.vue'

export default {
  name: 'AIChat',
  components: { MedicalPanel },
  setup() {

    const sessions = ref({})
    const currentSessionId = ref(null)
    const tempSession = ref(false)
    const currentMessages = ref([])
    const inputMessage = ref('')
    const loading = ref(false)
    const messagesRef = ref(null)
    const messageInput = ref(null)
    const sessionsOpened = ref(false)
    const optionsOpened = ref(false)
    const viewportStyle = ref({})
    // Mobile keyboards can resize only the visual viewport, leaving 100vh unchanged.
    const updateViewport = () => {
      const viewport = window.visualViewport
      if (viewport && viewport.scale !== 1) return
      const height = viewport?.height || window.innerHeight
      viewportStyle.value = {
        '--chat-viewport-height': `${height}px`,
        '--chat-viewport-top': `${viewport?.offsetTop || 0}px`,
        '--chat-panel-max-height': height < 500 ? '26%' : '38%'
      }
    }
    const selectedModel = ref('medical_agent')
    const savingModel = ref(false)
    const medicalEvents = ref([])
    const medicalQuestion = ref('')
    const medicalRequestStatus = ref('')
    const medicalReportId = ref('')
    const medicalReportReady = ref(true)
    const pendingMedicalMessage = ref(null)
    let medicalAbort = null
    const cancelMedical = () => medicalAbort?.abort()
    const isStreaming = ref(false)
    const isRecording = ref(false)
    const isTranscribing = ref(false)
    const voiceButtonText = computed(() => {
      if (isTranscribing.value) return '转写中'
      return isRecording.value ? '停止' : '🎙'
    })
    const voiceButtonTitle = computed(() => {
      if (isTranscribing.value) return '正在识别语音'
      return isRecording.value ? '停止录音' : '语音输入'
    })
    let audioContext = null
    let mediaStream = null
    let mediaSource = null
    let scriptProcessor = null
    let silentGain = null
    let audioChunks = []


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

    const cleanupRecorder = () => {
      if (scriptProcessor) {
        try {
          scriptProcessor.disconnect()
        } catch (e) {
          // ignore
        }
        scriptProcessor.onaudioprocess = null
        scriptProcessor = null
      }
      if (silentGain) {
        try {
          silentGain.disconnect()
        } catch (e) {
          // ignore
        }
        silentGain = null
      }
      if (mediaSource) {
        try {
          mediaSource.disconnect()
        } catch (e) {
          // ignore
        }
        mediaSource = null
      }
      if (mediaStream) {
        mediaStream.getTracks().forEach(track => track.stop())
        mediaStream = null
      }
      if (audioContext) {
        audioContext.close()
        audioContext = null
      }
    }

    const getUserAudioStream = async () => {
      const defaultAudioConstraints = {
        deviceId: { ideal: 'default' },
        channelCount: 1,
        sampleRate: 16000,
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true
      }
      const fallbackAudioConstraints = {
        channelCount: 1,
        sampleRate: 16000,
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true
      }
      const mediaDevices = navigator.mediaDevices
      if (mediaDevices && mediaDevices.getUserMedia) {
        try {
          return await mediaDevices.getUserMedia({ audio: defaultAudioConstraints })
        } catch (error) {
          if (error.name !== 'OverconstrainedError' && error.name !== 'ConstraintNotSatisfiedError') {
            throw error
          }
          return mediaDevices.getUserMedia({ audio: fallbackAudioConstraints })
        }
      }

      const legacyGetUserMedia = navigator.getUserMedia || navigator.webkitGetUserMedia || navigator.mozGetUserMedia || navigator.msGetUserMedia
      if (!legacyGetUserMedia) {
        return Promise.reject(new Error('getUserMedia is not available'))
      }

      return new Promise((resolve, reject) => {
        legacyGetUserMedia.call(navigator, { audio: fallbackAudioConstraints }, resolve, reject)
      })
    }

    const startVoiceInput = async () => {
      if (loading.value || isTranscribing.value) return
      if (!window.isSecureContext) {
        ElMessage.warning('麦克风需要 HTTPS 或 localhost，请使用 http://localhost:8081 访问')
        return
      }

      const AudioContextClass = window.AudioContext || window.webkitAudioContext
      if (!AudioContextClass) {
        ElMessage.warning('当前浏览器不支持音频录制')
        return
      }

      try {
        mediaStream = await getUserAudioStream()
        audioContext = new AudioContextClass()
        mediaSource = audioContext.createMediaStreamSource(mediaStream)
        scriptProcessor = audioContext.createScriptProcessor(4096, 1, 1)
        silentGain = audioContext.createGain()
        silentGain.gain.value = 0
        audioChunks = []

        scriptProcessor.onaudioprocess = event => {
          if (!isRecording.value) return
          const input = event.inputBuffer.getChannelData(0)
          audioChunks.push(new Float32Array(input))
        }

        mediaSource.connect(scriptProcessor)
        scriptProcessor.connect(silentGain)
        silentGain.connect(audioContext.destination)
        isRecording.value = true
      } catch (error) {
        console.error('Start recording error:', error)
        cleanupRecorder()
        ElMessage.error('无法访问麦克风')
      }
    }

    const stopVoiceInput = async () => {
      if (!isRecording.value) return
      isRecording.value = false

      const sourceSampleRate = audioContext ? audioContext.sampleRate : 44100
      const chunks = audioChunks
      cleanupRecorder()

      if (!chunks.length) {
        ElMessage.warning('未检测到录音内容')
        return
      }

      try {
        isTranscribing.value = true
        const wavBlob = encodeWav(chunks, sourceSampleRate, 16000)
        const formData = new FormData()
        formData.append('audio', wavBlob, 'voice-input.wav')

        const response = await api.post('/AI/chat/asr', formData, {
          headers: { 'Content-Type': 'multipart/form-data' }
        })

        if (response.data && response.data.status_code === 1000 && response.data.text) {
          inputMessage.value = appendRecognizedText(inputMessage.value, response.data.text)
          await nextTick()
          if (messageInput.value) messageInput.value.focus()
        } else {
          ElMessage.error(response.data?.status_msg || '语音识别失败')
        }
      } catch (error) {
        console.error('ASR error:', error)
        ElMessage.error(`请求语音识别接口失败：${getApiErrorMessage(error, '网络错误')}`)
      } finally {
        isTranscribing.value = false
      }
    }

    const toggleVoiceInput = () => {
      if (isRecording.value) {
        stopVoiceInput()
      } else {
        startVoiceInput()
      }
    }

    const appendRecognizedText = (currentText, recognizedText) => {
      const text = String(recognizedText || '').trim()
      if (!text) return currentText
      if (!currentText || !currentText.trim()) return text
      return `${currentText.trimEnd()} ${text}`
    }

    const mergeAudioChunks = chunks => {
      const length = chunks.reduce((total, chunk) => total + chunk.length, 0)
      const result = new Float32Array(length)
      let offset = 0
      chunks.forEach(chunk => {
        result.set(chunk, offset)
        offset += chunk.length
      })
      return result
    }

    const downsampleBuffer = (buffer, sourceRate, targetRate) => {
      if (targetRate === sourceRate) return buffer
      const ratio = sourceRate / targetRate
      const newLength = Math.round(buffer.length / ratio)
      const result = new Float32Array(newLength)
      let offsetResult = 0
      let offsetBuffer = 0

      while (offsetResult < result.length) {
        const nextOffsetBuffer = Math.round((offsetResult + 1) * ratio)
        let accum = 0
        let count = 0
        for (let i = offsetBuffer; i < nextOffsetBuffer && i < buffer.length; i++) {
          accum += buffer[i]
          count++
        }
        result[offsetResult] = count ? accum / count : 0
        offsetResult++
        offsetBuffer = nextOffsetBuffer
      }

      return result
    }

    const encodeWav = (chunks, sourceRate, targetRate) => {
      const samples = downsampleBuffer(mergeAudioChunks(chunks), sourceRate, targetRate)
      const bytesPerSample = 2
      const dataLength = samples.length * bytesPerSample
      const buffer = new ArrayBuffer(44 + dataLength)
      const view = new DataView(buffer)

      writeString(view, 0, 'RIFF')
      view.setUint32(4, 36 + dataLength, true)
      writeString(view, 8, 'WAVE')
      writeString(view, 12, 'fmt ')
      view.setUint32(16, 16, true)
      view.setUint16(20, 1, true)
      view.setUint16(22, 1, true)
      view.setUint32(24, targetRate, true)
      view.setUint32(28, targetRate * bytesPerSample, true)
      view.setUint16(32, bytesPerSample, true)
      view.setUint16(34, 8 * bytesPerSample, true)
      writeString(view, 36, 'data')
      view.setUint32(40, dataLength, true)

      let offset = 44
      samples.forEach(sample => {
        const normalized = Math.max(-1, Math.min(1, sample))
        view.setInt16(offset, normalized < 0 ? normalized * 0x8000 : normalized * 0x7FFF, true)
        offset += bytesPerSample
      })

      return new Blob([view], { type: 'audio/wav' })
    }

    const writeString = (view, offset, string) => {
      for (let i = 0; i < string.length; i++) {
        view.setUint8(offset + i, string.charCodeAt(i))
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
              modelType: s.modelType || 'medical_agent',
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

    const saveSessionModel = async () => {
      if (!currentSessionId.value || tempSession.value) return
      const session = sessions.value[currentSessionId.value]
      savingModel.value = true
      try {
        const { data } = await api.post('/AI/chat/model', {
          sessionId: currentSessionId.value, modelType: selectedModel.value
        })
        if (data.status_code !== 1000) throw new Error(data.message || '保存失败')
        session.modelType = selectedModel.value
      } catch (error) {
        selectedModel.value = session.modelType || 'medical_agent'
        ElMessage.error('模型选择保存失败，请重试')
      } finally {
        savingModel.value = false
      }
    }

    const createNewSession = () => {
      if (loading.value || savingModel.value) return
      sessionsOpened.value = false
      medicalEvents.value = []
      medicalQuestion.value = ''; medicalRequestStatus.value = ''
      currentSessionId.value = 'temp'
      tempSession.value = true
      currentMessages.value = []
      // focus input
      nextTick(() => {
        if (messageInput.value) messageInput.value.focus()
      })
    }

    const switchSession = async (sessionId) => {
      if (!sessionId || loading.value || savingModel.value) return
      sessionsOpened.value = false
      medicalEvents.value = []
      medicalQuestion.value = ''; medicalRequestStatus.value = ''
      currentSessionId.value = String(sessionId)
      tempSession.value = false
      selectedModel.value = sessions.value[sessionId].modelType || 'medical_agent'

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
      if (loading.value || savingModel.value) return
      if (selectedModel.value === 'medical_agent' && !medicalReportReady.value) { ElMessage.warning('请先确认报告识别结果'); return }
      if (isRecording.value || isTranscribing.value) {
        ElMessage.warning('请先完成语音输入')
        return
      }
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
        if (selectedModel.value === 'medical_agent') {
          await handleMedical(currentInput)
        } else if (isStreaming.value) {
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
        if (!isStreaming.value || selectedModel.value === 'medical_agent') {
          loading.value = false
        }
        await nextTick()
        scrollToBottom()
      }
    }


    async function handleMedical(question) {
      const assistant = { role: 'assistant', content: '', meta: { status: 'streaming' } }
      pendingMedicalMessage.value = assistant
      currentMessages.value.push(assistant)
      medicalEvents.value = []
      medicalQuestion.value = question; medicalRequestStatus.value = 'running'
      medicalAbort = new AbortController()
      const body = { question, modelType: 'medical_agent', reportId: medicalReportId.value }
      const isNew = tempSession.value || !currentSessionId.value
      if (!isNew) body.sessionId = currentSessionId.value
      const bindSession = id => {
        if (!id) return
        currentSessionId.value = id; tempSession.value = false
        if (!sessions.value[id]) sessions.value[id] = { id, modelType: 'medical_agent', title: question.slice(0, 30), messages: [], updated_at: new Date() }
      }
      const accept = event => {
        if (event.sessionId) bindSession(event.sessionId)
        medicalEvents.value.push(event)
        if (event.type === 'answer_delta') assistant.content += event.delta || ''
        if (event.type === 'answer_final') assistant.content = event.delta || ''
        if (event.type === 'error') throw new Error(event.summary || '运行失败')
        if (event.type === 'run_end') assistant.meta.status = event.status
        currentMessages.value = [...currentMessages.value]
      }
      try {
        await nextTick()
        scrollToBottom()
        if (!isStreaming.value) {
          const { data } = await api.post(isNew ? '/AI/chat/send-new-session' : '/AI/chat/send', body, { signal: medicalAbort.signal })
          if (data.status_code !== 1000) throw new Error(data.status_msg || '发送失败')
          bindSession(data.sessionId)
          assistant.content = data.Information || ''
          assistant.meta.status = data.run?.status || 'completed'
          medicalRequestStatus.value = assistant.meta.status
          medicalEvents.value = JSON.parse(data.run?.eventsJson || '[]')
        } else {
          const response = await fetch(isNew ? '/api/AI/chat/send-stream-new-session' : '/api/AI/chat/send-stream', {
            method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${localStorage.getItem('token') || ''}` }, body: JSON.stringify(body), signal: medicalAbort.signal
          })
          if (!response.ok) { const data = await response.json(); throw new Error(data.status_msg || '请求失败') }
          const reader = response.body.getReader(), decoder = new TextDecoder()
          let buffer = '', completed = false
          try {
            // eslint-disable-next-line no-constant-condition
            while (true) {
              const { value, done } = await reader.read()
              buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
              const frames = buffer.split(/\r?\n\r?\n/); buffer = frames.pop() || ''
              for (const frame of frames) {
                const data = frame.split(/\r?\n/).filter(line => line.startsWith('data:')).map(line => line.slice(5).trimStart()).join('\n')
                if (data === '[DONE]') { completed = true; continue }
                if (data) accept(JSON.parse(data))
              }
              if (done) break
            }
          } finally { reader.releaseLock() }
          if (!completed) throw new Error('连接已结束，运行状态可在历史记录中查看')
          medicalRequestStatus.value = medicalEvents.value.find(e => e.type === 'run_end')?.status || 'unknown'
        }
      } catch (e) {
        medicalRequestStatus.value = medicalAbort.signal.aborted ? 'stopped' : (medicalEvents.value.some(e => e.type === 'error') || e.response ? 'failed' : 'unknown')
        assistant.meta.status = 'failed'
        if (!assistant.content) assistant.content = medicalAbort.signal.aborted ? '已停止本次回答。' : (e.message || '运行失败')
        if (!medicalAbort.signal.aborted) ElMessage.error(getApiErrorMessage(e, '运行失败'))
      } finally {
        pendingMedicalMessage.value = null
        if (!tempSession.value && sessions.value[currentSessionId.value]) {
          sessions.value[currentSessionId.value].messages = [...currentMessages.value]
          sessions.value[currentSessionId.value].updated_at = new Date()
        }
        currentMessages.value = [...currentMessages.value]
        medicalAbort = null
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
                        modelType: selectedModel.value,
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
            modelType: selectedModel.value,
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

    onMounted(() => {
      updateViewport()
      window.addEventListener('resize', updateViewport)
      window.visualViewport?.addEventListener('resize', updateViewport)
      window.visualViewport?.addEventListener('scroll', updateViewport)
      loadSessions()
    })

    onUnmounted(() => {
      window.removeEventListener('resize', updateViewport)
      window.visualViewport?.removeEventListener('resize', updateViewport)
      window.visualViewport?.removeEventListener('scroll', updateViewport)
      cancelMedical()
      isRecording.value = false
      cleanupRecorder()
    })

    // expose to template
    return {
      sessionsOpened, optionsOpened, viewportStyle,
      pendingMedicalMessage,
      medicalEvents, medicalQuestion, medicalRequestStatus, medicalReportId, medicalReportReady, cancelMedical,
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
      savingModel,
      saveSessionModel,
      isStreaming,
      isRecording,
      isTranscribing,
      voiceButtonText,
      voiceButtonTitle,
      renderMarkdown,
      playTTS,
      toggleVoiceInput,
      createNewSession,
      switchSession,
      syncHistory,
      sendMessage
    }
  }
}
</script>

<style scoped>
.ai-chat-container {
  height: 100vh;
  height: 100dvh;
  display: flex;
  background: var(--app-bg);
  overflow: hidden;
  color: var(--app-text);
}

.session-list {
  width: 280px;
  flex-shrink: 0;
  height: 100%;
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
  overflow-wrap: anywhere;
}

.mobile-control { display: none; }
.mobile-session-list { margin-top: 12px; }
.mobile-session-list .session-item { width: 100%; text-align: left; border: 0; background: transparent; font: inherit; }
.mobile-session-list .session-item.active { background: var(--app-primary-soft); }
.chat-options { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.streaming-option { display: flex; align-items: center; gap: 6px; white-space: nowrap; }
.cancel-medical { flex-shrink: 0; padding: 6px; }

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
  flex-shrink: 0;
  position: relative;
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
.sync-btn {
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

.back-btn:hover {
  border-color: #bfdbfe;
  color: var(--app-primary);
  background: var(--app-primary-soft);
}

.sync-btn:hover:not(:disabled) {
  background: #15803d;
}

.sync-btn:disabled {
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
.agent-waiting-hint {
  margin: 8px 0;
  font-size: 13px;
  line-height: 1.6;
  color: var(--app-text-muted);
}

.message {
  flex-shrink: 0;
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
  white-space: normal;
  overflow-wrap: anywhere;
  min-width: 0;
}

.message-content :deep(h1),
.message-content :deep(h2),
.message-content :deep(h3),
.message-content :deep(h4),
.message-content :deep(h5),
.message-content :deep(h6) {
  margin: 1em 0 0.5em;
  font-weight: 650;
  line-height: 1.4;
}

.message-content :deep(h1) { font-size: 1.5em; }
.message-content :deep(h2) { font-size: 1.3em; }
.message-content :deep(h3) { font-size: 1.15em; }
.message-content :deep(h4),
.message-content :deep(h5),
.message-content :deep(h6) { font-size: 1em; }

.message-content :deep(p),
.message-content :deep(ul),
.message-content :deep(ol),
.message-content :deep(blockquote),
.message-content :deep(pre) { margin: 0.65em 0; }

.message-content :deep(ul),
.message-content :deep(ol) { padding-left: 1.6em; }
.message-content :deep(li) { margin: 0.25em 0; }
.message-content :deep(blockquote) {
  padding-left: 12px;
  border-left: 3px solid var(--app-border);
  color: var(--app-text-muted);
}

.message-content :deep(code) {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 0.9em;
  background: var(--app-surface-subtle);
  color: var(--app-text);
  border-radius: 4px;
  padding: 2px 4px;
}

.message-content :deep(pre) {
  max-width: 100%;
  overflow-x: auto;
  padding: 12px;
  background: var(--app-surface-subtle);
  border-radius: var(--app-radius);
  white-space: pre;
  overflow-wrap: normal;
}
.message-content :deep(pre code) { padding: 0; }
.message-content :deep(table) {
  display: block;
  max-width: 100%;
  overflow-x: auto;
  border-collapse: collapse;
  margin: 0.75em 0;
}
.message-content :deep(th),
.message-content :deep(td) {
  padding: 8px 12px;
  border: 1px solid var(--app-border);
  text-align: left;
}
.message-content :deep(a) { color: inherit; text-decoration: underline; }
.message-content :deep(img) { max-width: 100%; height: auto; }
.message-content :deep(hr) { border: 0; border-top: 1px solid var(--app-border); }
.message-content :deep(> :first-child) { margin-top: 0; }
.message-content :deep(> :last-child) { margin-bottom: 0; }

/* input area */
.chat-input {
  flex-shrink: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 10px;
  padding: 16px 20px;
  background: var(--app-surface);
  border-top: 1px solid var(--app-border-soft);
}

.chat-input textarea {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
  resize: none;
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  padding: 12px 14px;
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

.voice-btn {
  white-space: nowrap;
  min-width: 52px;
  height: 32px;
  padding: 0 10px;
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  background: var(--app-surface-subtle);
  color: var(--app-text);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.18s ease, border-color 0.18s ease, color 0.18s ease;
}

.voice-btn:hover:not(:disabled) {
  border-color: #bfdbfe;
  color: var(--app-primary);
}

.voice-btn.recording {
  background: #fee2e2;
  border-color: #fca5a5;
  color: #b91c1c;
}

.voice-btn:disabled {
  background: #e2e8f0;
  color: var(--app-text-muted);
  cursor: not-allowed;
}

.send-btn {
  white-space: nowrap;
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

@media (max-width: 820px), (max-height: 500px) and (pointer: coarse) {
  .ai-chat-container {
    position: fixed;
    top: var(--chat-viewport-top, 0px);
    left: 0;
    width: 100%;
    height: var(--chat-viewport-height, 100dvh);
    padding-left: env(safe-area-inset-left);
    padding-right: env(safe-area-inset-right);
  }

  .session-list { display: none; }
  .mobile-control { display: inline-block; }
  .options-toggle { margin-left: auto; }

  .top-bar {
    min-height: 52px;
    padding: 8px 10px;
    padding-top: max(8px, env(safe-area-inset-top));
    gap: 8px;
    flex-wrap: nowrap;
  }

  .chat-options {
    display: none;
    position: absolute;
    z-index: 10;
    top: 100%;
    left: 0;
    right: 0;
    padding: 12px;
    max-height: 60dvh;
    overflow-y: auto;
    background: var(--app-surface);
    border-bottom: 1px solid var(--app-border);
    box-shadow: var(--app-shadow);
  }
  .chat-options.is-open { display: flex; }
  .model-select { font-size: 16px; }
  .chat-messages { padding: 12px; gap: 12px; overscroll-behavior-y: contain; }
  .message { max-width: 94%; }
  .chat-input { padding: 8px 10px max(8px, env(safe-area-inset-bottom)); gap: 6px; }
  .chat-input textarea { padding: 10px; font-size: 16px; }
  .voice-btn { min-width: 40px; height: 44px; padding: 0 8px; }
  .send-btn { height: 44px; padding: 0 10px; }
}
</style>
