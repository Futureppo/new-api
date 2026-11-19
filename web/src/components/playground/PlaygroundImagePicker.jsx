import React, { useEffect, useState } from 'react';
import { Button, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';

function ImagePreview({ file, label, onRemove }) {
  const { t } = useTranslation();
  const [url, setURL] = useState('');
  useEffect(() => {
    const preview = URL.createObjectURL(file);
    setURL(preview);
    return () => URL.revokeObjectURL(preview);
  }, [file]);

  return (
    <li className='flex min-w-0 items-center gap-2 rounded border border-[var(--semi-color-border)] p-2'>
      {url && (
        <img
          src={url}
          alt={label}
          className='h-14 w-14 shrink-0 rounded object-contain'
        />
      )}
      <span className='min-w-0 flex-1 break-all text-xs'>{file.name}</span>
      <Button
        size='small'
        aria-label={`${t('移除')} ${label}`}
        onClick={onRemove}
      >
        {t('移除')}
      </Button>
    </li>
  );
}

export default function PlaygroundImagePicker({
  label,
  files,
  limit,
  target,
  onAdd,
  onRemove,
}) {
  const { t } = useTranslation();
  const overLimit = limit > 0 && files.length > limit;
  return (
    <div
      tabIndex={0}
      role='group'
      aria-label={label}
      data-image-target={target}
      className='space-y-2 rounded-lg border border-dashed border-[var(--semi-color-border)] p-3 focus:border-[var(--semi-color-primary)]'
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <Typography.Text>{label}</Typography.Text>
        {limit > 0 && (
          <Typography.Text
            type={overLimit ? 'danger' : 'tertiary'}
            className='text-xs'
          >
            {t('已添加 {{count}} 张，最多 {{limit}} 张', {
              count: files.length,
              limit,
            })}
          </Typography.Text>
        )}
      </div>
      {files.length > 0 && (
        <ol className='grid gap-2 sm:grid-cols-2'>
          {files.map(({ id, file }, index) => (
            <ImagePreview
              key={id}
              file={file}
              label={`${label} ${index + 1}`}
              onRemove={() => onRemove(id)}
            />
          ))}
        </ol>
      )}
      <label className='block space-y-1 text-sm'>
        <span className='block'>{t('粘贴或选择参考图片')}</span>
        <input
          type='file'
          accept='image/*'
          multiple={limit !== 1}
          onChange={(event) => {
            onAdd(Array.from(event.target.files || []));
            event.target.value = '';
          }}
        />
      </label>
    </div>
  );
}
