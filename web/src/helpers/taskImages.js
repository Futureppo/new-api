const toDataUrl = (value) => {
  if (!value) return '';
  return value.startsWith('data:') ? value : `data:image/png;base64,${value}`;
};

export const extractImageResults = (record) => {
  const data = record?.data;
  const outcome = data?.outcome || data?.data?.outcome;
  const rawItems = Array.isArray(data)
    ? data
    : Array.isArray(data?.data)
      ? data.data
      : [
          ...(Array.isArray(outcome?.media_urls) ? outcome.media_urls : []),
          ...(Array.isArray(outcome?.medias) ? outcome.medias : []),
        ];
  const seen = new Set();
  const results = rawItems
    .filter((item) => !item?.type || item.type === 'image')
    .map((item, index) => {
      const src =
        item?.url ||
        item?.image_url ||
        item?.data_url ||
        toDataUrl(item?.b64_json || item?.base64 || '');
      return {
        src,
        index,
        width: item?.width,
        height: item?.height,
        revisedPrompt: item?.revised_prompt || '',
        hasB64: Boolean(item?.has_b64_json || item?.b64_json || item?.base64),
      };
    })
    .filter((item) => {
      if (!item.src || seen.has(item.src)) return false;
      seen.add(item.src);
      return true;
    });

  const fallback = record?.result_url || outcome?.thumbnail_image_url;
  if (results.length === 0 && fallback) {
    results.push({ src: fallback, index: 0, revisedPrompt: '', hasB64: false });
  }
  return results;
};
