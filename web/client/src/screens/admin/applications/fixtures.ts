import type { AdminApplication, AdminApplicationList, Me } from '../../../api/dto';
import moderatorMe from '../../../../../../internal/dto/testdata/golden/me.moderator.json';
import list from '../../../../../../internal/dto/testdata/golden/admin-applications.moderator.json';
import pending from '../../../../../../internal/dto/testdata/golden/admin-application-pending.moderator.json';
import denied from '../../../../../../internal/dto/testdata/golden/admin-application-denied.moderator.json';
import approved from '../../../../../../internal/dto/testdata/golden/admin-application-approved.moderator.json';

// The applications screen's tests answer with the server's goldens
// (internal/dto/testdata/golden), copied so a test may change its own.

export const asModerator = () => structuredClone(moderatorMe) as Me;
export const listPage = () => structuredClone(list) as AdminApplicationList;
export const pendingApp = () => structuredClone(pending) as unknown as AdminApplication;
export const deniedApp = () => structuredClone(denied) as unknown as AdminApplication;
export const approvedApp = () => structuredClone(approved) as unknown as AdminApplication;

export const appUrl = (id: number) => `/api/v1/admin/applications/${id}`;
