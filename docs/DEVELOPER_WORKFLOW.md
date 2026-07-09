# KeyDeck Developer Workflow

1. Work only in `A:\KeyDeck-Workspace\repo`.
2. Start from updated `main`: `git switch main; git pull --ff-only`.
3. Create focused `work/*` or `automation/*` branches.
4. Run `./KEYDECK.ps1 test` before pushing.
5. Use `./KEYDECK.ps1 test-deep` for broader deterministic validation.
6. Let GitHub Actions validate pull requests.
7. Merge only verified changes.
8. Use GitHub Releases for packaged binaries and normal release artifacts.
9. Use Google Drive through `scripts/drive/upload-drive-archive.ps1` for giant archives, premade infrastructure, manifests, audit material, proof packages, and continuation handovers.
10. Never resume development from random extracted ZIPs in Downloads, Desktop, or Music.

Do not commit live credentials, rclone configuration, private keys, OAuth tokens, local-only receipts, or large continuation archives into normal Git history.
