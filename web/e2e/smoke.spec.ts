import { test, expect, type Page } from '@playwright/test';

// All API traffic goes to VITE_API_BASE_URL (set in playwright.config.ts) and is
// fulfilled here, so the smoke test needs no running backend.
const API = 'http://api.gargi.test';

const EXECUTION_ID = 'exec-smoke-1';

const executionDetail = {
  executionId: EXECUTION_ID,
  status: 'EXECUTION_STATUS_SUCCEEDED',
  nodeStates: {
    fetch_user: { nodeId: 'fetch_user', status: 'NODE_STATUS_SUCCEEDED' },
    calculate_discount: { nodeId: 'calculate_discount', status: 'NODE_STATUS_FAILED' },
    generate_invoice: { nodeId: 'generate_invoice', status: 'NODE_STATUS_SKIPPED' },
  },
};

async function mockApi(page: Page) {
  await page.route(`${API}/**`, async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/v1/executions') {
      return route.fulfill({
        json: {
          executions: [
            { executionId: EXECUTION_ID, status: 'EXECUTION_STATUS_SUCCEEDED', createdAt: new Date().toISOString() },
          ],
        },
      });
    }
    if (url.pathname === `/v1/executions/${EXECUTION_ID}`) {
      return route.fulfill({ json: executionDetail });
    }
    return route.fulfill({ status: 404, json: { message: 'not mocked' } });
  });
}

test.beforeEach(async ({ page }) => {
  await mockApi(page);
  await page.goto('/');
});

test('loads the execution canvas', async ({ page }) => {
  await expect(page.getByRole('heading', { name: 'Gargi Flow' })).toBeVisible();
  await expect(page.locator('.react-flow')).toBeVisible();
  await expect(page.getByText('Recent Executions')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Run' })).toBeVisible();
});

test('selecting an example experiment renders its nodes', async ({ page }) => {
  await page.locator('header select').selectOption({ label: 'Linear Pipeline (JSON)' });

  const nodes = page.locator('.react-flow__node');
  await expect(nodes).toHaveCount(3);
  await expect(page.locator('textarea')).toHaveValue(/"nodes"/);
});

test('selecting an execution shows node status badges', async ({ page }) => {
  await page.locator('header select').selectOption({ label: 'Linear Pipeline (JSON)' });
  await expect(page.locator('.react-flow__node')).toHaveCount(3);

  // Before any execution, every node shows the unspecified badge.
  await expect(page.locator('.custom-node').getByText('UNSPECIFIED')).toHaveCount(3);

  await page.getByText(EXECUTION_ID).click();

  const node = (id: string) => page.locator(`.react-flow__node[data-id="${id}"]`);
  await expect(node('fetch_user')).toContainText('SUCCEEDED');
  await expect(node('calculate_discount')).toContainText('FAILED');
  await expect(node('generate_invoice')).toContainText('SKIPPED');
});
