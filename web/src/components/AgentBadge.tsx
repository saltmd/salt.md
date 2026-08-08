import { Bot } from 'lucide-react';
import { agentColor, agentLogo, agentName, isFresh, minutesSince, useAgentPresence } from '../agentPresence';
import type { AgentWork } from '../agentPresence';
import { t, plural } from '../i18n';

// The agent's own logo, from the same files the connect dialog shows — the mark
// you saw while setting it up is the mark you see while it works. Anything
// without one gets the neutral robot rather than a lookalike.
export function AgentMark({ agent, size }: { agent: string; size: number }) {
  const logo = agentLogo(agent);
  if (!logo) return <Bot size={size} />;
  return (
    <img
      className={'agent-mark' + (logo.mono ? ' agent-img--mono' : '')}
      src={'/agents/' + logo.file}
      style={{ width: size, height: size }}
      alt=""
    />
  );
}

// "Claude · via Jeremia · here for 2h 14m · last seen 47 min ago".
//
// Two timestamps rather than one claim. The server cannot know whether an agent
// is still working — an agent has no clock and cannot report in — so the badge
// says what IS known and lets the reader judge. A faded badge is not "probably
// gone", it is "has not called in a while", and for a three-hour job that is
// exactly right.
function duration(minutes: number): string {
  if (minutes < 60) return plural(minutes, '{n} min', '{n} min');
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m === 0 ? `${h} h` : `${h} h ${m} min`;
}

function title(w: AgentWork): string {
  const bits = [
    `${agentName(w)} · ${t('via {name}').replace('{name}', w.accountName)}`,
    t('here for {time}').replace('{time}', duration(minutesSince(w.startedAt))),
    isFresh(w)
      ? t('active just now')
      : t('last seen {time} ago').replace('{time}', duration(minutesSince(w.lastSeen))),
  ];
  if (w.note) bits.splice(1, 0, w.note);
  if (w.expectedMinutes > 0) {
    bits.push(t('checked in for about {time}').replace('{time}', duration(w.expectedMinutes)));
  }
  return bits.join(' · ');
}

/** The badges for one page, for the page topbar. */
export function AgentPresence({ pageId }: { pageId: string }) {
  const working = useAgentPresence(pageId);
  if (working.length === 0) return null;
  return (
    <div className="agent-presence">
      {working.map((w) => (
        <span
          key={w.agent + w.accountName}
          className={'agent-work' + (isFresh(w) ? ' fresh' : '')}
          style={{ borderColor: agentColor(w.agent), color: agentColor(w.agent) }}
          title={title(w)}
        >
          <AgentMark agent={w.agent} size={14} />
          <span className="agent-work-name">{agentName(w)}</span>
          {/* The note only when one agent is here. With two, the pair pushed the
              breadcrumb off the topbar — and the note is in the tooltip anyway,
              where it is readable rather than cut off after four words. */}
          {w.note && working.length === 1 && <span className="agent-work-note">{w.note}</span>}
        </span>
      ))}
    </div>
  );
}

/** The small mark for a board card — no room for a note there. */
export function AgentDot({ pageId }: { pageId: string }) {
  const working = useAgentPresence(pageId);
  if (working.length === 0) return null;
  const w = working[0];
  return (
    <span
      className={'agent-dot' + (isFresh(w) ? ' fresh' : '')}
      style={{ background: agentColor(w.agent) }}
      title={title(w)}
    >
      <AgentMark agent={w.agent} size={11} />
    </span>
  );
}
