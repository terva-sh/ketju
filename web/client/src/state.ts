import { signal } from '@preact/signals';
import { readBootstrap } from './bootstrap';

// The viewer, from the bootstrap block, until /me is fetched (TKT-01M29CA99R).
export const viewer = signal(readBootstrap());
