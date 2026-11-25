import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
const backend = process.env.STATIC_HOST_BACKEND || 'http://127.0.0.1:8081'
export default defineConfig({ plugins: [vue(), tailwindcss()], build: { outDir: '../internal/web/dist', emptyOutDir: true }, server: { host: '127.0.0.1', port: 5173, proxy: Object.fromEntries(['/api', '/mcp', '/healthz'].map(p => [p, { target: backend, changeOrigin: true }])) } })
