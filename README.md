<div align="center">

<img src="assets/logo_contact.png" alt="Contact" width="260">

# Contact

*Process contact data provided by visitors of our website.*

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](go.mod)
[![Docker](https://img.shields.io/badge/Docker-supported-2496ED?logo=docker&logoColor=white)](https://www.docker.com/)
[![Last commit](https://img.shields.io/github/last-commit/honeytreelabs/contact?logo=github)](https://github.com/honeytreelabs/contact/commits/main)

**[Optional Cap CAPTCHA](#optional-cap-captcha)**

</div>

Process contact data provided by visitors of our website.

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
