// dto-contract holds the types in dto.ts to the goldens in
// internal/dto/testdata/golden, which are what the server serves. It is
// never bundled: nothing imports it, and tsc --noEmit, which the build runs
// first, is the test.
//
// Each golden is checked in both directions. Assigning it to its type fails
// when the golden lacks a field the type requires or carries one with another
// type. That alone would pass a golden with a field the types lack, because
// TypeScript checks excess properties only on object literals, never on an
// imported value; NoExtraKeys closes that half, at every depth.
//
// A golden added to the directory must be added here too; contract.test.ts
// fails until it is.

import type {
  AdminApplication,
  AdminApplicationList,
  AdminUserDetail,
  AdminUserList,
  ApplicationForm,
  ApplicationNote,
  ApplicationStatus,
  AuditPage,
  CreatedAgent,
  CreatedPost,
  CreatedThread,
  Draft,
  FlagReceipt,
  Form,
  FormAdmin,
  ForumDraft,
  ForumTree,
  GroupDetail,
  GroupList,
  HandleChange,
  IdentityList,
  IdentityRemoved,
  IssuedCredential,
  ManagedForumList,
  Me,
  PermissionGrid,
  PermissionPreview,
  Post,
  PostHistory,
  PostReplies,
  PostSource,
  Profile,
  Queue,
  QueueResolution,
  ReadPosition,
  SessionList,
  Settings,
  RenderResult,
  SiteSettings,
  SlowMode,
  ThreadHint,
  ThreadList,
  ThreadPage,
  UserPosts,
  UserSuggestions,
} from './dto';

import adminApplicationApprovedModerator from '../../../../internal/dto/testdata/golden/admin-application-approved.moderator.json';
import adminApplicationDeniedModerator from '../../../../internal/dto/testdata/golden/admin-application-denied.moderator.json';
import adminApplicationPendingModerator from '../../../../internal/dto/testdata/golden/admin-application-pending.moderator.json';
import adminApplicationWithdrawnModerator from '../../../../internal/dto/testdata/golden/admin-application-withdrawn.moderator.json';
import adminApplicationsModerator from '../../../../internal/dto/testdata/golden/admin-applications.moderator.json';
import applicationNoteModerator from '../../../../internal/dto/testdata/golden/application-note.moderator.json';
import auditAdmin from '../../../../internal/dto/testdata/golden/audit.admin.json';
import auditFilteredAdmin from '../../../../internal/dto/testdata/golden/audit-filtered.admin.json';
import adminForumsAdmin from '../../../../internal/dto/testdata/golden/admin-forums.admin.json';
import adminGroupAdmin from '../../../../internal/dto/testdata/golden/admin-group.admin.json';
import adminGroupsAdmin from '../../../../internal/dto/testdata/golden/admin-groups.admin.json';
import adminUserAgentAdmin from '../../../../internal/dto/testdata/golden/admin-user-agent.admin.json';
import adminUserSuspendedAdmin from '../../../../internal/dto/testdata/golden/admin-user-suspended.admin.json';
import adminUserSuspendedModerator from '../../../../internal/dto/testdata/golden/admin-user-suspended.moderator.json';
import adminUserAdmin from '../../../../internal/dto/testdata/golden/admin-user.admin.json';
import adminUserModerator from '../../../../internal/dto/testdata/golden/admin-user.moderator.json';
import adminUsersAdmin from '../../../../internal/dto/testdata/golden/admin-users.admin.json';
import permissionPreviewAdmin from '../../../../internal/dto/testdata/golden/permission-preview.admin.json';
import permissionsAdmin from '../../../../internal/dto/testdata/golden/permissions.admin.json';
import applyFormPreUser from '../../../../internal/dto/testdata/golden/apply-form.pre_user.json';
import applyStatusApprovedPreUser from '../../../../internal/dto/testdata/golden/apply-status-approved.pre_user.json';
import applyStatusNonePreUser from '../../../../internal/dto/testdata/golden/apply-status-none.pre_user.json';
import applyStatusPendingPreUser from '../../../../internal/dto/testdata/golden/apply-status-pending.pre_user.json';
import applyStatusWithdrawnPreUser from '../../../../internal/dto/testdata/golden/apply-status-withdrawn.pre_user.json';
import createdAgentAdmin from '../../../../internal/dto/testdata/golden/created-agent.admin.json';
import createdAgentTokenlessAdmin from '../../../../internal/dto/testdata/golden/created-agent-tokenless.admin.json';
import createdPostMember from '../../../../internal/dto/testdata/golden/created-post.member.json';
import createdThreadMember from '../../../../internal/dto/testdata/golden/created-thread.member.json';
import draftMember from '../../../../internal/dto/testdata/golden/draft.member.json';
import draftPlainMember from '../../../../internal/dto/testdata/golden/draft-plain.member.json';
import flagReceiptMember from '../../../../internal/dto/testdata/golden/flag-receipt.member.json';
import formAdminAdmin from '../../../../internal/dto/testdata/golden/form-admin.admin.json';
import formPreviewAdmin from '../../../../internal/dto/testdata/golden/form-preview.admin.json';
import forumDraftMember from '../../../../internal/dto/testdata/golden/forum-draft.member.json';
import forumDraftUntitledMember from '../../../../internal/dto/testdata/golden/forum-draft-untitled.member.json';
import forumsAnon from '../../../../internal/dto/testdata/golden/forums.anon.json';
import forumsMember from '../../../../internal/dto/testdata/golden/forums.member.json';
import historyMember from '../../../../internal/dto/testdata/golden/history.member.json';
import historyModerator from '../../../../internal/dto/testdata/golden/history.moderator.json';
import handleChangeAdmin from '../../../../internal/dto/testdata/golden/handle-change.admin.json';
import handleChangeMember from '../../../../internal/dto/testdata/golden/handle-change.member.json';
import identitiesMember from '../../../../internal/dto/testdata/golden/identities.member.json';
import identityRemovedMember from '../../../../internal/dto/testdata/golden/identity-removed.member.json';
import issuedCredentialAdmin from '../../../../internal/dto/testdata/golden/issued-credential.admin.json';
import meAgent from '../../../../internal/dto/testdata/golden/me.agent.json';
import meAnon from '../../../../internal/dto/testdata/golden/me.anon.json';
import meMember from '../../../../internal/dto/testdata/golden/me.member.json';
import meModerator from '../../../../internal/dto/testdata/golden/me.moderator.json';
import mePreUser from '../../../../internal/dto/testdata/golden/me.pre_user.json';
import postMember from '../../../../internal/dto/testdata/golden/post.member.json';
import postModerator from '../../../../internal/dto/testdata/golden/post.moderator.json';
import postSourceMember from '../../../../internal/dto/testdata/golden/post-source.member.json';
import profileMember from '../../../../internal/dto/testdata/golden/profile.member.json';
import profileModerator from '../../../../internal/dto/testdata/golden/profile.moderator.json';
import profileAnonymizedMember from '../../../../internal/dto/testdata/golden/profile-anonymized.member.json';
import queueModerator from '../../../../internal/dto/testdata/golden/queue.moderator.json';
import queueResolutionModerator from '../../../../internal/dto/testdata/golden/queue-resolution.moderator.json';
import readMember from '../../../../internal/dto/testdata/golden/read.member.json';
import renderMember from '../../../../internal/dto/testdata/golden/render.member.json';
import renderPlainMember from '../../../../internal/dto/testdata/golden/render-plain.member.json';
import repliesMember from '../../../../internal/dto/testdata/golden/replies.member.json';
import repliesModerator from '../../../../internal/dto/testdata/golden/replies.moderator.json';
import sessionsMember from '../../../../internal/dto/testdata/golden/sessions.member.json';
import settingsAdmin from '../../../../internal/dto/testdata/golden/settings.admin.json';
import slowModeModerator from '../../../../internal/dto/testdata/golden/slow-mode.moderator.json';
import threadAnon from '../../../../internal/dto/testdata/golden/thread.anon.json';
import threadLockedMember from '../../../../internal/dto/testdata/golden/thread-locked.member.json';
import threadArchivedMember from '../../../../internal/dto/testdata/golden/thread-archived.member.json';
import threadHintAnon from '../../../../internal/dto/testdata/golden/thread-hint.anon.json';
import threadMember from '../../../../internal/dto/testdata/golden/thread.member.json';
import threadModerator from '../../../../internal/dto/testdata/golden/thread.moderator.json';
import threadsAnon from '../../../../internal/dto/testdata/golden/threads.anon.json';
import threadsMember from '../../../../internal/dto/testdata/golden/threads.member.json';
import unreadMember from '../../../../internal/dto/testdata/golden/unread.member.json';
import userSettingsMember from '../../../../internal/dto/testdata/golden/user-settings.member.json';
import userPostsMember from '../../../../internal/dto/testdata/golden/user-posts.member.json';
import userPostsModerator from '../../../../internal/dto/testdata/golden/user-posts.moderator.json';
import userPostsOwnMember from '../../../../internal/dto/testdata/golden/user-posts-own.member.json';
import usersMember from '../../../../internal/dto/testdata/golden/users.member.json';

// NoExtraKeys is true when every key of U, at every depth, is a key of T.
// null in the golden says nothing about keys, and an empty array has no
// element to look into.
type NoExtraKeys<T, U> = [U] extends [null]
  ? true
  : [U] extends [readonly (infer UE)[]]
    ? NonNullable<T> extends readonly (infer TE)[]
      ? [UE] extends [never]
        ? true
        : NoExtraKeys<TE, UE>
      : false
    : [U] extends [object]
      ? [Exclude<keyof U, keyof NonNullable<T>>] extends [never]
        ? AllTrue<{ [K in keyof U]: K extends keyof NonNullable<T> ? NoExtraKeys<NonNullable<T>[K], U[K]> : false }>
        : false
      : true;

type AllTrue<O> = false extends O[keyof O] ? false : true;

// matches compiles only when golden is a T with no key T lacks. The error for
// an extra key names the golden's property as not assignable to never.
function matches<T>() {
  return <U extends T>(golden: U & (NoExtraKeys<T, U> extends true ? unknown : never)) => golden;
}

export const contract = [
  matches<AdminApplication>()(adminApplicationApprovedModerator),
  matches<AdminApplication>()(adminApplicationDeniedModerator),
  matches<AdminApplication>()(adminApplicationPendingModerator),
  matches<AdminApplication>()(adminApplicationWithdrawnModerator),
  matches<AdminApplicationList>()(adminApplicationsModerator),
  matches<ApplicationNote>()(applicationNoteModerator),
  matches<ManagedForumList>()(adminForumsAdmin),
  matches<GroupDetail>()(adminGroupAdmin),
  matches<GroupList>()(adminGroupsAdmin),
  matches<AdminUserDetail>()(adminUserAgentAdmin),
  matches<AdminUserDetail>()(adminUserAdmin),
  matches<AdminUserDetail>()(adminUserModerator),
  matches<AdminUserDetail>()(adminUserSuspendedAdmin),
  matches<AdminUserDetail>()(adminUserSuspendedModerator),
  matches<AdminUserList>()(adminUsersAdmin),
  matches<PermissionPreview>()(permissionPreviewAdmin),
  matches<PermissionGrid>()(permissionsAdmin),
  matches<ApplicationForm>()(applyFormPreUser),
  matches<ApplicationStatus>()(applyStatusApprovedPreUser),
  matches<ApplicationStatus>()(applyStatusNonePreUser),
  matches<ApplicationStatus>()(applyStatusPendingPreUser),
  matches<ApplicationStatus>()(applyStatusWithdrawnPreUser),
  matches<AuditPage>()(auditAdmin),
  matches<AuditPage>()(auditFilteredAdmin),
  matches<CreatedAgent>()(createdAgentAdmin),
  matches<CreatedAgent>()(createdAgentTokenlessAdmin),
  matches<CreatedPost>()(createdPostMember),
  matches<CreatedThread>()(createdThreadMember),
  matches<Draft>()(draftMember),
  matches<Draft>()(draftPlainMember),
  matches<FlagReceipt>()(flagReceiptMember),
  matches<FormAdmin>()(formAdminAdmin),
  matches<Form>()(formPreviewAdmin),
  matches<ForumDraft>()(forumDraftMember),
  matches<ForumDraft>()(forumDraftUntitledMember),
  matches<ForumTree>()(forumsAnon),
  matches<ForumTree>()(forumsMember),
  matches<PostHistory>()(historyMember),
  matches<PostHistory>()(historyModerator),
  matches<HandleChange>()(handleChangeAdmin),
  matches<HandleChange>()(handleChangeMember),
  matches<IdentityList>()(identitiesMember),
  matches<IdentityRemoved>()(identityRemovedMember),
  matches<IssuedCredential>()(issuedCredentialAdmin),
  matches<Me>()(meAgent),
  matches<Me>()(meAnon),
  matches<Me>()(meMember),
  matches<Me>()(meModerator),
  matches<Me>()(mePreUser),
  matches<Post>()(postMember),
  matches<Post>()(postModerator),
  matches<PostSource>()(postSourceMember),
  matches<PostReplies>()(repliesMember),
  matches<PostReplies>()(repliesModerator),
  matches<Profile>()(profileMember),
  matches<Profile>()(profileModerator),
  matches<Profile>()(profileAnonymizedMember),
  matches<Queue>()(queueModerator),
  matches<QueueResolution>()(queueResolutionModerator),
  matches<ReadPosition>()(readMember),
  matches<RenderResult>()(renderMember),
  matches<RenderResult>()(renderPlainMember),
  matches<SessionList>()(sessionsMember),
  matches<SiteSettings>()(settingsAdmin),
  matches<Settings>()(userSettingsMember),
  matches<SlowMode>()(slowModeModerator),
  matches<ThreadPage>()(threadAnon),
  matches<ThreadPage>()(threadLockedMember),
  matches<ThreadPage>()(threadArchivedMember),
  matches<ThreadPage>()(threadMember),
  matches<ThreadPage>()(threadModerator),
  matches<ThreadHint>()(threadHintAnon),
  matches<ThreadList>()(threadsAnon),
  matches<ThreadList>()(threadsMember),
  matches<ThreadList>()(unreadMember),
  matches<UserPosts>()(userPostsMember),
  matches<UserPosts>()(userPostsModerator),
  matches<UserPosts>()(userPostsOwnMember),
  matches<UserSuggestions>()(usersMember),
];
