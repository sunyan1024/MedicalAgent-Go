<template>
  <div id="app">
    <router-view v-slot="{ Component }">
      <transition name="page" mode="out-in">
        <component :is="Component" />
      </transition>
    </router-view>
  </div>
</template>

<script>
export default {
  name: 'App',
  mounted() {
    const setPageHidden = () => {
      document.body.classList.toggle('page-hidden', document.hidden)
    }
    document.addEventListener('visibilitychange', setPageHidden)
    this._onVisibilityChange = setPageHidden
  },
  beforeUnmount() {
    if (this._onVisibilityChange) {
      document.removeEventListener('visibilitychange', this._onVisibilityChange)
    }
  }
}
</script>

<style>
:root {
  --app-bg: #f3f6fb;
  --app-surface: #ffffff;
  --app-surface-subtle: #f8fafc;
  --app-border: #dfe5ef;
  --app-border-soft: #edf1f7;
  --app-text: #1f2937;
  --app-text-muted: #6b7280;
  --app-primary: #2563eb;
  --app-primary-hover: #1d4ed8;
  --app-primary-soft: #eff6ff;
  --app-success: #16a34a;
  --app-warning: #d97706;
  --app-danger: #dc2626;
  --app-shadow: 0 8px 24px rgba(15, 23, 42, 0.06);
  --app-radius: 8px;
}

* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

html, body {
  height: 100%;
  font-family: 'Helvetica Neue', Helvetica, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', '微软雅黑', Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  background: var(--app-bg);
  color: var(--app-text);
}

#app {
  height: 100%;
}

/* 页面切换动画 */
.page-enter-active,
.page-leave-active {
  transition: opacity 0.18s ease;
}

.page-enter-from {
  opacity: 0;
}

.page-leave-to {
  opacity: 0;
}

/* 全局滚动条样式 */
::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

::-webkit-scrollbar-track {
  background: transparent;
  border-radius: 4px;
}

::-webkit-scrollbar-thumb {
  background: #cbd5e1;
  border-radius: 4px;
  transition: background 0.3s ease;
}

::-webkit-scrollbar-thumb:hover {
  background: #94a3b8;
}

/* Element Plus 组件样式覆盖 */
.el-button {
  font-weight: 500;
  border-radius: var(--app-radius);
}

.el-input__wrapper {
  border-radius: var(--app-radius);
}

.el-card {
  border-radius: var(--app-radius);
  border-color: var(--app-border-soft);
  box-shadow: var(--app-shadow);
}

.el-message {
  border-radius: var(--app-radius);
}

.el-table {
  color: var(--app-text);
}

/* 页面不可见时暂停所有动画，减少 GPU 合成压力 */
body.page-hidden *,
body.page-hidden *::before,
body.page-hidden *::after {
  animation-play-state: paused !important;
}

/* 响应式设计 */
@media (max-width: 768px) {
  .page-enter-from,
  .page-leave-to {
    transform: translateX(0);
    opacity: 0;
  }

  .page-enter-active,
  .page-leave-active {
    transition: opacity 0.3s ease;
  }
}
</style>
