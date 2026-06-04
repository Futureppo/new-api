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

import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import { ITEMS_PER_PAGE } from '../../constants';
import { useTableCompactMode } from '../common/useTableCompactMode';

export const useUsersData = () => {
  const { t } = useTranslation();
  const [compactMode, setCompactMode] = useTableCompactMode('users');

  // State management
  const [users, setUsers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(ITEMS_PER_PAGE);
  const [searching, setSearching] = useState(false);
  const [groupOptions, setGroupOptions] = useState([]);
  const [userCount, setUserCount] = useState(0);
  const [purgingSoftDeletedUsers, setPurgingSoftDeletedUsers] = useState(false);

  // Modal states
  const [showAddUser, setShowAddUser] = useState(false);
  const [showEditUser, setShowEditUser] = useState(false);
  const [editingUser, setEditingUser] = useState({
    id: undefined,
  });

  // Form initial values
  const formInitValues = {
    searchKeyword: '',
    searchGroup: '',
    statusFilter: '',
    quotaOrder: '',
  };

  // Form API reference
  const [formApi, setFormApi] = useState(null);

  // Get form values helper function
  const getFormValues = () => {
    const formValues = formApi ? formApi.getValues() : {};
    return {
      searchKeyword: formValues.searchKeyword || '',
      searchGroup: formValues.searchGroup || '',
      statusFilter: formValues.statusFilter || '',
      quotaOrder: formValues.quotaOrder || '',
    };
  };

  const hasActiveFilters = ({
    searchKeyword,
    searchGroup,
    statusFilter,
    quotaOrder,
  }) => {
    return (
      searchKeyword !== '' ||
      searchGroup !== '' ||
      statusFilter !== '' ||
      quotaOrder !== ''
    );
  };

  const buildUsersQueryParams = (page, pageSize, filters = {}) => {
    const params = new URLSearchParams();
    params.set('p', page);
    params.set('page_size', pageSize);
    if (filters.searchKeyword) {
      params.set('keyword', filters.searchKeyword);
    }
    if (filters.searchGroup) {
      params.set('group', filters.searchGroup);
    }
    if (filters.statusFilter) {
      params.set('status', filters.statusFilter);
    }
    if (filters.quotaOrder) {
      params.set('quota_order', filters.quotaOrder);
    }
    return params.toString();
  };

  // Set user format with key field
  const setUserFormat = (users) => {
    for (let i = 0; i < users.length; i++) {
      users[i].key = users[i].id;
    }
    setUsers(users);
  };

  // Load users data
  const loadUsers = async (startIdx, pageSize, filters = {}) => {
    setLoading(true);
    const query = buildUsersQueryParams(startIdx, pageSize, filters);
    const res = await API.get(`/api/user/?${query}`);
    const { success, message, data } = res.data;
    if (success) {
      const newPageData = data.items;
      setActivePage(data.page);
      setUserCount(data.total);
      setUserFormat(newPageData);
    } else {
      showError(message);
    }
    setLoading(false);
  };

  // Search users with keyword and group
  const searchUsers = async (
    startIdx,
    pageSize,
    searchKeyword = null,
    searchGroup = null,
    statusFilter = null,
    quotaOrder = null,
  ) => {
    // If no parameters passed, get values from form
    if (
      searchKeyword === null ||
      searchGroup === null ||
      statusFilter === null ||
      quotaOrder === null
    ) {
      const formValues = getFormValues();
      searchKeyword = formValues.searchKeyword;
      searchGroup = formValues.searchGroup;
      statusFilter = formValues.statusFilter;
      quotaOrder = formValues.quotaOrder;
    }

    const filters = {
      searchKeyword,
      searchGroup,
      statusFilter,
      quotaOrder,
    };

    if (!hasActiveFilters(filters)) {
      await loadUsers(startIdx, pageSize);
      return;
    }
    setSearching(true);
    const query = buildUsersQueryParams(startIdx, pageSize, filters);
    const res = await API.get(`/api/user/search?${query}`);
    const { success, message, data } = res.data;
    if (success) {
      const newPageData = data.items;
      setActivePage(data.page);
      setUserCount(data.total);
      setUserFormat(newPageData);
    } else {
      showError(message);
    }
    setSearching(false);
  };

  // Manage user operations (promote, demote, enable, disable, delete)
  const manageUser = async (userId, action, record, reason) => {
    // Trigger loading state to force table re-render
    setLoading(true);

    const payload = {
      id: userId,
      action,
    };
    if (action === 'disable') {
      payload.reason = reason || '';
    }

    const res = await API.post('/api/user/manage', payload);

    const { success, message } = res.data;
    if (success) {
      showSuccess(t('操作成功完成！'));
      const user = res.data.data;

      // Create a new array and new object to ensure React detects changes
      const newUsers = users.map((u) => {
        if (u.id === userId) {
          if (action === 'delete') {
            return { ...u, DeletedAt: new Date() };
          }
          return {
            ...u,
            status: user.status,
            role: user.role,
            disable_reason:
              action === 'enable'
                ? ''
                : user.disable_reason || reason || u.disable_reason,
          };
        }
        return u;
      });

      setUsers(newUsers);
    } else {
      showError(message);
    }

    setLoading(false);
  };

  const resetUserPasskey = async (user) => {
    if (!user) {
      return;
    }
    try {
      const res = await API.delete(`/api/user/${user.id}/reset_passkey`);
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('Passkey 已重置'));
      } else {
        showError(message || t('操作失败，请重试'));
      }
    } catch (error) {
      showError(t('操作失败，请重试'));
    }
  };

  const resetUserTwoFA = async (user) => {
    if (!user) {
      return;
    }
    try {
      const res = await API.delete(`/api/user/${user.id}/2fa`);
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('二步验证已重置'));
      } else {
        showError(message || t('操作失败，请重试'));
      }
    } catch (error) {
      showError(t('操作失败，请重试'));
    }
  };

  // Handle page change
  const handlePageChange = (page) => {
    setActivePage(page);
    const filters = getFormValues();
    if (!hasActiveFilters(filters)) {
      loadUsers(page, pageSize).then();
    } else {
      searchUsers(
        page,
        pageSize,
        filters.searchKeyword,
        filters.searchGroup,
        filters.statusFilter,
        filters.quotaOrder,
      ).then();
    }
  };

  // Handle page size change
  const handlePageSizeChange = async (size) => {
    localStorage.setItem('page-size', size + '');
    setPageSize(size);
    setActivePage(1);
    const filters = getFormValues();
    const request = hasActiveFilters(filters)
      ? searchUsers(
          1,
          size,
          filters.searchKeyword,
          filters.searchGroup,
          filters.statusFilter,
          filters.quotaOrder,
        )
      : loadUsers(1, size);
    request.then().catch((reason) => {
      showError(reason);
    });
  };

  // Handle table row styling for disabled/deleted users
  const handleRow = (record, index) => {
    if (record.DeletedAt !== null || record.status !== 1) {
      return {
        style: {
          background: 'var(--semi-color-disabled-border)',
        },
      };
    } else {
      return {};
    }
  };

  // Refresh data
  const refresh = async (page = activePage) => {
    const filters = getFormValues();
    if (!hasActiveFilters(filters)) {
      await loadUsers(page, pageSize);
    } else {
      await searchUsers(
        page,
        pageSize,
        filters.searchKeyword,
        filters.searchGroup,
        filters.statusFilter,
        filters.quotaOrder,
      );
    }
  };

  const purgeSoftDeletedUsers = async () => {
    if (purgingSoftDeletedUsers) {
      return;
    }
    setPurgingSoftDeletedUsers(true);
    setLoading(true);
    try {
      const res = await API.post('/api/user/soft-deleted/purge');
      const { success, message, data } = res.data;
      if (success) {
        const count = Number(data?.deleted || 0);
        if (count > 0) {
          showSuccess(t('已清理 {{count}} 个已注销用户', { count }));
        } else {
          showSuccess(t('没有需要清理的已注销用户'));
        }
        await refresh(1);
      } else {
        showError(message);
      }
    } catch (error) {
      showError(error.message || error);
    } finally {
      setPurgingSoftDeletedUsers(false);
      setLoading(false);
    }
  };

  // Fetch groups data
  const fetchGroups = async () => {
    try {
      let res = await API.get(`/api/group/`);
      if (res === undefined) {
        return;
      }
      setGroupOptions(
        res.data.data.map((group) => ({
          label: group,
          value: group,
        })),
      );
    } catch (error) {
      showError(error.message);
    }
  };

  // Modal control functions
  const closeAddUser = () => {
    setShowAddUser(false);
  };

  const closeEditUser = () => {
    setShowEditUser(false);
    setEditingUser({
      id: undefined,
    });
  };

  // Initialize data on component mount
  useEffect(() => {
    loadUsers(0, pageSize)
      .then()
      .catch((reason) => {
        showError(reason);
      });
    fetchGroups().then();
  }, []);

  return {
    // Data state
    users,
    loading,
    activePage,
    pageSize,
    userCount,
    searching,
    groupOptions,
    purgingSoftDeletedUsers,

    // Modal state
    showAddUser,
    showEditUser,
    editingUser,
    setShowAddUser,
    setShowEditUser,
    setEditingUser,

    // Form state
    formInitValues,
    formApi,
    setFormApi,

    // UI state
    compactMode,
    setCompactMode,

    // Actions
    loadUsers,
    searchUsers,
    manageUser,
    purgeSoftDeletedUsers,
    resetUserPasskey,
    resetUserTwoFA,
    handlePageChange,
    handlePageSizeChange,
    handleRow,
    refresh,
    closeAddUser,
    closeEditUser,
    getFormValues,

    // Translation
    t,
  };
};
