# Repository instructions

This file is the canonical architecture guide for coding agents working in this repository.
Copilot coding agent reads it directly. Claude Code reads it because `CLAUDE.md` imports this path.
Edit this file when the rules change. Do not copy the rules into the agent entry points.

## Architecture

- Keep a change in the layer that already owns the behavior. A handler calls a service. A service does not reach around a repository that already exists for that data.
- Preserve the public contract of the function you are fixing. Change the body before adding a new type.
- Match the error handling already in the file. Return the same error type, add the same context, and do not drop an error the caller already handles.
- Do not add a framework, a dependency, or a configuration system in order to fix one incident.

## Incident fixes

- Reproduce the failure with a regression test in the same change as the fix.
- The test should fail on the current code and pass after the patch.
- Use the test framework and the directory layout this repository already uses.
- Name the test for the failure mode.
- Prefer the smallest diff that makes that test pass.
- Do not edit CI workflows, secret files, infrastructure definitions, or lockfiles unless the incident is about that file and a person asked for the edit.
- Do not merge the pull request. Leave it for a human review.

## Diff discipline

- No drive-by refactors, renames, or formatting of lines the fix does not need.
- No generated files unless the repository documents the command that regenerates them and you run that command.
- Explain the cause in a few sentences in the pull request or the issue comment.
