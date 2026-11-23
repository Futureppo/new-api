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

import { useEffect, useMemo, useRef, useState } from 'react';
import { API } from '../../helpers/api';
import {
  azureConnectionDraft,
  createAzureDiscoveryRunner,
} from './azureDiscovery';

export const useAzureDiscovery = ({
  visible,
  channelId,
  inputs,
  savedDraft,
  canOverride,
  onResult,
}) => {
  const session = `${visible}:${channelId ?? 'new'}:${inputs.type}`;
  const [credential, setCredential] = useState({ session, value: '' });
  const managementToken =
    credential.session === session ? credential.value : '';
  const [result, setResult] = useState(null);
  const [busy, setBusy] = useState('');
  const draftJSON = JSON.stringify(azureConnectionDraft(inputs));
  const ready = channelId === undefined || savedDraft?.channelId === channelId;
  const draftChanged =
    ready && channelId !== undefined && draftJSON !== savedDraft?.json;
  const permitted = canOverride || (channelId !== undefined && !draftChanged);
  const enabled =
    visible &&
    inputs.type === 3 &&
    ready &&
    permitted &&
    !!inputs.base_url?.trim() &&
    (!!inputs.key?.trim() ||
      channelId !== undefined ||
      (!!managementToken.trim() && !!inputs.azure_resource_id?.trim()));
  const snapshot = useMemo(
    () => ({
      draft: JSON.parse(draftJSON),
      channelId,
      canOverride,
      // A half-filled optional authorization block does not interrupt key-based discovery.
      managementToken: JSON.parse(draftJSON).resource_id.trim()
        ? managementToken
        : '',
    }),
    [draftJSON, channelId, canOverride, managementToken],
  );
  const latest = useRef();
  latest.current = { onResult, snapshot, enabled };
  const runner = useMemo(
    () =>
      createAzureDiscoveryRunner({
        request: async (current, target, signal) => {
          const config = {
            signal,
            skipErrorHandler: true,
            disableDuplicate: true,
          };
          const response = current.canOverride
            ? await API.post(
                '/api/channel/azure/discover',
                {
                  ...current.draft,
                  channel_id: current.channelId,
                  management_token: current.managementToken,
                  target,
                },
                config,
              )
            : await API.get(
                `/api/channel/azure/discover/${current.channelId}`,
                {
                  ...config,
                  params: { target },
                },
              );
          if (!response?.data?.success)
            throw new Error(
              response?.data?.message || 'Azure discovery failed',
            );
          return response.data.data;
        },
        onResult: (data, target, current) => {
          if (!latest.current.enabled || latest.current.snapshot !== current)
            return;
          setResult((previous) => ({
            ...previous,
            ...(target !== 'region'
              ? {
                  models: data.models,
                  model_source: data.model_source,
                  model_error: data.model_error,
                }
              : {}),
            ...(target !== 'models' ? { region_error: data.region_error } : {}),
            management_fallback: data.management_fallback,
          }));
          latest.current.onResult(data, target);
        },
        onError: (error, target) =>
          setResult((previous) => ({
            ...previous,
            ...(target !== 'region'
              ? { model_error: error?.response?.data?.message || error.message }
              : {}),
            ...(target !== 'models'
              ? {
                  region_error: error?.response?.data?.message || error.message,
                }
              : {}),
          })),
        onLoading: setBusy,
      }),
    [],
  );

  useEffect(() => {
    setCredential({ session, value: '' });
    setResult(null);
    return () => runner.cancel();
  }, [session, runner]);

  useEffect(() => {
    setResult(null);
    if (enabled) runner.schedule(snapshot);
    else {
      runner.cancel();
      setBusy('');
    }
    return () => runner.cancel();
  }, [enabled, snapshot, runner]);

  return {
    result,
    busy,
    enabled,
    permitted,
    managementToken,
    setManagementToken: (value) => setCredential({ session, value }),
    discover: async (target = 'all') => {
      if (!enabled) return null;
      const data = await runner.run(snapshot, target);
      return latest.current.enabled && latest.current.snapshot === snapshot
        ? data
        : null;
    },
  };
};
