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

export const azureConnectionDraft = (inputs) => ({
  base_url: inputs.base_url || '',
  key: inputs.key || '',
  other: inputs.other || '',
  proxy: inputs.proxy || '',
  header_override: inputs.header_override || '',
  resource_id: inputs.azure_resource_id || '',
});

export const getAzureDiscoveryModelConfig = (inputs, result) => {
  const models = result?.models || [];
  const unchanged = { models, model_mapping: inputs.model_mapping || '' };
  if (
    inputs.azure_auto_model_mapping === false ||
    result?.model_source !== 'deployments' ||
    result.model_error
  )
    return unchanged;
  let mapping;
  try {
    mapping = JSON.parse(inputs.model_mapping || '{}');
  } catch {
    return unchanged;
  }
  if (!mapping || typeof mapping !== 'object' || Array.isArray(mapping))
    return unchanged;

  const deploymentIDs = new Set(models);
  const byModel = new Map();
  for (const deployment of result.deployments || []) {
    const name = deployment.model?.trim();
    if (!name || !deploymentIDs.has(deployment.id)) continue;
    if (!byModel.has(name)) byModel.set(name, new Set());
    byModel.get(name).add(deployment.id);
  }
  const aliases = new Map();
  let changed = false;
  for (const [name, ids] of byModel) {
    const existing = Object.prototype.hasOwnProperty.call(mapping, name);
    // Keep manual routes. Multiple deployments need an explicit selection;
    // otherwise expose their IDs so every deployment remains selectable.
    const id = existing ? mapping[name] : ids.size === 1 ? [...ids][0] : null;
    if (!ids.has(id) || (name !== id && deploymentIDs.has(name))) continue;
    aliases.set(id, name);
    if (!existing && name !== id) {
      mapping = { ...mapping, [name]: id };
      changed = true;
    }
  }
  return {
    models: [...new Set(models.map((id) => aliases.get(id) || id))],
    model_mapping: changed
      ? JSON.stringify(mapping, null, 2)
      : unchanged.model_mapping,
  };
};

export const applyAzureDiscoveryResult = (inputs, result, target) => {
  const next = { ...inputs };
  let settings;
  try {
    settings = JSON.parse(inputs.settings || '{}');
  } catch {
    settings = {};
  }
  if (
    target !== 'region' &&
    result.model_source === 'deployments' &&
    !result.model_error
  ) {
    const config = getAzureDiscoveryModelConfig(inputs, result);
    if (!inputs.models?.length) next.models = config.models;
    next.model_mapping = config.model_mapping;
    settings.azure_deployment_models = Object.fromEntries(
      (result.deployments || [])
        .filter(({ id, model }) => id && model)
        .map(({ id, model }) => [id, model]),
    );
  }
  if (target !== 'models' && !result.region_error) {
    next.azure_region = result.region || '';
    next.azure_region_source = result.region_source || '';
    settings = {
      ...settings,
      azure_region: next.azure_region,
      azure_region_source: next.azure_region_source,
    };
  }
  next.settings = JSON.stringify(settings);
  return next;
};

// Cancellation plus a generation check also handles transports that ignore abort.
export const createAzureDiscoveryRunner = ({
  request,
  onResult,
  onError,
  onLoading,
  delay = 600,
}) => {
  let generation = 0;
  let timer;
  let controller;
  const cancel = () => {
    generation += 1;
    clearTimeout(timer);
    controller?.abort();
    controller = undefined;
  };
  const run = async (snapshot, target = 'all') => {
    cancel();
    const current = generation;
    controller = new AbortController();
    onLoading(target);
    try {
      const result = await request(snapshot, target, controller.signal);
      if (current !== generation) return null;
      onResult(result, target, snapshot);
      return result;
    } catch (error) {
      if (current === generation) onError(error, target);
      return null;
    } finally {
      if (current === generation) onLoading('');
    }
  };
  return {
    cancel,
    run,
    schedule(snapshot) {
      cancel();
      onLoading('');
      timer = setTimeout(() => {
        void run(snapshot);
      }, delay);
    },
  };
};
