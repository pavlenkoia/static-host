import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  globalSetup: './e2e/global-setup.ts',
  forbidOnly: true,
  retries: 0,
  timeout: 60_000,
  testDir: './e2e',
  testMatch: '*.spec.ts',
  fullyParallel: false,
  workers: 1,
  use: {
    ...devices['Desktop Chrome'],
    trace: 'retain-on-failure',
  },
})
