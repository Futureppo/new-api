/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import { describe, expect, test } from 'bun:test';
import {
  applyAzureDiscoveryResult,
  azureConnectionDraft,
  createAzureDiscoveryRunner,
} from './azureDiscovery';

const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const deferred = () => {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
};

describe('Azure discovery lifecycle', () => {
  test('debounces edits and manual fetch replaces the pending automatic request', async () => {
    const requests = [];
    const results = [];
    const runner = createAzureDiscoveryRunner({
      delay: 15,
      request: async (snapshot, target) => {
        requests.push([snapshot, target]);
        return snapshot;
      },
      onResult: (value) => results.push(value),
      onError: () => {},
      onLoading: () => {},
    });
    runner.schedule('old');
    runner.schedule('latest');
    await pause(35);
    expect(requests).toEqual([['latest', 'all']]);
    runner.schedule('pending');
    await runner.run('manual', 'region');
    await pause(35);
    expect(requests).toEqual([
      ['latest', 'all'],
      ['manual', 'region'],
    ]);
    expect(results).toEqual(['latest', 'manual']);
    runner.cancel();
  });

  test('drops stale responses and cancels requests when switching or closing', async () => {
    const first = deferred();
    const second = deferred();
    const results = [];
    const signals = [];
    const runner = createAzureDiscoveryRunner({
      request: (snapshot, target, signal) => {
        signals.push(signal);
        return snapshot === 'first' ? first.promise : second.promise;
      },
      onResult: (value) => results.push(value),
      onError: () => {},
      onLoading: () => {},
    });
    const old = runner.run('first');
    const current = runner.run('second');
    expect(signals[0].aborted).toBe(true);
    second.resolve('new response');
    await current;
    first.resolve('stale response');
    await old;
    expect(results).toEqual(['new response']);
    runner.schedule('closed');
    runner.cancel();
    await pause(20);
    expect(signals).toHaveLength(2);
  });

  test('manual retry can succeed after a failure', async () => {
    let attempts = 0;
    const errors = [];
    const results = [];
    const runner = createAzureDiscoveryRunner({
      request: async () => {
        if (++attempts === 1) throw new Error('timeout');
        return { models: ['deployment'] };
      },
      onResult: (value) => results.push(value),
      onError: (error) => errors.push(error.message),
      onLoading: () => {},
    });
    expect(await runner.run('same')).toBeNull();
    await runner.run('same');
    expect(errors).toEqual(['timeout']);
    expect(results).toEqual([{ models: ['deployment'] }]);
    runner.cancel();
  });
});

describe('Azure form updates', () => {
  const response = {
    models: ['deployment'],
    model_source: 'deployments',
    region: 'eastus',
    region_source: 'resource',
  };
  test('fills empty models from deployments and persists region while retaining other settings', () => {
    const next = applyAzureDiscoveryResult(
      { models: [], settings: '{"azure_responses_version":"preview"}' },
      response,
      'all',
    );
    expect(next.models).toEqual(['deployment']);
    expect(JSON.parse(next.settings)).toEqual({
      azure_responses_version: 'preview',
      azure_region: 'eastus',
      azure_region_source: 'resource',
    });
  });
  test('catalog and unknown sources never auto-fill channel models but still update region', () => {
    for (const model_source of ['catalog', '', undefined]) {
      const next = applyAzureDiscoveryResult(
        { models: [], settings: '{}' },
        { ...response, model_source, models: ['undeployed-base-model'] },
        'all',
      );
      expect(next.models).toEqual([]);
      expect(next.azure_region).toBe('eastus');
      expect(JSON.parse(next.settings).azure_region).toBe('eastus');
    }
    expect(
      applyAzureDiscoveryResult(
        { models: [] },
        { ...response, model_source: 'catalog', management_fallback: true },
        'models',
      ).models,
    ).toEqual([]);
  });
  test('keeps existing choices and handles model and region failures independently', () => {
    const original = {
      models: ['custom'],
      azure_region: 'westus',
      settings: '{}',
    };
    expect(applyAzureDiscoveryResult(original, response, 'all').models).toEqual(
      ['custom'],
    );
    const next = applyAzureDiscoveryResult(
      original,
      { ...response, region_error: 'timeout' },
      'all',
    );
    expect(next.azure_region).toBe('westus');
    expect(
      applyAzureDiscoveryResult(
        { models: [] },
        { ...response, model_error: '503' },
        'all',
      ).models,
    ).toEqual([]);
    expect(
      applyAzureDiscoveryResult({ models: [] }, response, 'region').models,
    ).toEqual([]);
  });
  test('draft contains only explicit connection fields', () => {
    expect(
      azureConnectionDraft({
        name: 'channel',
        management_token: 'secret',
        azure_region: 'eastus',
      }),
    ).toEqual({
      base_url: '',
      key: '',
      other: '',
      proxy: '',
      header_override: '',
      resource_id: '',
    });
  });
});
