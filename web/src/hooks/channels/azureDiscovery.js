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

export const applyAzureDiscoveryResult = (inputs, result, target) => {
  const next = { ...inputs };
  if (
    target !== 'region' &&
    result.model_source === 'deployments' &&
    !result.model_error &&
    !inputs.models?.length
  ) {
    next.models = result.models || [];
  }
  if (target !== 'models' && !result.region_error) {
    next.azure_region = result.region || '';
    next.azure_region_source = result.region_source || '';
    let settings;
    try {
      settings = JSON.parse(inputs.settings || '{}');
    } catch {
      settings = {};
    }
    next.settings = JSON.stringify({
      ...settings,
      azure_region: next.azure_region,
      azure_region_source: next.azure_region_source,
    });
  }
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
