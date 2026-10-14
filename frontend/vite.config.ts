import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      // Keep the browser's Host, so the backend's cross-origin check
      // (GLG-407) sees Origin and Host match even when the browser sends no
      // Sec-Fetch-Site, as over plain http from another device (--host).
      // PORT is the backend's own variable, so `PORT=8081 make dev` moves
      // both ends together.
      '/api': {
        target: `http://localhost:${process.env.PORT || '8080'}`,
        changeOrigin: false,
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/setupTests.ts'],
    // Above the 5s findBy/waitFor timeout in setupTests.ts, so a slow wait
    // fails with its DOM dump, not a bare test timeout.
    testTimeout: 15000,
  },
})
