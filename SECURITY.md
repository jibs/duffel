# Security Policy

## Deployment boundaries

Duffel is designed for trusted local-network environments.

- No authentication is built in.
- Do not expose Duffel directly to the public internet.
- If remote access is required, place Duffel behind an authenticated reverse proxy and network controls.

## Supported versions

Security fixes are applied to the latest `main` branch.

## Reporting a vulnerability

Please report security issues privately through your repository host's private security reporting channel (for example, private vulnerability advisories).

If private reporting is not available, open a minimal public issue that requests a private contact channel without posting exploit details.

When reporting, include:

- Affected endpoint/feature
- Reproduction steps
- Potential impact
- Suggested mitigation (if known)
