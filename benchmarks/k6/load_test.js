import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 50 },  // Ramp up to 50 users
    { duration: '1m', target: 50 },   // Sustain 50 users
    { duration: '30s', target: 100 }, // Spike to 100 users
    { duration: '1m', target: 100 },  // Sustain 100 users
    { duration: '30s', target: 0 },   // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<100'], // 95% of requests must complete below 100ms
    http_req_failed: ['rate<0.01'],    // Less than 1% failure rate
  },
};

const BASE_URL = __ENV.TARGET_URL || 'http://localhost:8080';

export default function () {
  // Scenario 1: Health probe check
  const healthRes = http.get(`${BASE_URL}/healthz`);
  check(healthRes, {
    'healthz status is 200': (r) => r.status === 200,
  });

  // Scenario 2: Prometheus metrics scrape
  const metricsRes = http.get(`${BASE_URL}/metrics`);
  check(metricsRes, {
    'metrics status is 200': (r) => r.status === 200,
    'metrics has gargi metrics': (r) => r.body.includes('gargi_'),
  });

  // Scenario 3: OpenAPI specification access
  const openapiRes = http.get(`${BASE_URL}/openapi.json`);
  check(openapiRes, {
    'openapi status is 200': (r) => r.status === 200,
  });

  // Scenario 4: Async Graph Execution request
  const payload = JSON.stringify({
    graphDefinition: {
      graphId: 'load-test-graph',
      version: '1.0.0',
      nodes: [
        {
          id: 'step1',
          type: 'reference',
          inputs: { action: 'compute' },
        },
      ],
    },
    async: true,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': `k6-${__VU}-${__ITER}`,
    },
  };

  const execRes = http.post(`${BASE_URL}/v1/executions`, payload, params);
  check(execRes, {
    'execute response valid': (r) => r.status === 200 || r.status === 202,
  });

  sleep(0.5);
}
