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

export const PASSWORD_POLICY_MESSAGE =
  '密码至少 8 个字符，最多 72 个 UTF-8 字节，且必须包含数字、大写字母、小写字母和英文标点符号。';

export function getPasswordChecks(password = '') {
  return {
    length: Array.from(password).length >= 8,
    bytes: new TextEncoder().encode(password).length <= 72,
    digit: /[0-9]/.test(password),
    upper: /[A-Z]/.test(password),
    lower: /[a-z]/.test(password),
    symbol: /[\x21-\x2f\x3a-\x40\x5b-\x60\x7b-\x7e]/.test(password),
  };
}

export function isValidLoginPassword(password) {
  return Object.values(getPasswordChecks(password)).every(Boolean);
}
