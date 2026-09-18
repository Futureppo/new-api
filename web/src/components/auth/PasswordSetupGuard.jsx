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

import React, { useContext, useEffect, useState } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { Button, Spin, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { UserContext } from '../../context/User';
import { API, updateAPI } from '../../helpers/api';
import { setUserData } from '../../helpers/data';

// Do not mount console children until the server has checked the current user.
// Stored user data is only a session hint, never proof that a password is set.
export default function PasswordSetupGuard({ children }) {
  const [userState, dispatch] = useContext(UserContext);
  const { t } = useTranslation();
  const location = useLocation();
  let storedUser;
  try {
    storedUser = JSON.parse(localStorage.getItem('user'));
  } catch {
    localStorage.removeItem('user');
  }
  const userId = userState.user?.id || storedUser?.id;
  const [checkedId, setCheckedId] = useState(null);
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);

  useEffect(() => {
    const onRequired = () => {
      let user;
      try {
        user = JSON.parse(localStorage.getItem('user'));
      } catch {
        return;
      }
      if (user) {
        const data = { ...user, has_password: false };
        setUserData(data);
        dispatch({ type: 'login', payload: data });
      }
    };
    window.addEventListener('password-setup-required', onRequired);
    return () =>
      window.removeEventListener('password-setup-required', onRequired);
  }, [dispatch]);

  useEffect(() => {
    if (!userId) {
      setCheckedId(null);
      return;
    }
    let cancelled = false;
    setError('');
    updateAPI();
    API.get('/api/user/self')
      .then(({ data: result }) => {
        if (cancelled) return;
        if (!result.success || typeof result.data?.has_password !== 'boolean') {
          throw new Error(result.message || t('无法确认密码状态，请重试'));
        }
        setUserData(result.data);
        dispatch({ type: 'login', payload: result.data });
        setCheckedId(userId);
      })
      .catch((err) => {
        if (!cancelled) setError(err.response?.data?.message || err.message);
      });
    return () => {
      cancelled = true;
    };
  }, [userId, retry, dispatch]);

  const logout = async () => {
    await API.get('/api/user/logout');
    localStorage.removeItem('user');
    dispatch({ type: 'logout' });
    window.location.assign('/login');
  };

  if (userId && checkedId !== userId) {
    return (
      <div className='min-h-screen flex flex-col items-center justify-center gap-4 p-6'>
        {error ? (
          <>
            <Typography.Text type='danger'>{error}</Typography.Text>
            <Button onClick={() => setRetry((value) => value + 1)}>
              {t('重试')}
            </Button>
            <Button onClick={logout}>{t('退出登录')}</Button>
          </>
        ) : (
          <Spin size='large' />
        )}
      </div>
    );
  }
  if (
    userId &&
    userState.user?.has_password === false &&
    location.pathname !== '/console/personal'
  ) {
    return <Navigate to='/console/personal' replace />;
  }
  return children;
}
