<template>
  <div class="knowledge-page">
    <div class="page-header">
      <button class="icon-btn" @click="$router.push('/menu')" title="返回">
        <el-icon><ArrowLeft /></el-icon>
      </button>
      <div>
        <h1>知识库</h1>
        <p>{{ files.length }} 个文档</p>
      </div>
      <button class="icon-btn" @click="loadFiles" :disabled="loading" title="刷新">
        <el-icon><Refresh /></el-icon>
      </button>
    </div>

    <div class="toolbar">
      <el-upload
        class="upload"
        drag
        :auto-upload="false"
        :show-file-list="false"
        accept=".md,.txt,.pdf,text/markdown,text/plain,application/pdf"
        :on-change="handleUpload"
        :disabled="uploading"
      >
        <el-icon class="upload-icon"><Upload /></el-icon>
        <div class="upload-text">{{ uploading ? '上传解析中...' : '拖拽或点击上传文档' }}</div>
      </el-upload>
    </div>

    <el-table v-loading="loading" :data="files" class="file-table" empty-text="暂无知识库文件">
      <el-table-column prop="original_name" label="文件名" min-width="260">
        <template #default="{ row }">
          <div class="file-name">
            <el-icon><Document /></el-icon>
            <span>{{ row.original_name }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="大小" width="120">
        <template #default="{ row }">{{ formatSize(row.size) }}</template>
      </el-table-column>
      <el-table-column label="上传时间" width="190">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="100" align="right">
        <template #default="{ row }">
          <el-button type="danger" :icon="Delete" circle plain @click="deleteFile(row)" />
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script>
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Delete, Document, Refresh, Upload } from '@element-plus/icons-vue'
import api, { getApiErrorMessage } from '../utils/api'
import { calculateFileSHA256 } from '../utils/hash'

export default {
  name: 'KnowledgeBase',
  components: {
    ArrowLeft,
    Document,
    Refresh,
    Upload
  },
  setup() {
    const files = ref([])
    const loading = ref(false)
    const uploading = ref(false)

    const loadFiles = async () => {
      loading.value = true
      try {
        const response = await api.get('/file/list')
        if (response.data && response.data.status_code === 1000) {
          files.value = response.data.files || []
        } else {
          ElMessage.error(response.data?.status_msg || '获取文件列表失败')
        }
      } catch (error) {
        console.error('Load knowledge files error:', error)
        ElMessage.error('获取文件列表失败')
      } finally {
        loading.value = false
      }
    }

    const validateFile = (rawFile) => {
      const name = rawFile.name.toLowerCase()
      if (!name.endsWith('.md') && !name.endsWith('.txt') && !name.endsWith('.pdf')) {
        ElMessage.error('只允许上传 .md、.txt 或 .pdf 文件')
        return false
      }
      return true
    }

    const handleUpload = async (uploadFile) => {
      const rawFile = uploadFile.raw
      if (!rawFile || !validateFile(rawFile)) return

      uploading.value = true
      try {
        const fileSHA256 = await calculateFileSHA256(rawFile)
        const precheck = await api.post('/file/precheck', {
          original_name: rawFile.name,
          file_sha256: fileSHA256,
          size: rawFile.size
        })
        if (!precheck.data || precheck.data.status_code !== 1000) {
          ElMessage.error(precheck.data?.status_msg || '上传预检失败')
          return
        }
        if (precheck.data.reused) {
          ElMessage.success('秒传成功')
          await loadFiles()
          return
        }
        if (!precheck.data.need_upload) {
          ElMessage.error('上传预检失败')
          return
        }

        const formData = new FormData()
        formData.append('file', rawFile)
        formData.append('file_sha256', fileSHA256)
        const response = await api.post('/file/upload', formData)
        if (response.data && response.data.status_code === 1000) {
          ElMessage.success('文件上传成功')
          await loadFiles()
        } else {
          ElMessage.error(response.data?.status_msg || '上传失败')
        }
      } catch (error) {
        console.error('Upload knowledge file error:', error)
        ElMessage.error(getApiErrorMessage(error, '文件上传失败'))
      } finally {
        uploading.value = false
      }
    }

    const deleteFile = async (row) => {
      try {
        await ElMessageBox.confirm(`确定删除「${row.original_name}」吗？`, '删除文件', {
          confirmButtonText: '删除',
          cancelButtonText: '取消',
          type: 'warning'
        })
        const response = await api.delete(`/file/${row.id}`)
        if (response.data && response.data.status_code === 1000) {
          ElMessage.success('文件已删除')
          await loadFiles()
        } else {
          ElMessage.error(response.data?.status_msg || '删除失败')
        }
      } catch (error) {
        if (error !== 'cancel') {
          console.error('Delete knowledge file error:', error)
          ElMessage.error('删除失败')
        }
      }
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

    const formatTime = (time) => {
      if (!time) return '-'
      return new Date(time).toLocaleString()
    }

    onMounted(loadFiles)

    return {
      Delete,
      files,
      loading,
      uploading,
      loadFiles,
      handleUpload,
      deleteFile,
      formatSize,
      formatTime
    }
  }
}
</script>

<style scoped>
.knowledge-page {
  min-height: 100vh;
  padding: 24px;
  background: #f5f7fa;
}

.page-header {
  display: grid;
  grid-template-columns: 44px 1fr 44px;
  align-items: center;
  gap: 16px;
  max-width: 1120px;
  margin: 0 auto 20px;
}

.page-header h1 {
  margin: 0;
  color: #1f2937;
  font-size: 28px;
  font-weight: 700;
}

.page-header p {
  margin: 6px 0 0;
  color: #6b7280;
  font-size: 14px;
}

.icon-btn {
  width: 44px;
  height: 44px;
  border: 1px solid #dcdfe6;
  border-radius: 8px;
  background: white;
  color: #303133;
  cursor: pointer;
}

.icon-btn:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.toolbar,
.file-table {
  max-width: 1120px;
  margin: 0 auto;
}

.toolbar {
  margin-bottom: 16px;
}

.upload :deep(.el-upload) {
  width: 100%;
}

.upload :deep(.el-upload-dragger) {
  width: 100%;
  border-radius: 8px;
  padding: 28px;
}

.upload-icon {
  color: #409eff;
  font-size: 34px;
}

.upload-text {
  color: #4b5563;
  font-size: 15px;
}

.file-table {
  border-radius: 8px;
  overflow: hidden;
}

.file-name {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.file-name span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 768px) {
  .knowledge-page {
    padding: 16px;
  }

  .page-header {
    grid-template-columns: 40px 1fr 40px;
  }
}
</style>
