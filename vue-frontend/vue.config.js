module.exports = {
  devServer: {
    port: 8080,
    client: {
      overlay: {
        // Keep real runtime errors visible. This browser layout notification
        // remains in the console, but must not block the entire dev UI.
        runtimeErrors: error => error?.message !== 'ResizeObserver loop completed with undelivered notifications.'
      }
    },
    proxy: {
      '/api': {
        target: 'http://localhost:9090',
        changeOrigin: true,
        pathRewrite: {
          '^/api': '/api/v3'
        }
      }
    }
  }
}