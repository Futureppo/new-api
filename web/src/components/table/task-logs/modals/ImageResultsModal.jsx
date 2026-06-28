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
import { Button, Empty, Modal, Space, Typography } from '@douyinfe/semi-ui';
import { Copy, Download, ExternalLink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { copy, showError, showSuccess } from '../../../../helpers';

const { Text } = Typography;

const toDataUrl = (value) => {
  if (!value) {
    return '';
  }
  if (value.startsWith('data:')) {
    return value;
  }
  return `data:image/png;base64,${value}`;
};

const getFilename = (src, index) => {
  if (!src) {
    return `image-${index + 1}.png`;
  }
  if (src.startsWith('data:')) {
    const match = src.match(/^data:image\/([^;]+);/);
    const ext = match?.[1] || 'png';
    return `image-${index + 1}.${ext === 'jpeg' ? 'jpg' : ext}`;
  }
  try {
    const url = new URL(src);
    const last = decodeURIComponent(
      url.pathname.split('/').filter(Boolean).pop() || '',
    );
    return last || `image-${index + 1}.png`;
  } catch (error) {
    return `image-${index + 1}.png`;
  }
};

const extractImageResults = (record) => {
  const data = record?.data;
  const rawItems = Array.isArray(data)
    ? data
    : Array.isArray(data?.data)
      ? data.data
      : [];

  const results = rawItems
    .map((item, index) => {
      const src =
        item?.url ||
        item?.image_url ||
        item?.data_url ||
        toDataUrl(item?.b64_json || item?.base64 || '');
      return {
        src,
        index,
        revisedPrompt: item?.revised_prompt || '',
        hasB64: Boolean(item?.has_b64_json || item?.b64_json || item?.base64),
      };
    })
    .filter((item) => item.src);

  if (results.length === 0 && record?.result_url) {
    results.push({
      src: record.result_url,
      index: 0,
      revisedPrompt: '',
      hasB64: false,
    });
  }

  return results;
};

const ImageResultsModal = ({
  isModalOpen,
  setIsModalOpen,
  imageRecord,
}) => {
  const { t } = useTranslation();
  const images = extractImageResults(imageRecord);

  const copySource = async (src) => {
    if (await copy(src)) {
      showSuccess(t('已复制链接'));
    } else {
      showError(t('复制失败'));
    }
  };

  const downloadSource = (src, index) => {
    const link = document.createElement('a');
    link.href = src;
    link.download = getFilename(src, index);
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  const openSource = (src) => {
    window.open(src, '_blank', 'noopener,noreferrer');
  };

  return (
    <Modal
      visible={isModalOpen}
      onOk={() => setIsModalOpen(false)}
      onCancel={() => setIsModalOpen(false)}
      title={t('图片结果')}
      width='86vw'
      style={{ maxWidth: 1100 }}
      bodyStyle={{ maxHeight: '78vh', overflow: 'auto' }}
    >
      {images.length === 0 ? (
        <Empty description={t('暂无可预览图片')} />
      ) : (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))',
            gap: 16,
          }}
        >
          {images.map((item, index) => (
            <div
              key={`${item.src.slice(0, 48)}-${index}`}
              style={{
                border: '1px solid var(--semi-color-border)',
                borderRadius: 8,
                overflow: 'hidden',
                background: 'var(--semi-color-bg-0)',
              }}
            >
              <div
                style={{
                  width: '100%',
                  aspectRatio: '1 / 1',
                  background: 'var(--semi-color-fill-0)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <img
                  src={item.src}
                  alt={`${t('图片结果')} ${index + 1}`}
                  style={{
                    width: '100%',
                    height: '100%',
                    objectFit: 'contain',
                  }}
                />
              </div>
              <div style={{ padding: 12 }}>
                <Space wrap>
                  <Button
                    size='small'
                    icon={<Download size={14} />}
                    onClick={() => downloadSource(item.src, index)}
                  >
                    {t('下载')}
                  </Button>
                  <Button
                    size='small'
                    icon={<Copy size={14} />}
                    onClick={() => copySource(item.src)}
                  >
                    {item.src.startsWith('data:') ? t('复制图片数据') : t('复制链接')}
                  </Button>
                  {!item.src.startsWith('data:') && (
                    <Button
                      size='small'
                      icon={<ExternalLink size={14} />}
                      onClick={() => openSource(item.src)}
                    >
                      {t('打开')}
                    </Button>
                  )}
                </Space>
                {item.revisedPrompt && (
                  <Text
                    type='tertiary'
                    ellipsis={{ showTooltip: true }}
                    style={{
                      display: 'block',
                      marginTop: 10,
                      maxWidth: '100%',
                    }}
                  >
                    {item.revisedPrompt}
                  </Text>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </Modal>
  );
};

export default ImageResultsModal;
