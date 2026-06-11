# Duffel Deployment

This is a generic systemd deployment recipe. Replace example hostnames, paths,
and usernames with values for your own environment.

## Production server

| Property | Example |
|----------|---------|
| Host | `example-host` |
| SSH | `ssh deploy@example-host` |
| Path | `/opt/duffel` |
| Port | `4386` |
| Service | `systemd: duffel.service` |
| Run as | `duffel` user |

## Deploying latest

Push your changes to `origin/main`, then:

```bash
make deploy
```

This SSHes to the server and restarts the systemd service. On restart, `start.sh` automatically:

1. `git fetch origin && git reset --hard origin/main`
2. `make setup` (installs/updates deps)
3. `make build` (compiles binary)
4. `exec ./duffel` (starts the server)

To override the target host:

```bash
DEPLOY_HOST=deploy@example-host make deploy
```

## Server environment (`.env`)

The service loads `/opt/duffel/.env` if it exists (`EnvironmentFile=-` in the unit). A typical `.env` contains:

```bash
DUFFEL_HOST=127.0.0.1
DUFFEL_PORT=4386
DUFFEL_DATA_DIR=/opt/duffel/data
```

`DUFFEL_SETUP_TOKEN` is only needed during first-time owner setup (see below). Remove it from `.env` once setup is complete.

`DUFFEL_HOST` controls the bind address. Set it to a private interface address to restrict access to that network. Leave it unset (or `""`) for local development to bind on all interfaces.

`DUFFEL_AUTH_ENABLED` defaults to `true`; omit it unless you need to change it.

If Duffel sits behind Tailscale Serve or another reverse proxy, make sure it forwards `X-Forwarded-Host` and `X-Forwarded-Proto` so OAuth metadata advertises the external URL. If your proxy cannot forward those headers, set `DUFFEL_AUTH_ISSUER` to the public origin explicitly.

## Auth credentials

Store owner credentials and setup tokens in your normal secret manager. Do not
commit `.env`, generated setup tokens, owner passwords, or bootstrap PATs.

Create and manage MCP access tokens from the in-app `#/_mcp` connector page.

## First-time server setup

```bash
# 1. Clone the repo
ssh deploy@example-host
sudo mkdir -p /opt/duffel
sudo chown "$USER":"$USER" /opt/duffel
git clone https://github.com/OWNER/duffel.git /opt/duffel
cd /opt/duffel

# 2. Generate a setup token and create .env
SETUP_TOKEN=$(openssl rand -base64 24 | tr -d '/+=')
cat > .env <<EOF
DUFFEL_HOST=127.0.0.1
DUFFEL_PORT=4386
DUFFEL_DATA_DIR=/opt/duffel/data
DUFFEL_SETUP_TOKEN=${SETUP_TOKEN}
EOF

# 3. Make start.sh executable
chmod +x ops/deploy/start.sh
cp ops/deploy/start.sh start.sh

# 4. Install systemd service (as root)
sudo cp ops/deploy/duffel.service /etc/systemd/system/duffel.service
sudo systemctl daemon-reload
sudo systemctl enable duffel
sudo systemctl start duffel

# 5. Wait for service to start, then create owner account
# (replace USERNAME and PASSWORD with your chosen values)
curl -s -X POST http://127.0.0.1:4386/oauth/setup \
  -H "Content-Type: application/json" \
  -d "{\"setupToken\":\"${SETUP_TOKEN}\",\"username\":\"USERNAME\",\"password\":\"PASSWORD\"}"
# returns {"status":"configured","bootstrap_token":"dpat_..."}

# 6. Store credentials in your secret manager

# 7. Remove setup token from .env
sed -i '/DUFFEL_SETUP_TOKEN/d' .env
```

## Checking service status

```bash
ssh deploy@example-host "systemctl status duffel"
ssh deploy@example-host "journalctl -u duffel -n 50"
```
