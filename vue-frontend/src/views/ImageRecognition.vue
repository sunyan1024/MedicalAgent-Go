<template>
  <div class="image-page">
    <div class="page-header">
      <button class="icon-btn" @click="$router.push('/menu')" title="返回">
        <el-icon><ArrowLeft /></el-icon>
      </button>
      <div>
        <h1>图像识别</h1>
        <p>{{ statusText }}</p>
      </div>
      <button class="icon-btn" @click="resetWorkspace" :disabled="recognizing || !hasWorkspaceData" title="重置">
        <el-icon><Refresh /></el-icon>
      </button>
    </div>

    <main class="workspace">
      <section class="upload-panel">
        <div class="panel-heading">
          <h2>上传图片</h2>
          <span>{{ selectedFile ? formatSize(selectedFile.size) : '支持常见图片格式' }}</span>
        </div>

        <el-upload
          class="image-upload"
          drag
          :auto-upload="false"
          :show-file-list="false"
          accept="image/*"
          :on-change="handleFileSelect"
          :disabled="recognizing"
        >
          <el-icon class="upload-icon"><Upload /></el-icon>
          <div class="upload-text">{{ selectedFile ? '拖拽或点击更换图片' : '拖拽或点击选择图片' }}</div>
        </el-upload>

        <div class="preview-frame" :class="{ empty: !previewUrl }">
          <img v-if="previewUrl" :src="previewUrl" alt="待识别图片" />
          <div v-else class="preview-empty">
            <el-icon><Picture /></el-icon>
            <span>图片预览</span>
          </div>
        </div>

        <div v-if="selectedFile" class="file-meta">
          <div>
            <span>文件名</span>
            <strong>{{ selectedFile.name }}</strong>
          </div>
          <div>
            <span>类型</span>
            <strong>{{ selectedFile.type || '未知' }}</strong>
          </div>
        </div>

        <div class="actions">
          <el-button :disabled="recognizing || !selectedFile" @click="clearSelection">清除</el-button>
          <el-button type="primary" :icon="Search" :loading="recognizing" :disabled="!selectedFile" @click="recognizeImage">
            开始识别
          </el-button>
        </div>
      </section>

      <section class="result-panel">
        <div class="panel-heading">
          <h2>识别结果</h2>
          <span>{{ history.length }} 次记录</span>
        </div>

        <div class="result-card" :class="resultState">
          <template v-if="recognizing">
            <el-icon class="state-icon loading"><Search /></el-icon>
            <h3>正在识别</h3>
            <p>模型正在分析图片内容</p>
          </template>
          <template v-else-if="latestResult">
            <el-icon class="state-icon success"><CircleCheck /></el-icon>
            <span class="result-label">分类结果</span>
            <h3>{{ latestResult.className }}</h3>
            <p>{{ latestResult.fileName }}</p>
          </template>
          <template v-else-if="errorMessage">
            <el-icon class="state-icon error"><Warning /></el-icon>
            <h3>识别失败</h3>
            <p>{{ errorMessage }}</p>
          </template>
          <template v-else>
            <el-icon class="state-icon idle"><Picture /></el-icon>
            <h3>等待图片</h3>
            <p>上传图片后开始识别</p>
          </template>
        </div>

        <div class="history-section">
          <div class="history-header">
            <h3>本次历史</h3>
            <button class="text-btn" :disabled="!history.length || recognizing" @click="clearHistory">清空</button>
          </div>

          <ul v-if="history.length" class="history-list">
            <li v-for="item in history" :key="item.id">
              <div class="history-thumb">
                <img :src="item.imageUrl" :alt="item.fileName" />
              </div>
              <div class="history-content">
                <strong>{{ item.className }}</strong>
                <span>{{ item.fileName }}</span>
              </div>
              <time>{{ item.time }}</time>
            </li>
          </ul>
          <div v-else class="history-empty">暂无识别记录</div>
        </div>
      </section>
    </main>
  </div>
</template>

<script>
import { computed, onBeforeUnmount, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowLeft, CircleCheck, Picture, Refresh, Search, Upload, Warning } from '@element-plus/icons-vue'
import api, { getApiErrorMessage } from '../utils/api'

export default {
  name: 'ImageRecognition',
  components: {
    ArrowLeft,
    CircleCheck,
    Picture,
    Refresh,
    Upload,
    Warning
  },
  setup() {
    const selectedFile = ref(null)
    const previewUrl = ref('')
    const recognizing = ref(false)
    const latestResult = ref(null)
    const errorMessage = ref('')
    const history = ref([])

    const hasWorkspaceData = computed(() => {
      return Boolean(selectedFile.value || previewUrl.value || latestResult.value || errorMessage.value || history.value.length)
    })

    const statusText = computed(() => {
      if (recognizing.value) return '识别中'
      if (latestResult.value) return `最近结果: ${latestResult.value.className}`
      if (selectedFile.value) return '图片已就绪'
      return '上传图片进行 AI 识别'
    })

    const resultState = computed(() => {
      if (recognizing.value) return 'processing'
      if (latestResult.value) return 'success'
      if (errorMessage.value) return 'error'
      return 'idle'
    })

    const revokePreview = () => {
      if (previewUrl.value) {
        URL.revokeObjectURL(previewUrl.value)
        previewUrl.value = ''
      }
    }

    const handleFileSelect = (uploadFile) => {
      const rawFile = uploadFile.raw
      if (!rawFile) return
      if (!rawFile.type || !rawFile.type.startsWith('image/')) {
        ElMessage.error('请选择图片文件')
        return
      }

      revokePreview()
      selectedFile.value = rawFile
      previewUrl.value = URL.createObjectURL(rawFile)
      latestResult.value = null
      errorMessage.value = ''
    }

    const recognizeImage = async () => {
      if (!selectedFile.value) return

      recognizing.value = true
      errorMessage.value = ''
      latestResult.value = null

      try {
        const formData = new FormData()
        formData.append('image', selectedFile.value)

        const response = await api.post('/image/recognize', formData, {
          headers: {
            'Content-Type': 'multipart/form-data'
          }
        })

        if (response.data && response.data.status_code === 1000 && response.data.class_name) {
          const imageUrl = URL.createObjectURL(selectedFile.value)
          const result = {
            id: `${Date.now()}-${selectedFile.value.name}`,
            className: response.data.class_name,
            fileName: selectedFile.value.name,
            imageUrl,
            time: new Date().toLocaleTimeString()
          }
          latestResult.value = result
          history.value.unshift(result)
          ElMessage.success('识别完成')
        } else {
          errorMessage.value = response.data?.status_msg || '识别失败'
        }
      } catch (error) {
        console.error('Recognize image error:', error)
        errorMessage.value = getApiErrorMessage(error, '图像识别失败')
      } finally {
        recognizing.value = false
      }
    }

    const clearSelection = () => {
      selectedFile.value = null
      latestResult.value = null
      errorMessage.value = ''
      revokePreview()
    }

    const clearHistory = () => {
      history.value.forEach(item => URL.revokeObjectURL(item.imageUrl))
      history.value = []
    }

    const resetWorkspace = () => {
      clearSelection()
      clearHistory()
    }

    const formatSize = (size) => {
      if (!size) return '0 B'
      const units = ['B', 'KB', 'MB', 'GB']
      let value = Number(size)
      let unitIndex = 0
      while (value >= 1024 && unitIndex < units.length - 1) {
        value = value / 1024
        unitIndex++
      }
      return `${value.toFixed(unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`
    }

    onBeforeUnmount(() => {
      revokePreview()
      clearHistory()
    })

    return {
      Search,
      selectedFile,
      previewUrl,
      recognizing,
      latestResult,
      errorMessage,
      history,
      hasWorkspaceData,
      statusText,
      resultState,
      handleFileSelect,
      recognizeImage,
      clearSelection,
      clearHistory,
      resetWorkspace,
      formatSize
    }
  }
}
</script>

<style scoped>
.image-page {
  min-height: 100vh;
  padding: 24px;
  background: var(--app-bg);
  color: var(--app-text);
}

.page-header {
  display: grid;
  grid-template-columns: 44px 1fr 44px;
  align-items: center;
  gap: 16px;
  max-width: 1180px;
  margin: 0 auto 20px;
}

.page-header h1 {
  margin: 0;
  font-size: 28px;
  font-weight: 700;
}

.page-header p {
  margin: 4px 0 0;
  color: var(--app-text-muted);
  font-size: 14px;
}

.icon-btn {
  width: 44px;
  height: 44px;
  border: 1px solid var(--app-border);
  border-radius: var(--app-radius);
  background: var(--app-surface);
  color: var(--app-text);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: border-color 0.18s ease, color 0.18s ease, background 0.18s ease;
}

.icon-btn:hover:not(:disabled) {
  border-color: #bfdbfe;
  color: var(--app-primary);
  background: var(--app-primary-soft);
}

.icon-btn:disabled {
  color: #cbd5e1;
  cursor: not-allowed;
}

.workspace {
  max-width: 1180px;
  margin: 0 auto;
  display: grid;
  grid-template-columns: minmax(360px, 0.95fr) minmax(420px, 1.05fr);
  gap: 20px;
  align-items: start;
}

.upload-panel,
.result-panel {
  background: var(--app-surface);
  border: 1px solid var(--app-border-soft);
  border-radius: var(--app-radius);
  padding: 20px;
  box-shadow: var(--app-shadow);
}

.panel-heading {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 16px;
}

.panel-heading h2 {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
}

.panel-heading span {
  color: var(--app-text-muted);
  font-size: 13px;
  line-height: 1.6;
  text-align: right;
}

.image-upload {
  margin-bottom: 16px;
}

.image-upload :deep(.el-upload) {
  width: 100%;
}

.image-upload :deep(.el-upload-dragger) {
  width: 100%;
  border-color: var(--app-border);
  border-radius: var(--app-radius);
  background: var(--app-surface-subtle);
  padding: 26px 16px;
}

.image-upload :deep(.el-upload-dragger:hover) {
  border-color: #93c5fd;
  background: var(--app-primary-soft);
}

.upload-icon {
  color: var(--app-primary);
  font-size: 34px;
  margin-bottom: 8px;
}

.upload-text {
  color: var(--app-text);
  font-size: 14px;
  font-weight: 600;
}

.preview-frame {
  width: 100%;
  aspect-ratio: 4 / 3;
  border: 1px solid var(--app-border-soft);
  border-radius: var(--app-radius);
  background: #0f172a;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

.preview-frame.empty {
  background: var(--app-surface-subtle);
}

.preview-frame img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.preview-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  color: var(--app-text-muted);
  font-size: 14px;
}

.preview-empty .el-icon {
  font-size: 34px;
}

.file-meta {
  margin-top: 14px;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.file-meta div {
  min-width: 0;
  border: 1px solid var(--app-border-soft);
  border-radius: var(--app-radius);
  padding: 10px 12px;
  background: var(--app-surface-subtle);
}

.file-meta span,
.file-meta strong {
  display: block;
}

.file-meta span {
  color: var(--app-text-muted);
  font-size: 12px;
  margin-bottom: 4px;
}

.file-meta strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  font-weight: 600;
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 16px;
}

.result-card {
  min-height: 220px;
  border: 1px solid var(--app-border-soft);
  border-radius: var(--app-radius);
  background: var(--app-surface-subtle);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 28px;
}

.result-card.success {
  background: #f0fdf4;
  border-color: #bbf7d0;
}

.result-card.error {
  background: #fef2f2;
  border-color: #fecaca;
}

.result-card.processing {
  background: var(--app-primary-soft);
  border-color: #bfdbfe;
}

.state-icon {
  font-size: 40px;
  margin-bottom: 12px;
}

.state-icon.success {
  color: var(--app-success);
}

.state-icon.error {
  color: var(--app-danger);
}

.state-icon.loading,
.state-icon.idle {
  color: var(--app-primary);
}

.state-icon.loading {
  animation: pulse 1.2s ease-in-out infinite;
}

.result-label {
  color: var(--app-success);
  font-size: 13px;
  font-weight: 700;
  margin-bottom: 6px;
}

.result-card h3 {
  max-width: 100%;
  margin: 0;
  font-size: 26px;
  line-height: 1.25;
  font-weight: 800;
  word-break: break-word;
}

.result-card p {
  max-width: 100%;
  margin: 8px 0 0;
  color: var(--app-text-muted);
  font-size: 14px;
  line-height: 1.6;
  word-break: break-word;
}

.history-section {
  margin-top: 20px;
}

.history-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.history-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
}

.text-btn {
  border: none;
  background: transparent;
  color: var(--app-primary);
  font-weight: 600;
  cursor: pointer;
  padding: 6px 0;
}

.text-btn:disabled {
  color: #cbd5e1;
  cursor: not-allowed;
}

.history-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.history-list li {
  display: grid;
  grid-template-columns: 58px minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  padding: 10px;
  border: 1px solid var(--app-border-soft);
  border-radius: var(--app-radius);
}

.history-thumb {
  width: 58px;
  height: 44px;
  border-radius: 6px;
  overflow: hidden;
  background: #0f172a;
}

.history-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.history-content {
  min-width: 0;
}

.history-content strong,
.history-content span {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.history-content strong {
  font-size: 14px;
  font-weight: 700;
}

.history-content span,
.history-list time {
  color: var(--app-text-muted);
  font-size: 12px;
}

.history-empty {
  border: 1px dashed var(--app-border);
  border-radius: var(--app-radius);
  padding: 26px 16px;
  color: var(--app-text-muted);
  text-align: center;
  background: var(--app-surface-subtle);
}

@keyframes pulse {
  0%,
  100% {
    opacity: 0.55;
    transform: scale(0.98);
  }

  50% {
    opacity: 1;
    transform: scale(1);
  }
}

@media (max-width: 920px) {
  .workspace {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .image-page {
    padding: 16px;
  }

  .page-header {
    gap: 12px;
    margin-bottom: 16px;
  }

  .page-header h1 {
    font-size: 22px;
  }

  .upload-panel,
  .result-panel {
    padding: 16px;
  }

  .panel-heading,
  .actions {
    flex-direction: column;
    align-items: stretch;
  }

  .panel-heading span {
    text-align: left;
  }

  .file-meta,
  .history-list li {
    grid-template-columns: 1fr;
  }

  .history-list time {
    justify-self: start;
  }
}
</style>
