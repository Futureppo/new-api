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

export const SITE_BACKGROUND_OPTION_KEY = 'site_background.config';
export const SITE_BACKGROUND_MAX_SOURCES = 20;

export const SITE_BACKGROUND_SOURCE_TYPES = {
  IMAGE_URL: 'image_url',
  IMAGE_API: 'image_api',
  JSON_API: 'json_api',
};

export const SITE_BACKGROUND_FIT_MODES = ['cover', 'contain', 'fill'];

export const DEFAULT_SITE_BACKGROUND_CONFIG = Object.freeze({
  enabled: false,
  fit: 'cover',
  overlay_opacity: 25,
  sources: [],
});

const ALLOWED_SOURCE_TYPES = new Set(
  Object.values(SITE_BACKGROUND_SOURCE_TYPES),
);

const parseConfig = (value) => {
  if (!value) return {};
  if (typeof value === 'object') return value;
  try {
    return JSON.parse(value);
  } catch {
    return {};
  }
};

export const isAllowedSiteBackgroundURL = (value) => {
  const candidate = String(value || '').trim();
  if (!candidate) return false;
  if (candidate.startsWith('/') && !candidate.startsWith('//')) return true;

  try {
    const parsed = new URL(candidate);
    return (
      (parsed.protocol === 'http:' || parsed.protocol === 'https:') &&
      !parsed.username &&
      !parsed.password
    );
  } catch {
    return false;
  }
};

const normalizeSource = (source) => {
  if (!source || typeof source !== 'object') return null;

  const type = String(source.type || '').trim();
  const url = String(source.url || '').trim();
  const jsonPath = String(source.json_path || '').trim();
  if (!ALLOWED_SOURCE_TYPES.has(type) || !isAllowedSiteBackgroundURL(url)) {
    return null;
  }
  if (type !== SITE_BACKGROUND_SOURCE_TYPES.JSON_API && jsonPath) {
    return null;
  }

  return {
    type,
    url,
    ...(type === SITE_BACKGROUND_SOURCE_TYPES.JSON_API
      ? { json_path: jsonPath }
      : {}),
  };
};

export const normalizeSiteBackgroundConfig = (value) => {
  const parsed = parseConfig(value);
  const fit = SITE_BACKGROUND_FIT_MODES.includes(parsed.fit)
    ? parsed.fit
    : DEFAULT_SITE_BACKGROUND_CONFIG.fit;
  const parsedOpacity = Number(parsed.overlay_opacity);
  const overlayOpacity = Number.isFinite(parsedOpacity)
    ? Math.min(80, Math.max(0, Math.round(parsedOpacity)))
    : DEFAULT_SITE_BACKGROUND_CONFIG.overlay_opacity;
  const sources = Array.isArray(parsed.sources)
    ? parsed.sources
        .slice(0, SITE_BACKGROUND_MAX_SOURCES)
        .map(normalizeSource)
        .filter(Boolean)
    : [];

  return {
    enabled: parsed.enabled === true && sources.length > 0,
    fit,
    overlay_opacity: overlayOpacity,
    sources,
  };
};

const shuffled = (values) => {
  const result = [...values];
  for (let index = result.length - 1; index > 0; index -= 1) {
    const target = Math.floor(Math.random() * (index + 1));
    [result[index], result[target]] = [result[target], result[index]];
  }
  return result;
};

export const getJSONPathValue = (value, path) => {
  const trimmedPath = String(path || '').trim();
  if (!trimmedPath) return value;

  return trimmedPath.split('.').reduce((current, segment) => {
    if (current == null) return undefined;
    if (Array.isArray(current) && /^\d+$/.test(segment)) {
      return current[Number(segment)];
    }
    if (typeof current !== 'object') return undefined;
    return current[segment];
  }, value);
};

const toAbsoluteURL = (value, baseURL = window.location.href) => {
  const resolved = new URL(value, baseURL);
  if (resolved.protocol !== 'http:' && resolved.protocol !== 'https:') {
    throw new Error('Unsupported image URL protocol');
  }
  if (resolved.username || resolved.password) {
    throw new Error('Image URL credentials are not allowed');
  }
  return resolved.toString();
};

const appendCacheBuster = (value) => {
  const resolved = new URL(value, window.location.href);
  resolved.searchParams.set('_site_background', String(Date.now()));
  return resolved.toString();
};

const createRequestSignal = (parentSignal, timeoutMs) => {
  const controller = new AbortController();
  const abort = () => controller.abort();
  const timer = window.setTimeout(abort, timeoutMs);
  parentSignal?.addEventListener('abort', abort, { once: true });

  return {
    signal: controller.signal,
    cleanup: () => {
      window.clearTimeout(timer);
      parentSignal?.removeEventListener('abort', abort);
    },
  };
};

const fetchJSONImageURL = async (source, signal) => {
  const request = createRequestSignal(signal, 8000);
  try {
    const response = await fetch(source.url, {
      cache: 'no-store',
      credentials: 'omit',
      referrerPolicy: 'no-referrer',
      signal: request.signal,
    });
    if (!response.ok) {
      throw new Error(`Background API request failed: ${response.status}`);
    }

    const payload = await response.json();
    const value = getJSONPathValue(payload, source.json_path);
    if (typeof value !== 'string' || !value.trim()) {
      throw new Error('Background API response does not contain an image URL');
    }
    return toAbsoluteURL(value.trim(), toAbsoluteURL(source.url));
  } finally {
    request.cleanup();
  }
};

const resolveSourceURL = async (source, signal) => {
  switch (source.type) {
    case SITE_BACKGROUND_SOURCE_TYPES.IMAGE_API:
      return appendCacheBuster(source.url);
    case SITE_BACKGROUND_SOURCE_TYPES.JSON_API:
      return fetchJSONImageURL(source, signal);
    case SITE_BACKGROUND_SOURCE_TYPES.IMAGE_URL:
    default:
      return toAbsoluteURL(source.url);
  }
};

export const preloadSiteBackgroundImage = (url, signal) =>
  new Promise((resolve, reject) => {
    const image = new Image();
    let settled = false;

    const cleanup = () => {
      image.onload = null;
      image.onerror = null;
      signal?.removeEventListener('abort', onAbort);
    };
    const finish = (callback, value) => {
      if (settled) return;
      settled = true;
      cleanup();
      callback(value);
    };
    const onAbort = () => {
      image.src = '';
      finish(reject, new DOMException('Aborted', 'AbortError'));
    };

    image.referrerPolicy = 'no-referrer';
    image.onload = () => finish(resolve, url);
    image.onerror = () =>
      finish(reject, new Error('Background image failed to load'));
    signal?.addEventListener('abort', onAbort, { once: true });
    if (signal?.aborted) {
      onAbort();
      return;
    }
    image.src = url;
  });

export const resolveSiteBackground = async (sources, options = {}) => {
  const { signal } = options;
  const normalizedSources = Array.isArray(sources)
    ? sources.map(normalizeSource).filter(Boolean)
    : [];
  let lastError;

  for (const source of shuffled(normalizedSources)) {
    if (signal?.aborted) {
      throw new DOMException('Aborted', 'AbortError');
    }
    try {
      const url = await resolveSourceURL(source, signal);
      await preloadSiteBackgroundImage(url, signal);
      return { url, source };
    } catch (error) {
      if (signal?.aborted) {
        throw new DOMException('Aborted', 'AbortError');
      }
      lastError = error;
    }
  }

  throw lastError || new Error('No valid site background source');
};
