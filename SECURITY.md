# Security Policy

## Reporting a vulnerability

Please report suspected vulnerabilities privately to
**security@privasys.org**. Do not open public issues for security
reports. We aim to acknowledge reports within 48 hours and to keep you
informed of progress; coordinated disclosure timelines are agreed per
report.

Please include what you can: affected commit or image digest,
reproduction steps, and an impact assessment.

## Scope

This template makes a small number of claims. A way to break any of them
is what we most want to hear about.

- **Private means private.** A way for a request that arrived in the
  clear to reach `/api/v1/me` or `/api/v1/events`, or for either to
  answer anything but 403 when `X-Privasys-Transport` is `public`.
- **Public means anonymous.** A public response that reflects a
  credential, an identity, or anything that could identify a visitor.
- **The webhook checks its signature.** A body accepted without a valid
  HMAC under the configured secret, or a comparison that is not constant
  time.
- **The shell contains no data.** Anything served on the static surface
  that is not code or text present in the image.

Out of scope: denial of service through resource exhaustion (the
runtime's per-client rate limit bounds the public surface; the private
surface is bounded by the session and spend machinery), and the
behaviour of the Privasys runtime and gateway themselves, which have
their own reporting channel at the same address.

## Supported versions

The `main` branch and the most recent tagged release.
