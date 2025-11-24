import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/health': 'http://127.0.0.1:7788',
      '/metrics': 'http://127.0.0.1:7788',
      '/services': 'http://127.0.0.1:7788',
      '/processes': 'http://127.0.0.1:7788',
      '/files': 'http://127.0.0.1:7788',
      '/logs': 'http://127.0.0.1:7788',
      '/ws': {
        target: 'ws://127.0.0.1:7788',
        ws: true,
      },
    },
  },
  build: { outDir: 'dist' }
})
