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

import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { API, processModelsData, processGroupsData } from '../../helpers';
import { API_ENDPOINTS } from '../../constants/playground.constants';

export const useDataLoader = (
  userState,
  inputs,
  handleInputChange,
  setModels,
  setGroups,
) => {
  const { t } = useTranslation();
  const modelRequestIdRef = useRef(0);
  const inputsRef = useRef(inputs);
  const [groupsLoaded, setGroupsLoaded] = useState(false);

  useEffect(() => {
    inputsRef.current = inputs;
  }, [inputs]);

  const loadModels = useCallback(
    async (group = inputsRef.current.group) => {
      const requestId = ++modelRequestIdRef.current;
      try {
        const res = await API.get(API_ENDPOINTS.USER_MODELS, {
          params: group ? { group } : undefined,
        });
        const { success, message, data } = res.data;

        if (requestId !== modelRequestIdRef.current) return;

        const currentModel = inputsRef.current.model;
        if (success) {
          const { modelOptions, selectedModel } = processModelsData(
            data,
            currentModel,
          );
          setModels(modelOptions);

          if (selectedModel !== currentModel) {
            handleInputChange('model', selectedModel || '');
          }
        } else {
          showError(t(message));
        }
      } catch (error) {
        if (requestId === modelRequestIdRef.current) {
          showError(t('加载模型失败'));
        }
      }
    },
    [handleInputChange, setModels, t],
  );

  const loadGroups = useCallback(async () => {
    try {
      const res = await API.get(API_ENDPOINTS.USER_GROUPS);
      const { success, message, data } = res.data;

      if (success) {
        const userGroup =
          userState?.user?.group ||
          JSON.parse(localStorage.getItem('user'))?.group;
        const groupOptions = processGroupsData(data, userGroup);
        setGroups(groupOptions);

        const currentGroup = inputsRef.current.group;
        const hasCurrentGroup = groupOptions.some(
          (option) => option.value === currentGroup,
        );
        const selectedGroup = hasCurrentGroup
          ? currentGroup
          : groupOptions[0]?.value || '';
        if (selectedGroup !== currentGroup) {
          handleInputChange('group', selectedGroup);
        }
        setGroupsLoaded(true);
        return selectedGroup;
      } else {
        showError(t(message));
      }
    } catch (error) {
      showError(t('加载分组失败'));
    }
  }, [userState, handleInputChange, setGroups, t]);

  // 用户加载完成后先确定有效分组
  useEffect(() => {
    if (userState?.user) {
      setGroupsLoaded(false);
      loadGroups();
    }
  }, [userState?.user, loadGroups]);

  // 分组确定或切换后加载对应模型
  useEffect(() => {
    if (userState?.user && groupsLoaded) {
      loadModels(inputs.group);
    }
  }, [userState?.user, groupsLoaded, inputs.group, loadModels]);

  return {
    loadModels,
    loadGroups,
  };
};
