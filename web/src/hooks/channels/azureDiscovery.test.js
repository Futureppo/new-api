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
  getAzureDiscoveryModelConfig,
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
  test('persists deployment metadata with mapping off and keeps it on partial failures', () => {
    const inputs = {
      models: ['manual'],
      azure_auto_model_mapping: false,
      settings: '{"azure_responses_version":"v1"}',
    };
    const result = {
      model_source: 'deployments',
      models: ['opaque'],
      deployments: [{ id: 'opaque', model: 'claude-opus-5' }],
    };
    const next = applyAzureDiscoveryResult(inputs, result, 'models');
    expect(next.models).toEqual(['manual']);
    expect(JSON.parse(next.settings)).toEqual({
      azure_responses_version: 'v1',
      azure_deployment_models: { opaque: 'claude-opus-5' },
    });
    expect(
      JSON.parse(
        applyAzureDiscoveryResult(next, { model_error: 'timeout' }, 'models')
          .settings,
      ).azure_deployment_models,
    ).toEqual({ opaque: 'claude-opus-5' });
    expect(
      JSON.parse(
        applyAzureDiscoveryResult(
          next,
          { ...result, models: [], deployments: [] },
          'models',
        ).settings,
      ).azure_deployment_models,
    ).toEqual({});
  });
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
      azure_deployment_models: {},
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

describe('Azure deployment model redirects', () => {
  const result = {
    model_source: 'deployments',
    models: ['dep-astra', 'dep-sol', 'sol61'],
    deployments: [
      { id: 'dep-astra', model: 'gpt-6-astra' },
      { id: 'dep-sol', model: 'gpt-6-sol' },
      { id: 'sol61', model: 'gpt-6.1-sol' },
    ],
    region: 'East US 2',
    region_source: 'response_header',
  };

  test('defaults on and fills original names plus deployment redirects', () => {
    const next = applyAzureDiscoveryResult(
      { models: [], model_mapping: '' },
      result,
      'all',
    );
    expect(next.models).toEqual(['gpt-6-astra', 'gpt-6-sol', 'gpt-6.1-sol']);
    expect(JSON.parse(next.model_mapping)).toEqual({
      'gpt-6-astra': 'dep-astra',
      'gpt-6-sol': 'dep-sol',
      'gpt-6.1-sol': 'sol61',
    });
    expect(next.azure_region).toBe('East US 2');
    expect(getAzureDiscoveryModelConfig(next, result).models).toEqual(
      next.models,
    );
  });

  test('disabled mode fills deployment IDs and preserves manual redirects', () => {
    const original = {
      models: [],
      azure_auto_model_mapping: false,
      model_mapping: '{"manual":"keep"}',
    };
    const next = applyAzureDiscoveryResult(original, result, 'all');
    expect(next.models).toEqual(result.models);
    expect(next.model_mapping).toBe(original.model_mapping);
  });

  test('merges without replacing models or manual mappings', () => {
    const next = applyAzureDiscoveryResult(
      {
        models: ['custom'],
        model_mapping: '{"gpt-6-astra":"manual-deployment","custom":"keep"}',
      },
      result,
      'models',
    );
    expect(next.models).toEqual(['custom']);
    expect(JSON.parse(next.model_mapping)).toEqual({
      'gpt-6-astra': 'manual-deployment',
      custom: 'keep',
      'gpt-6-sol': 'dep-sol',
      'gpt-6.1-sol': 'sol61',
    });
    expect(getAzureDiscoveryModelConfig(next, result).models).toContain(
      'dep-astra',
    );
  });

  test('keeps ambiguous deployments until an existing mapping selects one', () => {
    const duplicate = {
      model_source: 'deployments',
      models: ['a', 'b'],
      deployments: [
        { id: 'a', model: 'gpt-6-sol' },
        { id: 'b', model: 'gpt-6-sol' },
      ],
    };
    expect(getAzureDiscoveryModelConfig({}, duplicate)).toEqual({
      models: ['a', 'b'],
      model_mapping: '',
    });
    expect(
      getAzureDiscoveryModelConfig(
        { model_mapping: '{"gpt-6-sol":"b"}' },
        duplicate,
      ).models,
    ).toEqual(['a', 'gpt-6-sol']);
  });

  test('does not overwrite invalid mapping JSON or invent metadata', () => {
    for (const model_mapping of ['{', 'null', '[]']) {
      expect(getAzureDiscoveryModelConfig({ model_mapping }, result)).toEqual({
        models: result.models,
        model_mapping,
      });
    }
    expect(
      getAzureDiscoveryModelConfig({}, { ...result, deployments: [] }).models,
    ).toEqual(result.models);
  });

  test('catalogs, errors and region queries never add redirects', () => {
    for (const response of [
      { ...result, model_source: 'catalog' },
      { ...result, model_error: '503' },
    ]) {
      expect(
        applyAzureDiscoveryResult(
          { models: [], model_mapping: '' },
          response,
          'all',
        ).model_mapping,
      ).toBe('');
    }
    expect(
      applyAzureDiscoveryResult(
        { models: [], model_mapping: '' },
        result,
        'region',
      ).model_mapping,
    ).toBe('');
  });

  test('avoids redirecting another deployment ID or adding self redirects', () => {
    const collision = {
      model_source: 'deployments',
      models: ['model', 'other', 'same'],
      deployments: [
        { id: 'other', model: 'model' },
        { id: 'same', model: 'same' },
      ],
    };
    expect(getAzureDiscoveryModelConfig({}, collision)).toEqual({
      models: collision.models,
      model_mapping: '',
    });
  });
});
