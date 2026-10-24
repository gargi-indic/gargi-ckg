import { test, expect, type Page } from '@playwright/test';

const API = 'http://api.gargi.test';

type Issue = { path: string; code: string; message: string };

// Mocks the API and records the body of each validate request.
async function mockApi(page: Page, respond: () => Issue[]) {
  const bodies: { sim?: string; graph?: string; format?: string }[] = [];
  await page.route(`${API}/**`, async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/v1/models:validate') {
      bodies.push(route.request().postDataJSON());
      const errors = respond();
      return route.fulfill({ json: { valid: errors.length === 0, errors } });
    }
    if (url.pathname === '/v1/executions') {
      return route.fulfill({ json: { executions: [] } });
    }
    return route.fulfill({ status: 404, json: { message: 'not mocked' } });
  });
  return bodies;
}

async function loadLoan(page: Page) {
  await page.goto('/');
  await page.locator('header select').selectOption({ label: 'Loan Underwriting (Simulation)' });
  await page.getByTestId('sim-tab-sim').click();
  await expect(page.getByTestId('sim-editor-sim')).toHaveValue(/loan-underwriting/);
}

test('edit a world param and validate with no errors', async ({ page }) => {
  const bodies = await mockApi(page, () => []);
  await loadLoan(page);

  const sim = page.getByTestId('sim-editor-sim');
  const text = await sim.inputValue();
  expect(text).toContain('p_bad: 0.20');
  await sim.fill(text.replace('p_bad: 0.20', 'p_bad: 0.25'));

  await page.getByTestId('sim-validate').click();
  await expect(page.getByTestId('sim-validate-ok')).toHaveText('No errors');
  expect(bodies).toHaveLength(1);
  expect(bodies[0].sim).toContain('0.25');
  expect(bodies[0].format).toBe('yaml');
  expect(bodies[0].graph).toContain('nodes');
});

test('validation errors are listed', async ({ page }) => {
  await mockApi(page, () => [
    { path: 'behaviors.ai_score', code: 'unobserved_reference', message: 'ai_score reads an unobserved field' },
  ]);
  await loadLoan(page);

  await page.getByTestId('sim-validate').click();
  const items = page.getByTestId('sim-validate-errors').locator('li');
  await expect(items).toHaveCount(1);
  await expect(items.first()).toHaveText(
    'behaviors.ai_score · unobserved_reference · ai_score reads an unobserved field',
  );
  await expect(page.getByTestId('sim-validate-ok')).toHaveCount(0);
});
