# Automatic deployment from GitHub

Pushing to `main` runs `.github/workflows/deploy.yml`. Go tests, vet, command builds,
frontend type checks, unit/UI/proxy tests, and deployment regressions must pass
before a public prerelease `auto-<full-commit-sha>` becomes available. Pull requests
run checks without publishing a deployment. A failed check leaves production alone.

The release contains only `server`, `frontend-build.tar.gz`, and `manifest.json`.
The Linux amd64 backend is compiled with CGO disabled. The prebuilt frontend uses
same-origin `/api/v1`, and its Node server uses built-in modules. No production
configuration, host address, SSH key, application credential, or database data is
included. Runtime settings remain in server-local files. GitHub Actions runs on
GitHub-hosted machines; no Actions runner or GitHub token is installed on production.

## Server installation

Prerequisites: an existing healthy systemd deployment of `vidlens` and `vidlens-web`,
Linux amd64, Python 3.9+, Bash, git, curl, tar, Node.js, and outbound HTTPS access to
GitHub and its release asset hosts. The repository and deployment releases must be
public. This mode requires same-origin uploads; a custom upload origin must be
handled separately before enabling prebuilt deployment.

Install `deploy/pull_deploy.py`, `deploy/server-deploy.sh`, and
`deploy/frontend-deploy.sh` into `/usr/local/lib/vidlens-deploy/`, owned by root and
not writable by other users. Place `deploy/vidlens-deploy.service` and
`deploy/vidlens-deploy.timer` into `/etc/systemd/system/`.

Create `/etc/vidlens-deploy.json` with mode `0600`. The paths and ports below are
examples; use the local deployment's actual values. Do not commit this file.

```json
{
  "repository": "OWNER/REPOSITORY",
  "branch": "main",
  "deployment_path": "/srv/vidlens",
  "state_dir": "/var/lib/vidlens-deploy",
  "scripts_dir": "/usr/local/lib/vidlens-deploy",
  "runtime_generation": "postgres-pgvector-v1",
  "backend_service": "vidlens.service",
  "api_base": "http://127.0.0.1:8080",
  "backend_health_url": "http://127.0.0.1:8080/readyz",
  "frontend_health_url": "http://127.0.0.1:3000/index.html"
}
```

Validate the next release without activating it, then enable polling:

```sh
python3 /usr/local/lib/vidlens-deploy/pull_deploy.py --check
systemctl daemon-reload
systemctl enable --now vidlens-deploy.timer
```

## Deployment behavior

The server checks `main` every two minutes (plus up to ten seconds of jitter) and
downloads the release for that exact SHA anonymously. Draft releases are published
only after all assets are uploaded. If the release is not available, the running
version remains unchanged. The server checks SHA, runtime generation, asset sizes,
SHA256 hashes, and archive paths/types before calling either deployment script. It
checks `main` again after downloading to avoid activating a superseded release.

A local file lock prevents simultaneous automatic deployments. Before activating,
the updater checks both live services and saves a server-local copy of both program
versions under `.logs/auto-deploy-backups/`. It uses the existing deployment scripts
to restart services and check health. On deployment failure, it attempts to restore
both program versions and checks their health. Failed commits are remembered and
are not repeatedly activated by the timer; push a fix or explicitly retry locally.
`/var/lib/vidlens-deploy/state.json` records the last successful and failed SHAs.

This is program rollback. Startup database migrations are not reversed. Keep schema
changes compatible with the preceding program version. Service restarts can
interrupt active requests; this is not a zero-downtime deployment system. After a
successful deployment, the last five automatic program backups are retained;
manual backups are left alone. Public deployment releases accumulate and can be
removed when no longer needed, retaining the previous working version. Manual deployments should
stop the timer and wait for `vidlens-deploy.service` to finish before replacing files.

## Operations

```sh
systemctl list-timers vidlens-deploy.timer
systemctl status vidlens-deploy.service
journalctl -u vidlens-deploy.service -n 40 --no-pager
cat /var/lib/vidlens-deploy/state.json

# Retry a previously failed main SHA once using the same deployment lock.
python3 /usr/local/lib/vidlens-deploy/pull_deploy.py --retry

# Pause future automatic deployments (an already running deployment finishes).
systemctl stop vidlens-deploy.timer
# Resume them.
systemctl start vidlens-deploy.timer
```

The Actions run checks/builds/publishes the release; production activation is shown
in the server journal, not in the Actions job. Do not send local logs or settings
back to a public Actions run. GitHub receives outbound download requests, so this
design does not hide the server's network source IP from GitHub itself.
