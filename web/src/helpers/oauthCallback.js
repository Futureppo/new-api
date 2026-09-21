// Preserve provider errors and let the backend validate state before handling them.
export function getOAuthCallbackParams(searchParams) {
  const params = new URLSearchParams();
  for (const key of ['code', 'state', 'error', 'error_description']) {
    const value = searchParams.get(key);
    if (value !== null) params.set(key, value);
  }
  return params;
}
