import { createRouter, createWebHistory } from 'vue-router'
import Login from '../views/Login.vue'
import Register from '../views/Register.vue'
import Menu from '../views/Menu.vue'
import Memory from '../views/Memory.vue'
import AIChat from '../views/AIChat.vue'
import ImageRecognition from '../views/ImageRecognition.vue'
import KnowledgeBase from '../views/KnowledgeBase.vue'
import { loadCurrentUser, clearUser } from '../utils/auth'
import { ElMessage } from 'element-plus'

const routes = [
  { path: '/memory', name: 'Memory', component: Memory, meta: { requiresAuth: true, role: 'user' } },
  {
    path: '/',
    redirect: '/login'
  },
  {
    path: '/login',
    name: 'Login',
    component: Login
  },
  {
    path: '/register',
    name: 'Register',
    component: Register
  },
  {
    path: '/menu',
    name: 'Menu',
    component: Menu,
    meta: { requiresAuth: true, role: 'user' }
  },
  {
    path: '/ai-chat',
    name: 'AIChat',
    component: AIChat,
    meta: { requiresAuth: true, role: 'user' }
  },
  {
    path: '/image-recognition',
    name: 'ImageRecognition',
    component: ImageRecognition,
    meta: { requiresAuth: true, role: 'user' }
  },
  {
    path: '/knowledge-base',
    name: 'KnowledgeBase',
    component: KnowledgeBase,
    props: { scope: 'personal' },
    meta: { requiresAuth: true, role: 'user' }
  },
  {
    path: '/enterprise-knowledge-base',
    name: 'EnterpriseKnowledgeBase',
    component: KnowledgeBase,
    props: { scope: 'enterprise' },
    meta: { requiresAuth: true, role: 'admin' }
  }
]

const router = createRouter({
  history: createWebHistory(process.env.BASE_URL),
  routes
})

router.beforeEach(async (to) => {
  if (!to.matched.some(record => record.meta.requiresAuth)) return true
  if (!localStorage.getItem('token')) {
    clearUser()
    return '/login'
  }
  try {
    const user = await loadCurrentUser()
    if (to.matched.some(record => record.meta.role && record.meta.role !== user.role)) {
      return user.role === 'admin' ? '/enterprise-knowledge-base' : '/menu'
    }
    return true
  } catch (error) {
    clearUser()
    ElMessage.error(error.message || '获取用户信息失败，请重新登录')
    return '/login'
  }
})

export default router
