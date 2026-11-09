import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@': new URL('./src', import.meta.url).pathname } },
  build: { outDir: '../server/internal/webui/dist', emptyOutDir: true, sourcemap: false, chunkSizeWarningLimit: 900 },
  server: { port: 5173, proxy: { '/api': 'http://localhost:8080', '/hook': 'http://localhost:8080', '/r': 'http://localhost:8080', '/book': 'http://localhost:8080', '/files': 'http://localhost:8080', '/t': 'http://localhost:8080' } },
})
