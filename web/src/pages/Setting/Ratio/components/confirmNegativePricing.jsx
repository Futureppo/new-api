import React from 'react';
import { Modal } from '@douyinfe/semi-ui';
import { getNegativePricingItems } from '../negativePricing';

export async function confirmNegativePricing(options, submittedKeys, t) {
  const items = getNegativePricingItems(options, submittedKeys, t);
  if (!items.length) return true;
  return new Promise((resolve) => {
    Modal.confirm({
      title: t('负数价格提醒'),
      content: (
        <div>
          <p>
            {t(
              '检测到负数价格。负数费用会增加用户余额；存在正数费用时，按合计净费用结算。订阅计费将恢复套餐额度，最多恢复至套餐总额度。是否继续保存？',
            )}
          </p>
          <ul style={{ maxHeight: 240, overflowY: 'auto', paddingLeft: 20 }}>
            {items.map((item, index) => (
              <li key={index}>
                {item.name} · {item.label}：{item.value} {item.unit}
              </li>
            ))}
          </ul>
        </div>
      ),
      okText: t('确认保存'),
      cancelText: t('取消'),
      okButtonProps: { type: 'danger' },
      maskClosable: false,
      onOk: () => resolve(true),
      onCancel: () => resolve(false),
    });
  });
}
