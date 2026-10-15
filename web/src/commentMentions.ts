import type { CommentMention } from './types';

// Keep selected identities only while their whole label survives an edit.
// Offsets match the textarea's UTF-16 positions (including emoji).
export function moveMentions(before: string, after: string, mentions: CommentMention[]): CommentMention[] {
  let start = 0;
  while (start < before.length && start < after.length && before[start] === after[start]) start++;
  let oldEnd = before.length, newEnd = after.length;
  while (oldEnd > start && newEnd > start && before[oldEnd - 1] === after[newEnd - 1]) { oldEnd--; newEnd--; }
  const delta = newEnd - oldEnd;
  return mentions.flatMap(m => {
    if (m.end <= start) return [m];
    if (m.start >= oldEnd) return [{ ...m, start: m.start + delta, end: m.end + delta }];
    return [];
  }).filter(m => after.slice(m.start, m.end) === '@' + m.label);
}

export const OPEN_COMMENT = 'salt:open-comment';
let target: { pageId: string; commentId: string } | null = null;
export const commentTarget = () => target;
export const clearCommentTarget = () => { target = null; };
export function openComment(pageId: string, commentId: string) {
  target = { pageId, commentId };
  window.dispatchEvent(new CustomEvent('salt:navigate', { detail: pageId }));
  window.dispatchEvent(new Event(OPEN_COMMENT));
}

// Random IDs also work over ordinary HTTP on a local network.
export function newMentionID(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), b => b.toString(16).padStart(2, '0')).join('');
}
export const OPEN_PAGE_MENTION = 'salt:open-page-mention';
let pageTarget: { pageId: string; mentionId: string } | null = null;
export const pageMentionTarget = () => pageTarget;
export const clearPageMentionTarget = () => { pageTarget = null; };
export function openPageMention(pageId: string, mentionId: string) {
  pageTarget = { pageId, mentionId };
  window.dispatchEvent(new CustomEvent('salt:navigate', { detail: pageId }));
  window.dispatchEvent(new Event(OPEN_PAGE_MENTION));
}
export function hasPageMention(content: unknown, mentionId: string): boolean {
  if (!content || typeof content !== 'object') return false;
  if (Array.isArray(content)) return content.some(node => hasPageMention(node, mentionId));
  const node = content as { type?: string; props?: { mentionId?: string } };
  if (node.type === 'userMention' && node.props?.mentionId === mentionId) return true;
  return Object.values(content).some(value => hasPageMention(value, mentionId));
}
