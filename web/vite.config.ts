import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
export default defineConfig({ plugins: [vue(), tailwindcss()], build: { outDir: '../internal/web/dist', emptyOutDir: true }, server: { host: '127.0.0.1', port: 5173, proxy: Object.fromEntries(['/api', '/mcp', '/healthz'].map(p => [p, { target: 'http://127.0.0.1:8080', changeOrigin: true }])) } })
