import { defineConfig, devices } from '@playwright/test';

// Smoke tests run against the Vite dev server with all backend calls mocked
// (see e2e/smoke.spec.ts), so no Go server, worker or Temporal is required.
// Browsers are not downloaded by `npm install`; run `npx playwright install chromium` once.
const PORT = 4173;

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: `http://localhost:${PORT}`,
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `npm run dev -- --port ${PORT} --strictPort`,
    url: `http://localhost:${PORT}`,
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
    env: { VITE_API_BASE_URL: 'http://api.gargi.test' },
  },
});
