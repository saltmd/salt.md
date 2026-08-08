# Contributing

Issues and pull requests are welcome. This page says what to expect, in plain
words, including the part people are right to ask about.

## Before a big change

Open an issue first. salt.md has opinions — one binary, no external services,
English source, everything derived from the code rather than remembered — and a
pull request that runs against one of them is a waste of your evening. A short
issue costs nothing and saves that.

Small fixes need no ceremony. Send them.

## Running it

```sh
make build      # frontend and backend, with the frontend embedded
./salt          # http://localhost:8420
```

While developing, run the two halves separately:

```sh
SALT_DATA=/tmp/salt-dev SALT_ADDR=:8420 go run .
cd web && npx vite
```

## What has to pass

```sh
go test ./...
cd web && npm run check
```

`npm run check` is not a formality and it is not only types. It fails on an
unwrapped user-facing string, on German anywhere in the source, on a wiki page
that names a tool which no longer exists, and on a screenshot whose component
has changed since it was taken. It runs inside `npm run build`, so a failing
check fails the build. That is deliberate.

Source text is English and doubles as the translation key. Comments are English
too.

## The CLA, and why

A pull request needs a signed [Contributor License Agreement](CLA.md). A bot
asks for it on your first one; signing is a comment, once, and it covers
everything you send afterwards.

We would rather say why than let you guess. salt.md is AGPL-3.0 and stays that
way. The plan is to sell a hosted version later, and possibly a commercial
licence for companies that cannot use AGPL internally. Offering that licence
requires holding the rights to all of the code — which is impossible if forty
people each hold rights to their own patch and some of them cannot be reached
in two years.

So the CLA asks you to grant those rights, and it says so plainly instead of
burying it. What it does **not** do is take your work away from you: you keep
your copyright, and everything you contribute stays available under AGPL-3.0 to
everyone, including you.

If that is not a trade you want to make, that is a fair position. Open an issue
instead — a good bug report is worth more than most patches.

## Commit messages

Say what changed and why it was wrong before. The subject line is a sentence
somebody skimming the history should understand without opening the diff.
