import axios from 'axios'

const api = axios.create({
  baseURL: '/api', // 使用代理路径，开发环境会自动代理到后端
  timeout: 0  //不启用超时机制
  // timeout: 30000  // 30秒超时
})

export const getApiErrorMessage = (error, fallback) => {
  if (error?.response?.data?.status_msg) {
    return error.response.data.status_msg
  }
  if (error?.message) {
    return error.message
  }
  return fallback
}

// 请求拦截器
api.interceptors.request.use(
  config => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  error => {
    return Promise.reject(error)
  }
)

// 响应拦截器
api.interceptors.response.use(
  response => {
    return response
  },
  error => {
    if (error.response && error.response.status === 401) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

export default api
