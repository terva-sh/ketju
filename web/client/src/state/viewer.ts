import { computed, signal } from '@preact/signals';
import { apiGet } from '../api/client';
import type { Me } from '../api/dto';
import { readBootstrap } from '../bootstrap';

// viewer is who is reading, seeded from the bootstrap block the shell inlines
// (docs/07 "Embedding") so the first paint needs no request, and replaced
// whenever /me is fetched again.
export const viewer = signal<Me>(readBootstrap());

// notifications is the viewer's unread notification count. It is derived,
// not stored, so it cannot disagree with the viewer it came from.
export const notifications = computed(() => viewer.value.notifications);

// refreshViewer fetches /me and replaces the viewer with it. A failure leaves
// the viewer as it was and is the caller's to report.
export async function refreshViewer(signal?: AbortSignal): Promise<Me> {
  const me = await apiGet<Me>('/api/v1/me', { signal });
  viewer.value = me;
  return me;
}
