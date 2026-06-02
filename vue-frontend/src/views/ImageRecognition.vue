<template>
  <div class="image-recognition-container">
    <!-- 左侧会话列表 -->
    <div class="session-list">
      <div class="session-list-header">
        <span>图像识别</span>
      </div>
      <ul class="session-list-ul">
        <li class="session-item active">
          图像识别助手
        </li>
      </ul>
    </div>

    <!-- 右侧聊天区域 -->
    <div class="chat-section">
      <div class="top-bar">
        <button class="back-btn" @click="$router.push('/menu')">← 返回</button>
        <h2>AI 图像识别助手</h2>
      </div>

      <div class="chat-messages" ref="chatContainerRef">
        <div
          v-for="(message, index) in messages"
          :key="index"
          :class="['message', message.role === 'user' ? 'user-message' : 'ai-message']"
        >
          <div class="message-header">
            <b>{{ message.role === 'user' ? '你' : 'AI' }}:</b>
          </div>
          <div class="message-content">
            <span>{{ message.content }}</span>
            <img v-if="message.imageUrl" :src="message.imageUrl" alt="上传的图片" />
          </div>
        </div>
      </div>

      <div class="chat-input">
        <form @submit.prevent="handleSubmit">
          <input
            ref="fileInputRef"
            type="file"
            accept="image/*"
            required
            @change="handleFileSelect"
          />
          <button type="submit" :disabled="!selectedFile">发送图片</button>
        </form>
      </div>
    </div>
  </div>
</template>

<script>
import { ref, nextTick } from 'vue'
import api from '../utils/api'

export default {
  name: 'ImageRecognition',
  setup() {
    const messages = ref([])
    const selectedFile = ref(null)
    const fileInputRef = ref()
    const chatContainerRef = ref()

    const handleFileSelect = (event) => {
      selectedFile.value = event.target.files[0]
    }

    const handleSubmit = async () => {
      if (!selectedFile.value) return

      const file = selectedFile.value
      const imageUrl = URL.createObjectURL(file)

      // Add user message to UI
      messages.value.push({
        role: 'user',
        content: `已上传图片: ${file.name}`,
        imageUrl: imageUrl,
      })

      await nextTick()
      scrollToBottom()

      // Create FormData
      const formData = new FormData()
      formData.append('image', file)

      try {
        const response = await api.post('/image/recognize', formData, {
          headers: {
            'Content-Type': 'multipart/form-data',
          },
        })


        if (response.data && response.data.class_name) {
             const aiText = `识别结果: ${response.data.class_name}`
            messages.value.push({
                role: 'assistant',
                content: aiText,
            })
        } else {
             messages.value.push({
                 role: 'assistant',
                 content: `[错误] ${response.data.status_msg || '识别失败'}`,
             })
        }
      } catch (error) {
        console.error('Upload error:', error)
        messages.value.push({
          role: 'assistant',
          content: `[错误] 无法连接到服务器或上传失败: ${error.message}`,
        })
      } finally {

        URL.revokeObjectURL(imageUrl)

            await nextTick()
        scrollToBottom()


        selectedFile.value = null
        if (fileInputRef.value) {
          fileInputRef.value.value = ''
        }
      }
    }

    const scrollToBottom = () => {
      if (document.hidden) return
      if (chatContainerRef.value) {
        chatContainerRef.value.scrollTop = chatContainerRef.value.scrollHeight
      }
    }

    return {
      messages,
      selectedFile,
      fileInputRef,
      chatContainerRef,
      handleFileSelect,
      handleSubmit
    }
  }
}
</script>

<style scoped>
.image-recognition-container {
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
  color: var(--app-text);
  font-size: 14px;
}

.session-item.active {
  background: var(--app-primary-soft);
  color: var(--app-primary);
  font-weight: 600;
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
}

.back-btn {
  height: 34px;
  background: var(--app-surface);
  border: 1px solid var(--app-border);
  color: var(--app-text);
  padding: 0 12px;
  border-radius: var(--app-radius);
  cursor: pointer;
  font-weight: 600;
  transition: background 0.18s ease, border-color 0.18s ease, color 0.18s ease;
}

.back-btn:hover {
  border-color: #bfdbfe;
  color: var(--app-primary);
  background: var(--app-primary-soft);
}

.top-bar h2 {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
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

/* message content */
.message-content {
  white-space: pre-wrap;
  word-break: break-word;
}

.message-content img {
  max-width: 250px;
  border-radius: var(--app-radius);
  display: block;
  margin-top: 12px;
  border: 1px solid var(--app-border-soft);
}

.chat-input {
  padding: 16px 20px;
  background: var(--app-surface);
  border-top: 1px solid var(--app-border-soft);
}

.chat-input form {
  display: flex;
  gap: 12px;
}

.chat-input input[type="file"] {
  flex: 1;
  border: 1px dashed var(--app-border);
  border-radius: var(--app-radius);
  padding: 10px 12px;
  background: var(--app-surface-subtle);
  color: var(--app-text-muted);
  cursor: pointer;
  transition: border-color 0.18s ease, background 0.18s ease;
  font-size: 14px;
}

.chat-input input[type="file"]:hover {
  border-color: #93c5fd;
  background: var(--app-primary-soft);
}

.chat-input input[type="file"]::file-selector-button {
  border: none;
  background: var(--app-primary);
  padding: 7px 12px;
  border-radius: var(--app-radius);
  color: white;
  cursor: pointer;
  font-weight: 600;
  margin-right: 12px;
}

.chat-input button {
  height: 42px;
  padding: 0 18px;
  border: none;
  border-radius: var(--app-radius);
  background: var(--app-primary);
  color: white;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.18s ease;
}

.chat-input button:hover {
  background: var(--app-primary-hover);
}

.chat-input button:disabled {
  background: #cbd5e1;
  cursor: not-allowed;
}

@media (max-width: 820px) {
  .image-recognition-container {
    flex-direction: column;
  }

  .session-list {
    width: 100%;
    height: auto;
    border-right: none;
    border-bottom: 1px solid var(--app-border-soft);
  }

  .message {
    max-width: 88%;
  }

  .chat-input form {
    flex-direction: column;
  }
}
</style>
