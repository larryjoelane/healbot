---
name: incident-fix
description: Fix a production incident with a minimal patch and a regression test. Use when an issue or alert includes an exception, a stack trace, or a failing operation.
---

Follow `AGENTS.md` at the repository root. Do not copy its rules into this skill.

When this skill is invoked:

1. Read the diagnosis, the stack, and the acceptance criteria.
2. Add a regression test using this repository's existing test framework. It should fail before the fix and pass after.
3. Make the smallest change that satisfies `AGENTS.md`.
4. Do not edit CI workflows, secret files, infrastructure, or lockfiles.
5. Open a pull request and leave it for a human to merge.
