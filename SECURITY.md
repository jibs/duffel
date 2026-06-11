# Security Policy

## Deployment boundaries

Duffel is designed for trusted local-network environments.

- OAuth authentication is built in and enabled by default.
- First owner setup is protected by `DUFFEL_SETUP_TOKEN`.
- API data endpoints and MCP require Bearer tokens.
- For public deployment, still use HTTPS/TLS termination and standard network controls.

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
