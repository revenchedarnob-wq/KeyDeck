# Developer Workflow

## Canonical source

Use the GitHub repository as the editable source of truth:

```powershell
git clone https://github.com/revenchedarnob-wq/KeyDeck.git A:\KeyDeck-Workspace\repo
cd A:\KeyDeck-Workspace\repo
```

Use `work/*` branches for changes. Do not resume engineering from random ZIP extractions, Downloads, Desktop, or Music folders.

## Daily commands

```powershell
.\KEYDECK.ps1 status
.\KEYDECK.ps1 fast
.\KEYDECK.ps1 proof-registry
```

Run deeper replay when preparing proof/release work:

```powershell
.\KEYDECK.ps1 deep
```

## Evidence rule

A feature is not complete because code exists. Completion requires tests, proof runners, evidence files, receipts, reports, or reproducible commands.

## Large artifacts

Keep giant archives out of normal Git history. Use GitHub Releases for versioned deliverables and Google Drive for raw archive vault material.
