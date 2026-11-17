# Repository instructions

## Standing rules

- **Merge pull requests when they work.** The owner set this rule on 2026-11-16. When the
  checks for a pull request pass and you verified the change by running it, merge the pull
  request into `main` without asking. Use a squash merge and delete the branch.
- Do not push directly to `main`. Use a feature branch and a pull request.
- Do not create or change anything in a cloud account before the owner approves the expected
  cost.

## Orientation

- `docs/PLAN.md` is the source of truth for the architecture and the build order.
- `docs/HANDOFF.md` records the verified state of the build.
- `platform/CONTRACT.md` is the interface for the OpenTofu layers on each target.
- `apps/paperboat/SPEC.md` is the interface for the managed workload.
