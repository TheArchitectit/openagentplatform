import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist', 'node_modules', 'web/routeTree.gen.ts'] },
  {
    extends: [js.configs.recommended],
  },
  ...tseslint.configs.recommended,
  {
    files: ['src/**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      globals: { ...globals.browser, ...globals.node },
      parserOptions: {
        project: './tsconfig.json',
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // Project does not enforce noUnusedLocals/noUnusedParameters (tsconfig).
      '@typescript-eslint/no-unused-vars': 'off',
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-empty-object-type': 'off',
      '@typescript-eslint/no-non-null-assertion': 'off',
      '@typescript-eslint/ban-ts-comment': 'off',
      'no-empty': 'off',
      'no-case-declarations': 'off',
      'no-fallthrough': 'off',
      'no-control-regex': 'off',
      'prefer-const': 'warn',
    },
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': 'off',
      // The react-hooks plugin v7 ships experimental rules (set-state-in-effect,
      // refs, immutability, preserve-manual-memoization, static-components) that
      // enforce post-2024 React guidance. This codebase predates them and uses
      // the established "set loading flag then fetch in effect" pattern
      // pervasively; enabling them would require rewriting ~40 hooks. They are
      // opted out here; the stable recommended rules remain active.
      'react-hooks/set-state-in-effect': 'off',
      'react-hooks/refs': 'off',
      'react-hooks/immutability': 'off',
      'react-hooks/preserve-manual-memoization': 'off',
      'react-hooks/static-components': 'off',
    },
  },
  {
    files: ['*.config.ts', 'vite.config.ts', 'vitest.config.ts'],
    languageOptions: {
      globals: { ...globals.node, ...globals.browser },
    },
  }
)
