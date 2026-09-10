import { ref } from 'vue'
import api from './api'

export const currentUser = ref(null)

export function clearUser() {
  currentUser.value = null
  localStorage.removeItem('userInfo')
}

export async function loadCurrentUser() {
  const token = localStorage.getItem('token')
  clearUser()
  const { data } = await api.get('/user/me')
  if (token !== localStorage.getItem('token')) throw new Error('登录状态已改变，请重新登录')
  if (data.status_code !== 1000 || !data.user_info) {
    throw new Error(data.status_msg || '获取用户信息失败')
  }
  currentUser.value = data.user_info
  localStorage.setItem('userInfo', JSON.stringify(data.user_info))
  return currentUser.value
}
