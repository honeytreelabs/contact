<div align="center">

<img src="assets/logo_contact.png" alt="Contact" width="260">

# Contact

*Process contact data provided by visitors of our website.*

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go 1.27](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![Docker](https://img.shields.io/badge/Docker-supported-2496ED?logo=docker&logoColor=white)](https://www.docker.com/)
[![CI](https://github.com/honeytreelabs/contact/actions/workflows/ci.yml/badge.svg)](https://github.com/honeytreelabs/contact/actions/workflows/ci.yml)
[![Security](https://github.com/honeytreelabs/contact/actions/workflows/security.yml/badge.svg)](https://github.com/honeytreelabs/contact/actions/workflows/security.yml)
[![Last commit](https://img.shields.io/github/last-commit/honeytreelabs/contact?logo=github)](https://github.com/honeytreelabs/contact/commits/main)

**[Development](#development)** | **[Optional Cap CAPTCHA](#optional-cap-captcha)**

</div>

Process contact data provided by visitors of our website.

## Development

Run the local check suite before submitting changes:

```sh
make check
```

Format the Go sources with:

```sh
make format
```

Audit dependencies and reachable code paths with:

```sh
make audit
```

Container builds use Podman by default. Override `CONTAINER` when another
compatible runtime should be used:

```sh
CONTAINER=docker make build
```

## Rootless Podman

The production image is suitable for rootless Podman. It runs as the
unprivileged `contact` user with UID/GID `10001`, listens on port `8080` by
default, and does not require writable application directories.

The sample Compose file is intentionally compatible with a restricted rootless
deployment: it uses a read-only root filesystem, drops all Linux capabilities,
sets `no-new-privileges`, and publishes `8080:8080`.

Operational notes:

- Ensure the host has subordinate UID/GID ranges configured for rootless Podman.
- Publish an unprivileged host port such as `8080`; use a reverse proxy for
  public `80`/`443` traffic.
- Allow outbound DNS, SMTP, and optional CAPTCHA verification traffic.
- If bind mounts are added later, make them readable or writable by container
  UID/GID `10001`, as required by the mounted path.

GitHub Actions run the check suite and container build for pushes to `main` and
pull requests. The security workflow runs `govulncheck` for pushes, pull
requests, manual dispatches, and a weekly scheduled audit. Dependabot opens
weekly grouped update pull requests for Go modules, GitHub Actions, and Docker
base images.

## Optional Cap CAPTCHA

Cap verification is disabled by default. Set these environment variables to
require a valid `cap-token` before contact requests are queued for email
delivery:

```text
CAP_ENABLED=true
CAP_API_ENDPOINT=https://cap.example.com/<site-key>/
CAP_SECRET=<site-secret>
CAP_VERIFY_TIMEOUT=5s
```

`CAP_SECRET` must only be configured on the contact service. The website should
only receive the public Cap endpoint and asset URLs.
