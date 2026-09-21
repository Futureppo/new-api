export function buildNodeLocAuthorizationURL(status, origin, state) {
  let callback;
  try {
    callback = new URL(status.nodeloc_redirect_uri);
  } catch {
    throw new Error('NodeLoc 登录配置不完整，请联系管理员');
  }
  if (
    !status.nodeloc_client_id ||
    !['http:', 'https:'].includes(callback.protocol) ||
    callback.pathname !== '/oauth/nodeloc' ||
    callback.search ||
    callback.hash ||
    callback.username ||
    callback.password
  ) {
    throw new Error('NodeLoc 登录配置不完整，请联系管理员');
  }
  if (callback.origin !== origin) {
    const error = new Error('请在配置的正式站点使用 NodeLoc 登录：{{origin}}');
    error.origin = callback.origin;
    throw error;
  }
  const url = new URL('https://www.nodeloc.com/oauth-provider/authorize');
  url.searchParams.set('client_id', status.nodeloc_client_id);
  url.searchParams.set('redirect_uri', status.nodeloc_redirect_uri);
  url.searchParams.set('response_type', 'code');
  url.searchParams.set('scope', 'openid profile');
  url.searchParams.set('state', state);
  return url;
}
