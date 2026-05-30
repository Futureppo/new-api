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

import React from 'react';
import { Button, Modal } from '@douyinfe/semi-ui';
import { Trash2, UserPlus } from 'lucide-react';

const UsersActions = ({
  setShowAddUser,
  purgeSoftDeletedUsers,
  purgingSoftDeletedUsers,
  t,
}) => {
  // Add new user
  const handleAddUser = () => {
    setShowAddUser(true);
  };

  const handlePurgeSoftDeletedUsers = () => {
    Modal.confirm({
      title: t('确定清理所有已注销用户？'),
      content: t('将从数据库永久删除已注销用户，此操作不可撤销。'),
      okText: t('确认清理'),
      cancelText: t('取消'),
      type: 'danger',
      okButtonProps: { type: 'danger', loading: purgingSoftDeletedUsers },
      onOk: purgeSoftDeletedUsers,
    });
  };

  return (
    <div className='flex flex-wrap gap-2 w-full md:w-auto order-2 md:order-1'>
      <Button
        className='flex-1 md:flex-initial'
        icon={<UserPlus size={14} />}
        onClick={handleAddUser}
        size='small'
      >
        {t('添加用户')}
      </Button>
      <Button
        className='flex-1 md:flex-initial'
        icon={<Trash2 size={14} />}
        loading={purgingSoftDeletedUsers}
        onClick={handlePurgeSoftDeletedUsers}
        size='small'
        type='danger'
      >
        {t('一键清理已注销用户')}
      </Button>
    </div>
  );
};

export default UsersActions;
