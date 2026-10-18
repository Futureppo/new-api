import { describe, expect, test } from 'bun:test';
import * as formObject from '@douyinfe/semi-foundation/lib/es/utils/object';
import {
  getNodeLocSettingsUpdates,
  nodeLocFormFields,
} from './nodelocSettings';

describe('NodeLoc settings form', () => {
  test('keeps typed credentials as literal option keys through Semi Form and JSON', () => {
    const values = {};
    formObject.set(values, nodeLocFormFields.clientId, ' client-id ');
    formObject.set(values, nodeLocFormFields.clientSecret, ' client-secret ');
    expect(values.nodeloc).toBeUndefined();
    const requests = JSON.parse(
      JSON.stringify(getNodeLocSettingsUpdates(values, {})),
    );
    expect(requests).toEqual([
      { key: 'nodeloc.client_id', value: 'client-id' },
      { key: 'nodeloc.client_secret', value: 'client-secret' },
    ]);
  });

  test('clears the secret after saving without resubmitting or erasing it', () => {
    const saved = { 'nodeloc.client_id': 'client-id' };
    const values = { ...saved };
    formObject.set(values, nodeLocFormFields.clientSecret, 'new-secret');
    expect(getNodeLocSettingsUpdates(values, saved)).toEqual([
      { key: 'nodeloc.client_secret', value: 'new-secret' },
    ]);
    formObject.set(values, nodeLocFormFields.clientSecret, '');
    expect(getNodeLocSettingsUpdates(values, saved)).toEqual([]);
    expect(formObject.get(values, nodeLocFormFields.clientId)).toBe(
      'client-id',
    );
    expect(values.nodeloc).toBeUndefined();
  });

  test('uses the same flat key to display, change and restore the login switch', () => {
    const values = { 'nodeloc.enabled': true };
    expect(formObject.get(values, nodeLocFormFields.enabled)).toBe(true);
    formObject.set(values, nodeLocFormFields.enabled, false);
    expect(values).toEqual({ 'nodeloc.enabled': false });
    formObject.set(values, nodeLocFormFields.enabled, true);
    expect(values).toEqual({ 'nodeloc.enabled': true });
  });

  test('replaces an ID corrupted by the old form and preserves an existing secret', () => {
    const saved = { 'nodeloc.client_id': '<nil>' };
    const values = { ...saved };
    formObject.set(values, nodeLocFormFields.clientId, 'repaired-id');
    expect(getNodeLocSettingsUpdates(values, saved)).toEqual([
      { key: 'nodeloc.client_id', value: 'repaired-id' },
    ]);
  });

  test('never serializes missing or non-string values as credential updates', () => {
    for (const value of [undefined, null, false, 12, {}]) {
      expect(
        getNodeLocSettingsUpdates(
          { 'nodeloc.client_id': value, 'nodeloc.client_secret': value },
          {},
        ),
      ).toEqual([]);
    }
  });
});
