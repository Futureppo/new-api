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

import HeaderBar from './headerbar';
import { Layout } from '@douyinfe/semi-ui';
import SiderBar from './SiderBar';
import App from '../../App';
import FooterBar from './Footer';
import { ToastContainer } from 'react-toastify';
import ErrorBoundary from '../common/ErrorBoundary';
import React, { useContext, useEffect, useState } from 'react';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import { useSidebarCollapsed } from '../../hooks/common/useSidebarCollapsed';
import { useTranslation } from 'react-i18next';
import {
  API,
  getLogo,
  getSystemName,
  showError,
  setStatusData,
} from '../../helpers';
import { UserContext } from '../../context/User';
import { StatusContext } from '../../context/Status';
import { useLocation } from 'react-router-dom';
import { normalizeLanguage } from '../../i18n/language';
import SiteBackground from './SiteBackground';
import {
  darkVariantFilterId,
  SITE_BACKGROUND_GLASS_FILTER_ID,
} from './SiteBackgroundGlassFilter';
import { normalizeSiteBackgroundConfig } from '../../services/siteBackground';
const { Sider, Content, Header } = Layout;

const PageLayout = () => {
  const [userState, userDispatch] = useContext(UserContext);
  const [statusState, statusDispatch] = useContext(StatusContext);
  const isMobile = useIsMobile();
  const [collapsed, , setCollapsed] = useSidebarCollapsed();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const { i18n } = useTranslation();
  const location = useLocation();
  const siteBackgroundConfig = normalizeSiteBackgroundConfig(
    statusState?.status?.site_background,
  );
  const siteBackgroundActive = siteBackgroundConfig.enabled;
  const siteBackgroundGlassActive =
    siteBackgroundActive && siteBackgroundConfig.glass_enabled;
  const appLayoutClassName = [
    'app-layout',
    siteBackgroundActive ? 'site-background-active' : '',
    siteBackgroundGlassActive ? 'site-background-glass' : '',
  ]
    .filter(Boolean)
    .join(' ');
  const siteBackgroundGlassStyle = siteBackgroundGlassActive
    ? {
        '--site-background-glass-opacity': `${siteBackgroundConfig.glass_opacity}%`,
        // 明暗两套滤镜由 CSS 按主题挑选；折射为 0 时两个变量都不下发，
        // CSS 侧回落到无折射写法。
        ...(siteBackgroundConfig.glass_refraction > 0
          ? {
              '--site-background-glass-refract-light': `url(#${SITE_BACKGROUND_GLASS_FILTER_ID})`,
              '--site-background-glass-refract-dark': `url(#${darkVariantFilterId(
                SITE_BACKGROUND_GLASS_FILTER_ID,
              )})`,
            }
          : {}),
      }
    : {};

  const cardProPages = [
    '/console/channel',
    '/console/log',
    '/console/redemption',
    '/console/user',
    '/console/token',
    '/console/midjourney',
    '/console/task',
    '/console/models',
    '/console/enhancements',
    '/pricing',
  ];

  const shouldHideFooter =
    cardProPages.includes(location.pathname) ||
    location.pathname.startsWith('/console/enhancements');

  const shouldInnerPadding =
    location.pathname.includes('/console') &&
    !location.pathname.startsWith('/console/chat') &&
    location.pathname !== '/console/playground';

  const isConsoleRoute = location.pathname.startsWith('/console');
  const showSider = isConsoleRoute && (!isMobile || drawerOpen);

  useEffect(() => {
    if (isMobile && drawerOpen && collapsed) {
      setCollapsed(false);
    }
  }, [isMobile, drawerOpen, collapsed, setCollapsed]);

  const loadUser = () => {
    let user = localStorage.getItem('user');
    if (user) {
      let data = JSON.parse(user);
      userDispatch({ type: 'login', payload: data });
    }
  };

  const loadStatus = async () => {
    try {
      const res = await API.get('/api/status');
      const { success, data } = res.data;
      if (success) {
        statusDispatch({ type: 'set', payload: data });
        setStatusData(data);
      } else {
        showError('Unable to connect to server');
      }
    } catch (error) {
      showError('Failed to load status');
    }
  };

  useEffect(() => {
    loadUser();
    loadStatus().catch(console.error);
    let systemName = getSystemName();
    if (systemName) {
      document.title = systemName;
    }
    let logo = getLogo();
    if (logo) {
      let linkElement = document.querySelector("link[rel~='icon']");
      if (linkElement) {
        linkElement.href = logo;
      }
    }
  }, []);

  useEffect(() => {
    let preferredLang;

    if (userState?.user?.setting) {
      try {
        const settings = JSON.parse(userState.user.setting);
        preferredLang = normalizeLanguage(settings.language);
      } catch (e) {
        // Ignore parse errors
      }
    }

    if (!preferredLang) {
      const savedLang = localStorage.getItem('i18nextLng');
      if (savedLang) {
        preferredLang = normalizeLanguage(savedLang);
      }
    }

    if (preferredLang) {
      localStorage.setItem('i18nextLng', preferredLang);
      if (preferredLang !== i18n.language) {
        i18n.changeLanguage(preferredLang);
      }
    }
  }, [i18n, userState?.user?.setting]);

  if (location.pathname === '/model-status') {
    return (
      <>
        <SiteBackground config={siteBackgroundConfig} />
        <Layout
          className={appLayoutClassName}
          style={{ minHeight: '100vh', ...siteBackgroundGlassStyle }}
        >
          <Content
            className='site-background-content'
            style={{ minHeight: '100vh', overflow: 'auto' }}
          >
            <ErrorBoundary>
              <App />
            </ErrorBoundary>
          </Content>
          <ToastContainer />
        </Layout>
      </>
    );
  }

  return (
    <>
      <SiteBackground config={siteBackgroundConfig} />
      <Layout
        className={appLayoutClassName}
        style={{
          display: 'flex',
          flexDirection: 'column',
          overflow: isMobile ? 'visible' : 'hidden',
          ...siteBackgroundGlassStyle,
        }}
      >
        <Header
          style={{
            padding: 0,
            height: 'auto',
            lineHeight: 'normal',
            position: 'fixed',
            width: '100%',
            top: 0,
            zIndex: 100,
          }}
        >
          <HeaderBar
            onMobileMenuToggle={() => setDrawerOpen((prev) => !prev)}
            drawerOpen={drawerOpen}
          />
        </Header>
        <Layout
          className='site-background-shell'
          style={{
            overflow: isMobile ? 'visible' : 'auto',
            display: 'flex',
            flexDirection: 'column',
          }}
        >
          {showSider && (
            <Sider
              className='app-sider'
              style={{
                position: 'fixed',
                left: 0,
                top: '64px',
                zIndex: 99,
                border: 'none',
                paddingRight: '0',
                width: 'var(--sidebar-current-width)',
              }}
            >
              <SiderBar
                onNavigate={() => {
                  if (isMobile) setDrawerOpen(false);
                }}
              />
            </Sider>
          )}
          <Layout
            className='site-background-main'
            style={{
              marginLeft: isMobile
                ? '0'
                : showSider
                  ? 'var(--sidebar-current-width)'
                  : '0',
              flex: '1 1 auto',
              display: 'flex',
              flexDirection: 'column',
            }}
          >
            <Content
              className='site-background-content'
              style={{
                flex: '1 0 auto',
                overflowY: isMobile ? 'visible' : 'hidden',
                WebkitOverflowScrolling: 'touch',
                padding: shouldInnerPadding ? (isMobile ? '5px' : '24px') : '0',
                position: 'relative',
              }}
            >
              <ErrorBoundary>
                <App />
              </ErrorBoundary>
            </Content>
            {!shouldHideFooter && (
              <Layout.Footer
                style={{
                  flex: '0 0 auto',
                  width: '100%',
                }}
              >
                <FooterBar />
              </Layout.Footer>
            )}
          </Layout>
        </Layout>
        <ToastContainer />
      </Layout>
    </>
  );
};

export default PageLayout;
