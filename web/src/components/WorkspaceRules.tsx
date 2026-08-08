import { useState } from 'react';
import { api } from '../api';
import Portal from './Portal';
import { useExclusiveModal } from '../modal';
import { toast } from '../toast';
import { t } from '../i18n';
import { formatMoment } from '../format';

// The workspace rules: working conventions an admin writes down once — agents
// receive the same text over MCP (get_workspace) before they work here, and
// members read it in this dialog. Editing stays with admins, and only through
// the browser: the server refuses API tokens on the rules routes, so an agent
// can never rewrite its own guardrails. What an agent CAN do is propose a
// draft (propose_workspace_rules); it lands here, inert, until an admin loads
// it into the editor and saves — or dismisses it.
export default function WorkspaceRules({
  workspaceId,
  initial,
  proposal: initialProposal,
  proposalBy,
  proposalAt,
  canEdit,
  onClose,
  onSaved,
}: {
  workspaceId: string;
  initial: string;
  proposal: string;
  proposalBy: string;
  proposalAt: string;
  canEdit: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [text, setText] = useState(initial);
  const [proposal, setProposal] = useState(initialProposal);
  const [saving, setSaving] = useState(false);
  useExclusiveModal(onClose);

  const save = async () => {
    setSaving(true);
    try {
      await api.setWorkspaceRules(workspaceId, text.trim());
      onSaved();
      toast(t('Workspace rules saved'));
      onClose();
    } catch (e) {
      toast((e as Error).message || t('Saving failed'));
    } finally {
      setSaving(false);
    }
  };

  const dismissProposal = async () => {
    try {
      await api.dismissRulesProposal(workspaceId);
      setProposal('');
      onSaved();
      toast(t('Proposal dismissed'));
    } catch (e) {
      toast((e as Error).message || t('Saving failed'));
    }
  };

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog" role="dialog" aria-modal="true" aria-label={t('Workspace rules')}>
          <h2>{t('Workspace rules')}</h2>
          <p className="dialog-hint">
            {t('Conventions for working in this workspace. Agents receive them over MCP before they write here; members can read them too.')}
          </p>
          {proposal && canEdit && (
            <div className="ws-rules-proposal">
              <div className="ws-rules-proposal-head">
                {t('Proposed by {name} — {when}', { name: proposalBy, when: formatMoment(proposalAt, 'datetime') })}
              </div>
              <div className="ws-rules-view">{proposal}</div>
              <div className="dialog-buttons">
                <button className="btn" onClick={() => void dismissProposal()}>{t('Dismiss proposal')}</button>
                <button className="btn primary" onClick={() => setText(proposal)}>{t('Load into editor')}</button>
              </div>
            </div>
          )}
          {canEdit ? (
            <>
              <textarea
                className="ws-rules-input"
                rows={12}
                maxLength={16000}
                value={text}
                placeholder={t('e.g. Invoices go into Finance/Inbox. Titles start with the date. Never edit the Handbook section.')}
                onChange={(e) => setText(e.target.value)}
              />
              <div className="dialog-buttons">
                <button className="btn" onClick={onClose}>{t('Cancel')}</button>
                <button
                  className="btn primary"
                  disabled={saving || text.trim() === initial.trim()}
                  onClick={() => void save()}
                >
                  {t('Save')}
                </button>
              </div>
            </>
          ) : (
            <>
              {initial.trim() ? (
                <div className="ws-rules-view">{initial}</div>
              ) : (
                <div className="dialog-hint">{t('No rules yet — a workspace admin can add them.')}</div>
              )}
              <button className="btn dialog-close" onClick={onClose}>{t('Close')}</button>
            </>
          )}
        </div>
      </div>
    </Portal>
  );
}
