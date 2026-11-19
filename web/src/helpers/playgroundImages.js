export function clipboardImages(clipboard) {
  const files = Array.from(clipboard?.files || []).filter((file) =>
    file.type.startsWith('image/'),
  );
  if (files.length) return files;
  return Array.from(clipboard?.items || [])
    .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
    .map((item) => item.getAsFile())
    .filter(Boolean);
}

export function readImageDataURL(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result);
    reader.onerror = () => reject(reader.error);
    reader.onabort = () => reject(new Error('File read aborted'));
    reader.readAsDataURL(file);
  });
}
