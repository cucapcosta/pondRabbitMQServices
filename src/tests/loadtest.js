import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

const successfulRequests = new Counter('successful_requests');

const targetUrl = __ENV.TARGET_URL || 'http://back:3420/add';

export const options = {
  stages: [
    { duration: '5s', target: 10 },
    { duration: '5s', target: 10 },
    { duration: '10s', target: 10000 },
    { duration: '5s', target: 10000 },
    { duration: '5s', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{expected_response:true}': ['p(95)<2000'],
    checks: ['rate>0.99'],
  },
};

export function setup() {
  const backendBase = targetUrl.replace(/\/add$/, '');
  const deadline = Date.now() + 30000;

  while (Date.now() < deadline) {
    try {
      const res = http.get(backendBase);
      if (res.status > 0) {
        console.log('Backend is ready');
        return {};
      }
    } catch (_) {}
    }
    console.log('Waiting for backend...');
    sleep(2);
  }

  console.log('WARNING: backend may not be ready, proceeding anyway');
  return {};
}

const sensorTypes = ['temperature', 'humidity', 'pressure', 'luminosity'];
const units = ['°C', '%', 'hPa', 'lux'];

export default function () {
  const idx = Math.floor(Math.random() * sensorTypes.length);

  const payload = JSON.stringify({
    SensorID: `sensor-${__VU}-${__ITER}`,
    Timestamp: new Date().toISOString(),
    Type: sensorTypes[idx],
    Unit: units[idx],
    IsDiscrete: false,
    Value: Math.round(Math.random() * 1000) / 10.0,
  });

  const res = http.post(targetUrl, payload, {
    headers: { 'Content-Type': 'application/json' },
  });

  const passed = check(res, {
    'status is 200': (r) => r.status === 200,
  });

  if (passed) {
    successfulRequests.add(1);
  }
}

export function handleSummary(data) {
  const successCount =
    data.metrics.successful_requests
      ? data.metrics.successful_requests.values.count
      : 0;

  const result = { success_count: successCount };

  return {
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
    '/tmp/k6-results.json': JSON.stringify(result),
  };
}
