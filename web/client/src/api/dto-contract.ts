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
  CreatedPost,
  CreatedThread,
  Draft,
  ForumTree,
  Me,
  Post,
  PostHistory,
  PostReplies,
  ReadPosition,
  RenderResult,
  ThreadList,
  ThreadPage,
  UserSuggestions,
} from './dto';

import createdPostMember from '../../../../internal/dto/testdata/golden/created-post.member.json';
import createdThreadMember from '../../../../internal/dto/testdata/golden/created-thread.member.json';
import draftMember from '../../../../internal/dto/testdata/golden/draft.member.json';
import draftPlainMember from '../../../../internal/dto/testdata/golden/draft-plain.member.json';
import forumsAnon from '../../../../internal/dto/testdata/golden/forums.anon.json';
import forumsMember from '../../../../internal/dto/testdata/golden/forums.member.json';
import historyMember from '../../../../internal/dto/testdata/golden/history.member.json';
import historyModerator from '../../../../internal/dto/testdata/golden/history.moderator.json';
import meAnon from '../../../../internal/dto/testdata/golden/me.anon.json';
import meMember from '../../../../internal/dto/testdata/golden/me.member.json';
import meModerator from '../../../../internal/dto/testdata/golden/me.moderator.json';
import mePreUser from '../../../../internal/dto/testdata/golden/me.pre_user.json';
import postMember from '../../../../internal/dto/testdata/golden/post.member.json';
import postModerator from '../../../../internal/dto/testdata/golden/post.moderator.json';
import readMember from '../../../../internal/dto/testdata/golden/read.member.json';
import renderMember from '../../../../internal/dto/testdata/golden/render.member.json';
import renderPlainMember from '../../../../internal/dto/testdata/golden/render-plain.member.json';
import repliesMember from '../../../../internal/dto/testdata/golden/replies.member.json';
import repliesModerator from '../../../../internal/dto/testdata/golden/replies.moderator.json';
import threadAnon from '../../../../internal/dto/testdata/golden/thread.anon.json';
import threadLockedMember from '../../../../internal/dto/testdata/golden/thread-locked.member.json';
import threadMember from '../../../../internal/dto/testdata/golden/thread.member.json';
import threadModerator from '../../../../internal/dto/testdata/golden/thread.moderator.json';
import threadsAnon from '../../../../internal/dto/testdata/golden/threads.anon.json';
import threadsMember from '../../../../internal/dto/testdata/golden/threads.member.json';
import unreadMember from '../../../../internal/dto/testdata/golden/unread.member.json';
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
  matches<CreatedPost>()(createdPostMember),
  matches<CreatedThread>()(createdThreadMember),
  matches<Draft>()(draftMember),
  matches<Draft>()(draftPlainMember),
  matches<ForumTree>()(forumsAnon),
  matches<ForumTree>()(forumsMember),
  matches<PostHistory>()(historyMember),
  matches<PostHistory>()(historyModerator),
  matches<Me>()(meAnon),
  matches<Me>()(meMember),
  matches<Me>()(meModerator),
  matches<Me>()(mePreUser),
  matches<Post>()(postMember),
  matches<Post>()(postModerator),
  matches<PostReplies>()(repliesMember),
  matches<PostReplies>()(repliesModerator),
  matches<ReadPosition>()(readMember),
  matches<RenderResult>()(renderMember),
  matches<RenderResult>()(renderPlainMember),
  matches<ThreadPage>()(threadAnon),
  matches<ThreadPage>()(threadLockedMember),
  matches<ThreadPage>()(threadMember),
  matches<ThreadPage>()(threadModerator),
  matches<ThreadList>()(threadsAnon),
  matches<ThreadList>()(threadsMember),
  matches<ThreadList>()(unreadMember),
  matches<UserSuggestions>()(usersMember),
];
