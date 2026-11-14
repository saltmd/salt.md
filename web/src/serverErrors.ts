import { plural, t } from './i18n';

// The server's side of the translation.
//
// Server messages travel as English text plus a machine-readable `code`. The
// English is what curl, a script or an MCP agent sees, and it is a real
// sentence rather than an identifier — an agent that hits `owner_only_backup`
// should not have to guess. The browser ignores the text and renders the
// reader's own language from the code.
//
// So the server never learns what language anybody speaks, and adding a
// language means adding entries to a catalog — not touching Go.
//
// A code with no entry here falls back to the server's English. That is the
// same bargain the rest of the interface makes: a missing translation degrades
// to a correct sentence in the wrong language, never to a broken one.

/** Values that came alongside the code, for the messages that carry a count. */
export type ErrorData = Record<string, unknown>;

export function serverMessage(code: string, fallback: string, data?: ErrorData): string {
  const n = typeof data?.pages === 'number' ? (data.pages as number) : 0;
  // Text the PROVIDER produced — Google's rejection, an HTTP body. Nobody can
  // translate it, so it travels beside the sentence instead of being baked into
  // it, and is appended verbatim (see codedError in server/server.go).
  const detail = typeof data?.detail === 'string' ? (data.detail as string) : '';
  const withDetail = (s: string) => (detail ? `${s} (${detail})` : s);
  switch (code) {
    // ---- registration and sign-in ----
    case 'signup_not_allowed':
      return t('This email address cannot register on its own. Ask for an invitation.');
    case 'bad_credentials':
      return t('Wrong email or wrong password.');
    case 'account_disabled':
      return t('This account has been deactivated. Talk to an admin.');

    // ---- read-only workspaces ----
    case 'workspace_read_only':
      return t('You can only read this workspace, so nothing can be created or reordered in it.');
    case 'no_writable_workspace':
      return t('There is no workspace this account may create pages in.');

    case 'collection_move_invalid':
      return t('Choose a card and a different collection.');
    case 'collection_move_unavailable':
      return t('The card or collection is no longer available.');
    case 'collection_move_stale':
      return t('The card moved again. This move could not be undone.');
    case 'collection_move_cycle':
      return t('A card cannot be moved into a collection inside it.');
    case 'collection_move_failed':
      return t('The card could not be moved.');

    case 'comment_anchor_stale':
      return t('The page changed. Select the text again.');

    // ---- agent keys from listed addresses only ----
    case 'agent_addresses_invalid':
      return t('Line {line} of the address list is neither an address nor a network: {entry}', {
        line: Number(data?.line ?? 0),
        entry: String(data?.entry ?? ''),
      });
    case 'agent_addresses_empty':
      return t('Add at least one address or network before switching this on.');
    case 'agent_address_not_allowed':
      return t('This instance accepts agent keys only from listed addresses, and {address} is not one of them.', {
        address: String(data?.address ?? ''),
      });
    case '2fa_required':
      return t('Please enter the 6-digit code from your authenticator app.');
    case '2fa_invalid':
      return t('Wrong code. Try again.');

    // ---- the owner role ----
    case 'owner_only_backup':
      return t('Only the owner can download an instance backup. It contains every workspace.');
    case 'backup_incomplete':
      return t('Some uploaded files could not be read, so the backup would be incomplete. The server log names them.');
    case 'owner_cannot_be_disabled':
      return t('The owner cannot be deactivated. Hand the owner role on first.');
    case 'owner_cannot_be_deleted':
      return t('The owner cannot be deleted. Hand the owner role to another account first.');
    case 'owner_rights_locked':
      return t('The owner’s rights cannot be revoked. Hand the owner role on first.');
    case 'owner_must_be_admin':
      return t(
        'Only an account that is already an instance admin can take the instance over. Make it one first.',
      );
    case 'owner_only_credentials':
      return t(
        'Only the owner can change another account’s password or email. As an admin you can send an invitation.',
      );
    case 'disabled_cannot_own':
      return t('A deactivated account cannot take over the instance.');
    case 'owner_only':
      return t('Only the owner of this instance can do that.');
    case 'already_owner':
      return t('You are already the owner.');
    case 'session_required':
      return t('This action requires signing in through a browser. An API token is not enough.');
    case 'rules_too_long':
      return t('Workspace rules are limited to 16000 characters.');
    case 'reason_too_short':
      return t(
        'Please give a reason somebody can follow (at least 10 characters). It is logged and shown to the people in charge of this workspace.',
      );
    case 'no_self_disable':
      return t('You cannot deactivate your own account.');

    // ---- personal spaces ----
    case 'personal_not_adoptable':
      return t('A personal space is not adopted. It belongs to an account.');
    case 'personal_no_break_glass':
      return t(
        'A personal space cannot be looked into even in an emergency. It belongs to exactly one account.',
      );
    case 'personal_no_autojoin':
      return t('A personal space cannot be opened to everyone.');
    case 'personal_role_fixed':
      return t('That is this person’s personal space. Their role in it stays as it is.');
    case 'personal_no_remove':
      return t('That is this person’s personal space. They cannot be removed from it.');
    case 'personal_invite_owner_only':
      return t('A personal space is not handed out from outside. Only its owner invites anyone there.');

    // ---- workspaces and membership ----
    case 'workspace_has_members':
      return t(
        'This workspace still has members. If nobody is in charge, appoint one of them in user management. For a look inside there is emergency access.',
      );
    case 'workspace_delete_from_inside':
      return t('This workspace still has members. It can only be deleted from the inside.');
    case 'owner_only_autojoin':
      return t('Only the owner can open a workspace to everyone.');
    case 'not_workspace_admin':
      return t('Only the owner or an admin of this workspace can change its members.');
    case 'last_admin_other':
      return t('That is the last admin of this workspace. Make somebody else an admin first.');
    case 'no_self_grant':
      return t('You cannot grant yourself access here. Use emergency access, which is logged.');
    case 'last_admin':
      return t(
        'You are the last admin of this workspace. Make somebody else an admin first, or delete the workspace if it should go.',
      );
    case 'already_member':
      return t('You are already a member of this workspace. Emergency access is not needed.');

    // These two carry a count, which is why it travels beside the code: a
    // number baked into an English sentence cannot go through German's — or
    // Polish's — plural rules.
    case 'private_pages_left_self':
      return t('You have {pages} here. They stay in the workspace and will only be visible to its admins afterwards.', {
        pages: plural(n, '{n} private page', '{n} private pages'),
      });
    case 'private_pages_left_other':
      return t('This person has {pages} here. They stay in the workspace and will only be visible to its admins afterwards.', {
        pages: plural(n, '{n} private page', '{n} private pages'),
      });

    // ---- signing in with Google / Microsoft ----
    // These do not arrive as JSON: the provider sends the browser back to a
    // URL, so the code rides in the query string instead. Same bargain.
    case 'oauth_not_configured':
      return t('This sign-in method is not configured.');
    case 'oauth_cancelled':
      return t('Sign-in was cancelled.');
    case 'oauth_expired':
      return t('Sign-in expired. Please try again.');
    case 'oauth_bad_state':
      return t('Sign-in could not be verified. Please try again.');
    case 'oauth_no_code':
      return t('No authorization code received.');
    case 'oauth_token_exchange':
      return t('Token exchange failed.');
    case 'oauth_failed':
      return t('Sign-in failed.');
    case 'oauth_no_email':
      return t('The provider did not supply an email address.');
    case 'oauth_email_unverified':
      return t('This Google address is not verified.');
    case 'oauth_email_squatter':
      return t(
        'This address belongs to an account that has not confirmed it. Please sign in with a password or contact your administrator.',
      );
    case 'oauth_signup_blocked':
      return t('This address cannot create an account here.');
    case 'oauth_session_failed':
      return t('The session could not be created.');
    case 'oauth_wrong_tenant':
      return t('This Microsoft account belongs to a different organisation than the one this instance is set up for.');
    // Not a sign-in error but the settings dialog's, kept beside the others
    // about the same tenant.
    case 'oauth_tenant_invalid':
      return t(
        'The Microsoft tenant must be a directory (tenant) ID or a verified domain name, such as contoso.onmicrosoft.com.',
      );

    // ---- connecting a mailbox for sending ----
    case 'mail_oauth_cancelled':
      return t('Cancelled.');
    case 'mail_oauth_expired':
      return t('Expired. Please connect again.');
    case 'mail_oauth_bad_state':
      return t('Could not be verified. Please connect again.');
    case 'mail_oauth_no_code':
      return t('No authorization code.');
    case 'mail_oauth_token_exchange':
      return t('Token exchange failed.');
    case 'mail_oauth_provider':
      return t('The provider refused the connection.');
    // ---- sending mail, once a mailbox is connected ----
    case 'mail_not_configured':
      return t('No mail delivery is configured. Set up SMTP, or connect Google or Microsoft.');
    case 'mail_not_connected':
      return t('No mail provider is connected.');
    case 'mail_oauth_no_client':
      return t('Enter the client ID and secret in the Access tab first.');
    case 'mail_refresh_failed':
      return withDetail(t('The connection to the mailbox has expired. Connect it again.'));
    case 'mail_send_failed':
      return withDetail(t('The provider refused to send the message.'));

    case 'mail_oauth_no_refresh':
      return t(
        'No refresh token received. Remove the access in your account settings and connect again.',
      );

    // ---- other ----
    case 'impact_unavailable':
      return t('The consequences of this deletion could not be determined. Please try again.');
    case 'file_too_large':
      return fallback; // already built and translated on this side
    default:
      return fallback;
  }
}
