import { useState } from 'react';
import { api } from '../api';
import type { Workspace } from '../types';
import Portal from './Portal';
import { useExclusiveModal } from '../modal';
import { toast } from '../toast';
import { promptText } from '../dialog';
import { t } from '../i18n';
import {
  Bot,
  Download,
  FileText,
  Image as ImageIcon,
  LayoutList,
  Pencil,
  ScrollText,
  ShieldAlert,
  Trash2,
  Upload,
  Users,
} from 'lucide-react';

// Everything about ONE workspace, in one place.
//
// It used to be eighteen buttons in the workspace dropdown, in the order they
// were added: rename next to export next to a break-glass log next to delete.
// A menu is a list of ACTIONS you take now; most of these are SETTINGS you
// look at, compare and change. Those are different things and want different
// shapes.
//
// The switches live here inline, because reading them is the point — you should
// be able to see what this workspace is set to without clicking anything. The
// bigger screens (members, rules, files, break-glass) stay their own dialogs
// and are rows that open them: pulling them in would make this a second copy of
// four things.

interface Props {
  ws: Workspace;
  isOwner: boolean;
  onChanged: () => void;
  onOpenMembers: () => void;
  onOpenRules: () => void;
  onOpenFiles: () => void;
  onOpenBreakGlass: () => void;
  onOpenImage: () => void;
  onDelete: () => void;
  onImport: () => void;
  onClose: () => void;
}

export default function WorkspaceSettings({
  ws,
  isOwner,
  onChanged,
  onOpenMembers,
  onOpenRules,
  onOpenFiles,
  onOpenBreakGlass,
  onOpenImage,
  onDelete,
  onImport,
  onClose,
}: Props) {
  useExclusiveModal(onClose);
  const [busy, setBusy] = useState(false);
  // Empty means the default, everywhere: no third state to handle, and an
  // instance that never touched these reads as today's behaviour.
  const access = ws.agentAccess || 'open';
  const tree = ws.treeMode || 'split';

  const patch = async (body: Record<string, unknown>) => {
    if (busy) return;
    setBusy(true);
    try {
      await api.updateWorkspace(ws.id, body);
      onChanged();
    } catch (e) {
      toast((e as Error).message || t('Change not saved'));
    } finally {
      setBusy(false);
    }
  };

  const rename = async () => {
    const name = await promptText(t('Rename workspace'), {
      defaultValue: ws.name,
      placeholder: t('New name'),
      confirmText: t('Rename'),
    });
    const n = name?.trim();
    if (!n || n === ws.name) return;
    await patch({ name: n });
    toast(t('Workspace renamed'));
  };

  const row = (icon: React.ReactNode, label: string, hint: string, onClick: () => void, danger = false) => (
    <button className={'ws-row' + (danger ? ' danger' : '')} onClick={onClick}>
      <span className="ws-row-ic">{icon}</span>
      <span className="ws-row-main">
        <span className="ws-row-label">{label}</span>
        <span className="ws-row-hint">{hint}</span>
      </span>
    </button>
  );

  const choice = (
    checked: boolean,
    label: string,
    hint: string,
    onPick: () => void,
  ) => (
    <label className={'ws-choice' + (checked ? ' active' : '')}>
      <input type="radio" checked={checked} onChange={onPick} disabled={busy} />
      <span>
        <span className="ws-choice-label">{label}</span>
        <span className="ws-choice-hint">{hint}</span>
      </span>
    </label>
  );

  return (
    <Portal>
      <div className="modal-overlay" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
        <div className="dialog wide ws-dialog" role="dialog" aria-modal="true" aria-label={t('Workspace settings')}>
          <h2>{t('Workspace settings')}</h2>
          <p className="dialog-hint">{ws.name}</p>

          <div className="ws-body">
            <h3 className="ws-section">{t('General')}</h3>
            {row(<Pencil size={16} />, t('Name'), ws.name, () => void rename())}
            {row(<ImageIcon size={16} />, t('Picture'), t('Shown in the switcher and on cards'), onOpenImage)}

            <h3 className="ws-section">{t('Access')}</h3>
            {row(<Users size={16} />, t('Members'), t('Who is in, and in which role'), onOpenMembers)}

            {/* The opt-in. Its default IS the behaviour that exists today, so a
                workspace nobody configures behaves exactly as before. */}
            <div className="ws-setting">
              <div className="ws-setting-head">
                <Bot size={15} /> {t('What agents may do here')}
              </div>
              {choice(access === 'open', t('Anything they were granted'), t('Any connection that was given this workspace.'), () => void patch({ agentAccess: 'open' }))}
              {choice(access === 'strict', t('Only signed-in connections'), t('A permanent token is refused, even one naming this workspace. For confidential material.'), () => void patch({ agentAccess: 'strict' }))}
              {choice(access === 'closed', t('No agents at all'), t('Browser sessions only.'), () => void patch({ agentAccess: 'closed' }))}
            </div>

            {/* Was a toggle in the menu and nearly got lost in the move — it is
                a setting, not an action, and reads far better as one. Not
                offered for a personal space: "every new account joins my own
                area" is not a thing anybody means. */}
            {!ws.personal && (
              <label className="ws-switch">
                <input
                  type="checkbox"
                  checked={ws.autoJoin === true}
                  disabled={busy}
                  onChange={() => void patch({ autoJoin: !ws.autoJoin })}
                />
                <span>
                  <span className="ws-choice-label">{t('Open to every new user')}</span>
                  <span className="ws-choice-hint">
                    {t('Every newly created account automatically becomes a member of this workspace')}
                  </span>
                </span>
              </label>
            )}

            {isOwner && row(<ShieldAlert size={16} />, t('Emergency access log'), t('Who looked in as the instance owner — and why'), onOpenBreakGlass)}

            <h3 className="ws-section">{t('Layout')}</h3>
            {/* The answer to "in a documentation workspace the databases belong
                to their document". Both readings are right, for different
                workspaces — so it is asked rather than decided. */}
            <div className="ws-setting">
              <div className="ws-setting-head">
                <LayoutList size={15} /> {t('How the sidebar is arranged')}
              </div>
              {choice(tree === 'split', t('Documents and collections apart'), t('Two sections. Good when the databases are the point.'), () => void patch({ treeMode: 'split' }))}
              {choice(tree === 'mixed', t('One tree, filed where you put it'), t('A collection stays under its document. Good for documentation.'), () => void patch({ treeMode: 'mixed' }))}
            </div>

            <h3 className="ws-section">{t('Conventions')}</h3>
            {row(
              <ScrollText size={16} />,
              t('Workspace rules'),
              ws.rulesProposal
                ? t('A rules proposal is waiting for review')
                : t('Conventions everyone — especially agents — follows in this workspace'),
              onOpenRules,
            )}

            <h3 className="ws-section">{t('Data')}</h3>
            {row(<FileText size={16} />, t('Files'), t('Every uploaded file in this workspace, with the page carrying it'), onOpenFiles)}
            {row(<Download size={16} />, t('Export workspace'), t('Native archive — importable one to one'), () => api.download(`/api/workspaces/${ws.id}/export`))}
            {row(<FileText size={16} />, t('Export as Markdown'), t('Readable anywhere, without the databases'), () => api.download(`/api/export?workspace=${ws.id}`))}
            {row(<Upload size={16} />, t('Import workspace…'), t('A native archive from another instance'), onImport)}
            {row(<Trash2 size={16} />, t('Delete workspace'), t('Takes every page in it along'), onDelete, true)}
          </div>

          <div className="dialog-actions">
            <button className="btn-sm" onClick={onClose}>
              {t('Close')}
            </button>
          </div>
        </div>
      </div>
    </Portal>
  );
}
