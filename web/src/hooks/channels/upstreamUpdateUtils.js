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

export const normalizeModelList = (models = []) =>
  Array.from(
    new Set(
      (models || []).map((model) => String(model || '').trim()).filter(Boolean),
    ),
  );

// Collapse upstream IDs already represented by a Cline short-name mapping.
// Keep the original mapping and any separately mapped full ID intact.
export const collapseClineMappedModels = (models, modelMapping) => {
  const normalized = normalizeModelList(models);
  let mapping = modelMapping;
  if (typeof mapping === 'string') {
    try {
      mapping = JSON.parse(mapping);
    } catch {
      return normalized;
    }
  }
  if (!mapping || typeof mapping !== 'object' || Array.isArray(mapping)) {
    return normalized;
  }
  return normalizeModelList(
    normalized.map((id) => {
      const slash = id.indexOf('/');
      if (slash <= 0 || Object.hasOwn(mapping, id)) return id;
      const alias = id.slice(slash + 1).trim();
      return alias && mapping[alias] === id ? alias : id;
    }),
  );
};

export const parseUpstreamUpdateMeta = (settings, channelType) => {
  let parsed = null;
  if (settings && typeof settings === 'object') {
    parsed = settings;
  } else if (typeof settings === 'string') {
    try {
      parsed = JSON.parse(settings);
    } catch (error) {
      parsed = null;
    }
  }

  if (!parsed || typeof parsed !== 'object') {
    return {
      enabled: Number(channelType) === 73,
      pendingAddModels: [],
      pendingRemoveModels: [],
    };
  }

  return {
    enabled:
      (Number(channelType) === 63 &&
        parsed.opencode_auto_sync_free_models_enabled === true) ||
      (Number(channelType) === 73 &&
        parsed.cline_auto_sync_free_models_enabled !== false) ||
      parsed.upstream_model_update_check_enabled === true ||
      (Number(channelType) === 20 &&
        parsed.openrouter_auto_sync_free_and_alpha_models_enabled === true),
    pendingAddModels: normalizeModelList(
      parsed.upstream_model_update_last_detected_models,
    ),
    pendingRemoveModels: normalizeModelList(
      parsed.upstream_model_update_last_removed_models,
    ),
  };
};
