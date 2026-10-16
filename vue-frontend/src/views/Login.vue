<template>
  <div class="login-container">
    <el-card class="login-card">
      <template #header>
        <div class="card-header">
          <h2>登录</h2>
        </div>
      </template>
      <el-form
        ref="loginFormRef"
        :model="loginForm"
        :rules="loginRules"
        label-width="100px"
      >
        <el-form-item label="UID/邮箱" prop="identifier">
          <el-input
            v-model="loginForm.identifier"
            placeholder="请输入UID或邮箱"
          />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input
            v-model="loginForm.password"
            placeholder="请输入密码"
            type="password"
            show-password
          />
        </el-form-item>
        <el-form-item style="margin-left: -100px;">
          <el-button
            type="primary"
            :loading="loading"
            @click="handleLogin"
            style="width: 100%"
          >
            登录
          </el-button>
        </el-form-item>
        <el-form-item style="margin-left: -100px;">
          <el-button
            type="text"
            @click="$router.push('/register')"
            style="width: 100%"
          >
            还没有账号？去注册
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../utils/api'
import { clearUser } from '../utils/auth'

export default {
  name: 'LoginView',
  setup() {
    const router = useRouter()
    const loginFormRef = ref()
    const loading = ref(false)
    const loginForm = ref({
      identifier: '',
      password: ''
    })

    const loginRules = {
      identifier: [
        { required: true, message: '请输入UID或邮箱', trigger: 'blur' },
        { 
          validator: (_, value, callback) => {
            if (!value) {
              callback()
              return
            }
            // 如果包含@，验证邮箱格式
            if (value.includes('@')) {
              const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
              if (!emailRegex.test(value)) {
                callback(new Error('请输入有效的邮箱地址'))
                return
              }
            }
            callback()
          }, 
          trigger: 'blur' 
        }
      ],
      password: [
        { required: true, message: '请输入密码', trigger: 'blur' },
        { min: 6, message: '密码长度不能少于6位', trigger: 'blur' }
      ]
    }

    const handleLogin = async () => {
      try {
        await loginFormRef.value.validate()
        loading.value = true

        const identifier = loginForm.value.identifier.trim()
        const password = loginForm.value.password

        // 构建请求数据，后端期望identifier字段
        const requestData = {
          identifier: identifier,
          password: password
        }

        const response = await api.post('/user/login', requestData)

        if (response.data.status_code === 1000) {
          // 存储token和用户信息
          clearUser()
          if (response.data.token) {
            localStorage.setItem('token', response.data.token)
          }
          if (response.data.user_info) {
            localStorage.setItem('userInfo', JSON.stringify(response.data.user_info))
          }

          ElMessage.success('登录成功')
          router.push('/menu')
        } else {
          ElMessage.error(response.data.status_msg || '登录失败，请检查UID和密码')
        }
      } catch (error) {
        console.error('Login error:', error)

        // 处理不同类型的错误
        if (error.response) {
          // 服务器返回的错误
          const status = error.response.status
          if (status === 401) {
            ElMessage.error('UID或密码错误')
          } else if (status === 429) {
            ElMessage.error('请求过于频繁，请稍后再试')
          } else {
            ElMessage.error(error.response.data?.status_msg || '登录失败，请重试')
          }
        } else if (error.request) {
          // 网络错误
          ElMessage.error('网络连接失败，请检查网络设置')
        } else {
          // 其他错误
          ElMessage.error('登录失败，请重试')
        }
      } finally {
        loading.value = false
      }
    }

    return {
      loginFormRef,
      loading,
      loginForm,
      loginRules,
      handleLogin
    }
  }
}
</script>

<style scoped>
.login-container {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
  padding: 24px;
  background: var(--app-bg);
}

.login-card {
  width: min(420px, 100%);
  border: 1px solid var(--app-border-soft);
  box-shadow: var(--app-shadow);
}

.card-header {
  text-align: center;
  padding: 16px 0 8px;
}

.card-header h2 {
  margin: 0;
  color: var(--app-text);
  font-size: 26px;
  font-weight: 700;
}

.el-form-item {
  margin-bottom: 22px;
}

.el-button {
  height: 44px;
  font-weight: 600;
}

.login-link {
  text-align: center;
  margin-top: 20px;
}

@media (max-width: 520px) {
  .login-container {
    align-items: flex-start;
    padding-top: 56px;
  }
}
</style>
