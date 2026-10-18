<template>
  <div class="menu-container">
    <el-header class="header">
      <h1>AI医疗助理 <el-tag v-if="currentUser?.role === 'admin'">管理员</el-tag></h1>
      <el-button type="danger" @click="handleLogout">退出登录</el-button>
    </el-header>
    <el-main class="main">
      <div class="menu-grid">
        <el-card class="menu-item" @click="$router.push('/ai-chat')">
          <div class="card-content">
            <el-icon size="48" color="#409eff"><ChatDotRound /></el-icon>
            <h3>医疗 Agent</h3>
            <p>症状咨询、医学知识检索、联网搜索、报告解读</p>
          </div>
        </el-card>
        <el-card class="menu-item" @click="$router.push('/knowledge-base')">
          <div class="card-content">
            <el-icon size="48" color="#e6a23c"><Document /></el-icon>
            <h3>个人知识库</h3>
            <p>管理仅本人可访问的问答文档</p>
          </div>
        </el-card>
        <el-card class="menu-item" @click="$router.push('/memory')">
          <div class="card-content">
            <el-icon size="48" color="#8b5cf6"><Document /></el-icon>
            <h3>个人档案</h3>
            <p>查看和编辑 Agent 记住的偏好与健康背景</p>
          </div>
        </el-card>
      </div>
    </el-main>
  </div>
</template>

<script>
import { useRouter } from 'vue-router'
import { clearUser, currentUser } from '../utils/auth'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ChatDotRound, Document } from '@element-plus/icons-vue'

export default {
  name: 'MenuView',
  components: {
    ChatDotRound,
    Document
  },
  setup() {
    const router = useRouter()

    const handleLogout = async () => {
      try {
        await ElMessageBox.confirm('确定要退出登录吗？', '提示', {
          confirmButtonText: '确定',
          cancelButtonText: '取消',
          type: 'warning'
        })
        localStorage.removeItem('token')
        clearUser()
        ElMessage.success('退出登录成功')
        router.push('/login')
      } catch {
        // 用户取消操作
      }
    }

    return {
      currentUser,
      handleLogout
    }
  }
}
</script>

<style scoped>
.menu-container {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  background: var(--app-bg);
}

.header {
  height: 64px;
  background: var(--app-surface);
  color: var(--app-text);
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 32px;
  border-bottom: 1px solid var(--app-border-soft);
}

.header h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
}

.main {
  flex: 1;
  display: flex;
  justify-content: flex-start;
  align-items: flex-start;
  padding: 32px;
}

.menu-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 20px;
  max-width: 1120px;
  width: 100%;
  margin: 0 auto;
}

.menu-item {
  cursor: pointer;
  background: var(--app-surface);
  border: 1px solid var(--app-border-soft);
  box-shadow: none;
  transition: border-color 0.18s ease, box-shadow 0.18s ease, transform 0.18s ease;
  overflow: hidden;
}

.menu-item:hover {
  transform: translateY(-2px);
  border-color: #bfdbfe;
  box-shadow: var(--app-shadow);
}

.card-content {
  text-align: left;
  padding: 28px;
}

.el-icon {
  display: block;
  margin-bottom: 20px;
}

.card-content h3 {
  margin: 0 0 8px;
  color: var(--app-text);
  font-size: 20px;
  font-weight: 700;
}

.card-content p {
  margin: 0;
  color: var(--app-text-muted);
  font-size: 14px;
  line-height: 1.5;
}

@media (max-width: 768px) {
  .header {
    padding: 0 16px;
  }

  .main {
    padding: 20px 16px;
  }

  .menu-grid {
    grid-template-columns: 1fr;
  }
}
</style>
