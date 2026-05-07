# Security Policy

## Reporting a vulnerability

Please do not publish vulnerability details, exploit steps, tokens, credentials, private configuration, or generated private data in a public issue, pull request, discussion, or commit.

This project does not currently publish a dedicated security email address. If you need to report a security issue, use GitHub's private vulnerability reporting for this repository if it is available from the repository Security tab.

If private vulnerability reporting is unavailable, open a minimal public GitHub issue so a maintainer can arrange private follow-up. Keep the public issue limited to:

* The affected area at a high level.
* A statement that you can share details privately with a maintainer.
* No secrets, private feed or source details, API credentials, `.env` values, `configs/config.yaml`, logs, or generated briefing data.
* No step-by-step exploit instructions or weaponized proof-of-concept details.

## Supported scope

Security fixes are generally handled for the current `main` branch and the latest released version when releases are available. Older unreleased snapshots or local forks may not receive separate fixes.

## Project security boundaries

`news-briefing` is a local-first Go CLI/tool that fetches configured news sources, filters articles by keywords and time windows, and generates briefing output through a locally configured AI CLI. It stores local configuration, generated Markdown output, seen-state data, and logs according to the repository configuration and runtime options.

Important boundaries and assumptions:

* Put sensitive values such as email auth codes in `.env`, not in YAML or command examples.
* Keep structured local configuration in `configs/config.yaml`; commit only `configs/config.example.yaml`.
* Do not commit `.env`, `configs/config.yaml`, generated output, seen-state data, logs, private feed/source lists, or other private runtime data.
* Treat source URLs, keywords, AI CLI configuration, generated briefing content, state files, email settings, and logs as potentially private.
* Review any issue, pull request, log excerpt, screenshot, shell output, or sample config for secrets and private data before publishing it.

## Secret handling

Before contributing, run the local checks and secret scanner when possible:

```bash
bash ./scripts/ci-local.sh
bash ./scripts/secret-scan.sh --current --history
```

If you accidentally commit or publish a secret, rotate or revoke it immediately. Removing it from a later commit is not enough once it has been exposed.
