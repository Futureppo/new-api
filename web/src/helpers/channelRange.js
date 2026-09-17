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

// Keep the original text until validation so decimals and oversized IDs cannot
// be rounded into a different, valid deletion target by a numeric input.
export const parseChannelIdRange = (start, end) => {
  const values = [start, end].map((value) => String(value ?? '').trim());
  if (!values.every((value) => /^\d+$/.test(value))) return null;
  const [startId, endId] = values.map(Number);
  if (
    !Number.isSafeInteger(startId) ||
    !Number.isSafeInteger(endId) ||
    startId <= 0 ||
    endId < startId
  ) {
    return null;
  }
  return { start_id: startId, end_id: endId };
};
