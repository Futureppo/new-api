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

import React, { useRef, useEffect, useCallback } from 'react';
import { Button, Toast } from '@douyinfe/semi-ui';
import { Paperclip } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { usePlayground } from '../../contexts/PlaygroundContext';
import {
  clipboardImages,
  readImageDataURL,
} from '../../helpers/playgroundImages';

const CustomInputRender = (props) => {
  const { t } = useTranslation();
  const {
    onPasteImages,
    onRemoveChatImage,
    chatImages,
    chatAttachmentsEnabled,
  } = usePlayground();
  const { detailProps } = props;
  const { clearContextNode, inputNode, sendNode, onClick } = detailProps;
  const containerRef = useRef(null);
  const fileInputRef = useRef(null);
  const readingImages = useRef(Promise.resolve());

  const addFiles = useCallback(
    (files) => {
      if (!files.length || !chatAttachmentsEnabled) return;
      readingImages.current = readingImages.current.then(async () => {
        try {
          const images = await Promise.all(
            files.map(async (file) => ({
              name: file.name,
              url: await readImageDataURL(file),
            })),
          );
          onPasteImages(images);
        } catch {
          Toast.error(t('读取图片失败'));
        }
      });
    },
    [chatAttachmentsEnabled, onPasteImages, t],
  );

  const handlePaste = useCallback(
    (event) => {
      if (!chatAttachmentsEnabled) return;
      const files = clipboardImages(event.clipboardData);
      if (!files.length) return;
      event.preventDefault();
      addFiles(files);
    },
    [addFiles, chatAttachmentsEnabled],
  );

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    container.addEventListener('paste', handlePaste);
    return () => {
      container.removeEventListener('paste', handlePaste);
    };
  }, [handlePaste]);

  // 清空按钮
  const styledClearNode = clearContextNode
    ? React.cloneElement(clearContextNode, {
        className: `!rounded-full !bg-gray-100 hover:!bg-red-500 hover:!text-white flex-shrink-0 transition-all ${clearContextNode.props.className || ''}`,
        style: {
          ...clearContextNode.props.style,
          width: '32px',
          height: '32px',
          minWidth: '32px',
          padding: 0,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        },
      })
    : null;

  // 发送按钮
  const styledSendNode = React.cloneElement(sendNode, {
    className: `!rounded-full !bg-purple-500 hover:!bg-purple-600 flex-shrink-0 transition-all ${sendNode.props.className || ''}`,
    style: {
      ...sendNode.props.style,
      width: '32px',
      height: '32px',
      minWidth: '32px',
      padding: 0,
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
    },
  });

  return (
    <div className='p-2 sm:p-4' ref={containerRef}>
      {chatAttachmentsEnabled && chatImages.length > 0 && (
        <ol className='mb-2 flex flex-wrap gap-2'>
          {chatImages.map(({ name, url }, index) => (
            <li
              key={index}
              className='flex items-center gap-2 rounded border border-[var(--semi-color-border)] p-1 text-xs'
            >
              <img
                src={url}
                alt={name}
                className='h-12 w-12 rounded object-contain'
              />
              <Button
                size='small'
                aria-label={`${t('移除')} ${name}`}
                onClick={() => onRemoveChatImage(index)}
              >
                {t('移除')}
              </Button>
            </li>
          ))}
        </ol>
      )}
      <div
        className='flex items-center gap-2 sm:gap-3 p-2 bg-gray-50 rounded-xl sm:rounded-2xl shadow-sm hover:shadow-md transition-shadow'
        style={{ border: '1px solid var(--semi-color-border)' }}
        onClick={onClick}
        title={t('支持 Ctrl+V 粘贴图片')}
      >
        {/* 清空对话按钮 - 左边 */}
        {styledClearNode}
        {chatAttachmentsEnabled && (
          <>
            <Button
              icon={<Paperclip size={16} />}
              aria-label={t('选择本地图片')}
              onClick={(event) => {
                event.stopPropagation();
                fileInputRef.current.click();
              }}
            />
            <input
              ref={fileInputRef}
              type='file'
              accept='image/*'
              multiple
              hidden
              onChange={(event) => {
                addFiles(Array.from(event.target.files || []));
                event.target.value = '';
              }}
            />
          </>
        )}
        <div className='flex-1'>{inputNode}</div>
        {/* 发送按钮 - 右边 */}
        {styledSendNode}
      </div>
    </div>
  );
};

export default CustomInputRender;
