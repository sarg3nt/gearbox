# GitHub Webhook Setup

The gearbox-agent can receive GitHub webhooks to trigger immediate sync when changes are pushed to your configuration repository. This provides near-instant updates instead of waiting for the polling interval.

**Note:** When webhooks are enabled, polling is disabled by default. The agent will only sync when it receives a webhook from GitHub. To keep polling as a backup (recommended for production), set `HAPROXY_WEBHOOK_POLL_BACKUP=true`.

## Prerequisites

- gearbox-agent deployed and running with sync enabled
- GitHub repository with your Docker Compose configurations
- Public internet access to your HAProxy server on port 8405

## Step 1: Enable Webhooks on the Server

Add the webhook environment variable to the gearbox-agent configuration:

```bash
echo 'HAPROXY_WEBHOOK_ENABLED=true' | sudo tee -a /etc/default/gearbox-agent
```

**Optional:** To keep polling as a backup in case webhooks fail:

```bash
echo 'HAPROXY_WEBHOOK_POLL_BACKUP=true' | sudo tee -a /etc/default/gearbox-agent
```

**Optional:** To configure the polling interval (default is 60 seconds):

```bash
# Using minutes
echo 'HAPROXY_POLL_INTERVAL=5m' | sudo tee -a /etc/default/gearbox-agent

# Or using seconds (backward compatible)
echo 'HAPROXY_POLL_INTERVAL=300' | sudo tee -a /etc/default/gearbox-agent
```

Supported formats: `60` (seconds), `60s` (seconds), `5m` (minutes), `1h` (hours).

Restart the service:

```bash
sudo systemctl restart gearbox-agent
```

The service will generate a webhook secret on first start. View it in the logs:

```bash
sudo journalctl -u gearbox-agent -n 50 | grep -A2 "WEBHOOK SECRET"
```

Or retrieve it later:

```bash
sudo gearbox-agent --show-webhook-secret
```

## Step 2: Configure Firewall Rules

### nftables (on the HAProxy server)

The firewall rule should already be in place if you're using the standard configuration. Verify:

```bash
sudo nft list ruleset | grep -A1 "GitHub webhooks"
```

You should see a rule allowing GitHub's IP ranges on port 8405.

### UniFi (or other edge firewall)

Create a port forward rule:

| Setting      | Value                                  |
|--------------|----------------------------------------|
| Name         | GitHub Webhooks to HAProxy Agent       |
| Forward From | WAN                                    |
| Port         | 8405                                   |
| Forward To   | 10.30.0.10 (light-hugger PROXY_DMZ IP) |
| Port         | 8405                                   |
| Protocol     | TCP                                    |

**Optional but recommended**: Restrict source IPs to GitHub's webhook ranges:

- 192.30.252.0/22
- 185.199.108.0/22
- 140.82.112.0/20
- 143.55.64.0/20

These IPs are published at [https://api.github.com/meta](https://api.github.com/meta) (see the "hooks" section).

## Step 3: Configure GitHub Webhook

1. Go to your GitHub repository → Settings → Webhooks → Add webhook
2. Configure the webhook:
  | Setting          | Value                                                       |
  |------------------|-------------------------------------------------------------|
  | Payload URL      | `https://light-hugger.sarg3.net:8405/api/v1/webhook/github` |
  | Content type     | `application/json`                                          |
  | Secret           | (paste the webhook secret from Step 1)                      |
  | SSL verification | Disable (self-signed cert) or Enable (if using valid cert)  |
  | Events           | Just the `push` event                                       |
  | Active           | ✓ Checked                                                   |
3. Click "Add webhook"
4. GitHub will send a ping event. Check the "Recent Deliveries" tab to verify it succeeded.

## Step 4: Test the Webhook

1. Make a small change to a docker-compose.yml in your apps folder
2. Commit and push the change
3. Check the gearbox-agent logs:

```bash
sudo journalctl -u gearbox-agent -f
```

You should see:

```txt
WEBHOOK: Received push to your-org/your-repo (ref: refs/heads/main) from username - commit: abc1234
WEBHOOK: Triggered sync for push event
Webhook triggered sync...
```

## Troubleshooting

### Webhook returns 401 Unauthorized

- Verify the secret matches exactly (no trailing whitespace)
- Check that GitHub is sending the `X-Hub-Signature-256` header

### Webhook returns 404 Not Found

- Webhooks are not enabled. Add `HAPROXY_WEBHOOK_ENABLED=true` and restart.

### Connection timeout

- Check UniFi port forward rule
- Check nftables rules on the server
- Verify the server is listening: `curl -sk https://localhost:8405/health`

### SSL certificate errors

- GitHub may reject self-signed certificates. Either:
  - Disable SSL verification in the webhook settings (less secure)
  - Use a valid certificate from Let's Encrypt

## Security Considerations

- The webhook endpoint validates GitHub's HMAC-SHA256 signature
- Only GitHub's published IP ranges should be allowed through the firewall
- The webhook secret is stored with restricted permissions (0600)
- Failed signature attempts are logged

## API Endpoints

| Endpoint                 | Method | Auth             | Description                  |
|--------------------------|--------|------------------|------------------------------|
| `/api/v1/webhook/github` | POST   | GitHub signature | Receives webhook events      |
| `/api/v1/webhook/info`   | GET    | API key          | Shows webhook URL and status |
