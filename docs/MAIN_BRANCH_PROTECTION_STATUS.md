# Main Branch Protection Status

Branch protection/ruleset configuration is intentionally handled after CI is merged and confirmed working. Until then, this document records the required target state:

- Block force pushes to `main`.
- Block deletion of `main`.
- Encourage pull-request based changes.
- Require relevant CI checks where the repository plan supports it.
- Avoid locking out the repository owner.

Actual GitHub support and enforcement status must be inspected with authenticated `gh` after the automation PR is merged.
