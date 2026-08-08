# fail2ban for salt.md

salt.md throttles wrong credentials by itself — 30 password attempts a minute
per address, and a separate budget for rejected API tokens. That happens inside
the process and stops when the process does. fail2ban puts the ban in the
firewall instead, where it costs the attacker a TCP connection rather than a
request.

Both are worth having. The in-process limit is the one that always works; the
jail is the one that makes a night of knocking expensive.

## What salt.md writes

One line per rejected credential, to stdout — which under systemd means the
journal:

```
auth: rejected password from 203.0.113.9
auth: rejected token from 203.0.113.9
```

The address is there because that is what gets banned. The email and the token
are deliberately NOT: this log ends up in journald, in log shipping and in
backups, and "who did what" belongs in the audit table behind a login.

## Behind a proxy or a Cloudflare tunnel

**Turn on "trust proxy" in the instance settings first**, or every visitor
arrives as the tunnel and the address in this line is always `127.0.0.1` — you
would ban the tunnel and lock out everybody.

And only turn it on when the tunnel is the ONLY way in. If the machine also
answers on the LAN, anything on that network can set `X-Forwarded-For` and both
the ban and the rate limit are bypassable from inside.

With Cloudflare in front, the ban belongs at Cloudflare rather than in the local
firewall — a banned address is still let through to the tunnel, because the
connection comes from `cloudflared`. Use the jail below on a machine reached
directly, and a Cloudflare WAF rate-limit rule on a tunnelled one.

## Install

```bash
sudo cp salt.conf /etc/fail2ban/filter.d/salt.conf
sudo cp jail.local /etc/fail2ban/jail.d/salt.conf
sudo systemctl reload fail2ban
sudo fail2ban-client status salt
```

**Check the filter against the real journal before trusting it.** A jail that
matches nothing looks exactly like a jail with nothing to do — the first version
of the filter here was written from the log format rather than from a real line
and would never have fired, because Go prefixes every line with its own
timestamp:

```bash
journalctl -u salt --since -7d | fail2ban-regex - /etc/fail2ban/filter.d/salt.conf
```
