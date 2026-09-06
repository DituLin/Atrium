import js from '@eslint/js';
import reactHooks from 'eslint-plugin-react-hooks';
import globals from 'globals';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['dist/**', 'node_modules/**', 'coverage/**'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['src/**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2020,
      globals: { ...globals.browser, __CLIENT_VERSION__: 'readonly' },
    },
    plugins: { 'react-hooks': reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      '@typescript-eslint/consistent-type-imports': ['error', { prefer: 'type-imports' }],
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      'no-restricted-globals': [
        'error',
        { name: 'fetch', message: 'Use the ApiClient from src/core/api.ts.' },
      ],
    },
  },
  {
    // No `any` anywhere in the core layer (project rule).
    files: ['src/core/**/*.ts'],
    rules: { '@typescript-eslint/no-explicit-any': 'error' },
  },
  {
    // The API client owns `fetch`; test harnesses build their own transport.
    files: [
      'src/core/api.ts',
      'src/core/auth.ts',
      'src/test/**/*.ts',
      'src/**/*.test.ts',
      'src/**/*.test.tsx',
    ],
    rules: { 'no-restricted-globals': 'off' },
  },
  {
    files: ['vite.config.ts', 'mock/**/*.ts', 'scripts/**/*.mjs', 'eslint.config.js'],
    languageOptions: { globals: { ...globals.node } },
    rules: { '@typescript-eslint/no-explicit-any': 'off' },
  },
);
