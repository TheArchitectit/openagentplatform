import { defineConfig } from 'vitest/config';

// Vitest configuration. Kept separate from vite.config.ts because the project
// uses vite 6 while vitest 2.x bundles vite 5, and merging the `test` field
// into vite's defineConfig triggers a Plugin type mismatch. Vitest picks up
// vitest.config.ts automatically and merges it with vite.config.ts.
export default defineConfig({
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./test/setup.ts'],
  },
});
