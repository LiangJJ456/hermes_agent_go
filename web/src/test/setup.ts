import '@testing-library/jest-dom/vitest';

class RO {
  observe() {}
  unobserve() {}
  disconnect() {}
}
// jsdom lacks ResizeObserver; polyfill for React Flow
if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = RO as unknown as typeof ResizeObserver;
}
