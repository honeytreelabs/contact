# contact
Process contact data provided by visitors of our website

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
