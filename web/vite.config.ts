/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  // NOTE: outDir is the Go embed dir (pkg/grapheditor/static). emptyOutDir wipes
  // it on every build — including the committed placeholder index.html — and
  // replaces it with the bundle. CI/Makefile must run `npm run build` before
  // `go build` on a clean checkout (handled in the build/embed task).
  build: {
    outDir: '../pkg/grapheditor/static',
    emptyOutDir: true,
  },
  server: {
    proxy: { '/api': 'http://127.0.0.1:7390' },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
  },
});
