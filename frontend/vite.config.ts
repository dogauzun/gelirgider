import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

const proxy = {
  // Keep the browser's Host, so the backend's cross-origin check sees
  // Origin and Host match even when the browser sends no Sec-Fetch-Site,
  // as over plain http from another device (--host).
  // PORT is the backend's own variable, so `PORT=8081 make dev` moves
  // both ends together.
  '/api': {
    target: `http://localhost:${process.env.PORT || '8080'}`,
    changeOrigin: false,
  },
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  // The release version, set by the image build; vitest uses it too.
  define: { __APP_VERSION__: JSON.stringify(process.env.VITE_APP_VERSION ?? 'dev') },
  server: { proxy },
  // The same proxy for `vite preview`, which serves the built frontend in
  // `make screenshots`.
  preview: { proxy },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/setupTests.ts'],
    // Above the 5s findBy/waitFor timeout in setupTests.ts, so a slow wait
    // fails with its DOM dump, not a bare test timeout.
    testTimeout: 15000,
  },
})
