'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

async function loadUtils() {
  return import('../../webui/src/features/overview/usageLedgerUtils.js');
}

test('usageQueryParams formats parameters correctly', async () => {
  const { usageQueryParams, buildUsagePath } = await loadUtils();

  // 'all' range omits start/end
  const paramsAll = usageQueryParams('all');
  assert.equal(paramsAll.get('range'), 'all');
  assert.equal(paramsAll.has('start'), false);
  assert.equal(paramsAll.has('end'), false);
  assert.equal(buildUsagePath(paramsAll).includes('range=all'), true);

  // '15m' range
  const params15m = usageQueryParams('15m');
  assert.equal(params15m.get('range'), '15m');

  // 'custom' range includes epoch ms
  const customStart = '2026-09-01T00:00:00Z';
  const customEnd = '2026-09-02T00:00:00Z';
  const expectedStartMs = new Date(customStart).getTime();
  const expectedEndMs = new Date(customEnd).getTime();

  const paramsCustom = usageQueryParams('custom', customStart, customEnd);
  assert.equal(paramsCustom.get('range'), 'custom');
  assert.equal(paramsCustom.get('start'), String(expectedStartMs));
  assert.equal(paramsCustom.get('end'), String(expectedEndMs));
});

test('mapUsageTimeline maps required fields', async () => {
  const { mapUsageTimeline } = await loadUtils();

  const timeline = [
    {
      start_ms: 1700000000000,
      end_ms: 1700003600000,
      prompt: 100,
      completion: 200,
      reasoning: 50,
      total: 300,
      count: 2,
      avg_latency_ms: 450,
      models: ['deepseek-chat', 'deepseek-reasoner'],
    },
  ];

  const mapped = mapUsageTimeline(timeline);
  assert.equal(mapped.length, 1);
  const item = mapped[0];

  assert.equal(item.id, 'bucket-0');
  assert.equal(item.prompt, 100);
  assert.equal(item.completion, 200);
  assert.equal(item.reasoning, 50);
  assert.equal(item.total, 300);
  assert.equal(item.count, 2);
  assert.equal(item.elapsedMs, 450);
  assert.equal(item.model, 'deepseek-chat, deepseek-reasoner');
  assert.ok(item.label);
  assert.ok(item.fullTime);
});

test('mapUsageTotals and mapUsageRecent handle missing fields', async () => {
  const { mapUsageTotals, mapUsageRecent } = await loadUtils();

  // Empty totals
  const totals = mapUsageTotals({});
  assert.equal(totals.totalTokens, 0);
  assert.equal(totals.totalPrompt, 0);
  assert.equal(totals.totalCompletion, 0);
  assert.equal(totals.totalReasoning, 0);
  assert.equal(totals.totalReqs, 0);
  assert.equal(totals.successRate, 100);
  assert.equal(totals.avgLatencyMs, 0);
  assert.equal(totals.tokensPerSec, 0);
  assert.equal(totals.tokensPerMin, 0);

  // Missing fields recent
  const recent = mapUsageRecent([{}]);
  assert.equal(recent.length, 1);
  assert.equal(recent[0].id, 'usage-0');
  assert.equal(recent[0].model, 'unknown');
  assert.equal(recent[0].status, 'unknown');
  assert.equal(recent[0].total_tokens, 0);
  assert.equal(recent[0].elapsed_ms, null);

  // Null input handles gracefully
  assert.deepEqual(mapUsageRecent(null), []);
});

test('isSuccessItem truth table', async () => {
  const { isSuccessItem } = await loadUtils();

  assert.equal(isSuccessItem(null), false);
  assert.equal(isSuccessItem({}), false);

  // Status strings
  assert.equal(isSuccessItem({ status: 'success' }), true);
  assert.equal(isSuccessItem({ status: 'ok' }), true);
  assert.equal(isSuccessItem({ status: 'SUCCESS' }), true);
  assert.equal(isSuccessItem({ status: 'error' }), false);
  assert.equal(isSuccessItem({ status: 'stopped' }), false);

  // Status codes
  assert.equal(isSuccessItem({ status_code: 200 }), true);
  assert.equal(isSuccessItem({ status_code: 204 }), true);
  assert.equal(isSuccessItem({ status_code: 304 }), true);
  assert.equal(isSuccessItem({ status_code: 400 }), false);
  assert.equal(isSuccessItem({ status_code: 500 }), false);

  assert.equal(isSuccessItem({ statusCode: 200 }), true);
  assert.equal(isSuccessItem({ statusCode: 404 }), false);

  // Precedence: explicit error/stopped overrides 200
  assert.equal(isSuccessItem({ status: 'stopped', status_code: 200 }), false);
  assert.equal(isSuccessItem({ status: 'error', status_code: 200 }), false);
});
