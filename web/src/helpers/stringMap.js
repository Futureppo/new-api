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

export function parseStringMap(value) {
  const parsed =
    typeof value === 'string' ? (value.trim() ? JSON.parse(value) : {}) : value;
  let code;
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    code = 'object';
  } else if (Object.keys(parsed).some((key) => key === '')) {
    code = 'empty_key';
  } else if (Object.values(parsed).some((entry) => typeof entry !== 'string')) {
    code = 'string';
  }
  if (code) {
    const error = new Error(code);
    error.code = code;
    throw error;
  }
  return parsed;
}
