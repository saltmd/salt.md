# Security

## Reporting a vulnerability

Write to **dev@salt.md**, or open a private advisory through GitHub's
[report a vulnerability](https://github.com/saltmd/salt.md/security/advisories/new)
form. Please do not open a public issue for a security problem.

You will get an acknowledgement within 72 hours. salt.md is maintained by one
person, so a fix takes as long as it takes. You will be told what is happening
rather than left in silence. There is no bounty programme.

Please include what you found, how to reproduce it, and what an attacker could
do with it. A proof of concept helps and is never required.

## What is in scope

The server, the frontend, the MCP surface, the desktop application, and the
install script in this repository.

Out of scope: an instance somebody else runs, and how they have configured it.
If you found something on a salt.md instance that is not ours, tell its
operator.

## What we ask

Do not test against instances you do not own. A self-hosted product is easy to
run locally with one binary and one command, and that is the right place to
look.

## Supported versions

The latest release. salt.md is early and moves quickly; there are no long-term
support branches yet.
