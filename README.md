<div align="center">

<img src="assets/logo_contact.png" alt="Contact" width="260">

# Contact

*Process contact data provided by visitors of our website.*

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](go.mod)
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
