import { ref } from 'vue'
import api from './api'

export const currentUser = ref(null)

let cachedToken = null
let pendingRequest = null
let generation = 0

export function clearUser() {
  generation++
  cachedToken = null
  pendingRequest = null
  currentUser.value = null
  localStorage.removeItem('userInfo')
}

export async function loadCurrentUser() {
  const token = localStorage.getItem('token')
  if (!token) {
    clearUser()
    throw new Error('请先登录')
  }
  if (cachedToken === token && currentUser.value) return currentUser.value
  if (pendingRequest?.token === token) return pendingRequest.promise

  clearUser()
  const requestGeneration = generation
  const request = { token, promise: null }
  request.promise = (async () => {
    const { data } = await api.get('/user/me')
    if (requestGeneration !== generation || token !== localStorage.getItem('token')) {
      throw new Error('登录状态已改变，请重新登录')
    }
    if (data.status_code !== 1000 || !data.user_info) {
      throw new Error(data.status_msg || '获取用户信息失败')
    }
    currentUser.value = data.user_info
    cachedToken = token
    localStorage.setItem('userInfo', JSON.stringify(data.user_info))
    return currentUser.value
  })()
  pendingRequest = request
  try {
    return await request.promise
  } finally {
    if (pendingRequest === request) pendingRequest = null
  }
}
