# Getting Started with Gearbox

Welcome to Gearbox! This guide will walk you through setting up your first monitored server or workstation (we call these "Boxes").

## Overview

Gearbox is a plugin-based server monitoring and management platform that consists of two components:

- **Gearbox Dashboard** - Web interface for monitoring multiple servers (port 3000)
- **Gearbox Agent** - Lightweight service installed on each Box you want to monitor (port 8405)

## Prerequisites

- A Linux server or workstation to monitor (the "Box")
- Go 1.21+ installed on the Box (for building the agent)
- Network connectivity between the dashboard and the Box on port 8405

## Quick Start

### Step 1: Install Gearbox Dashboard

The dashboard is where you'll view and manage all your monitored Boxes.

```bash
# Clone the repository
git clone https://github.com/sarg3nt/gearbox.git
cd gearbox/gearbox

# Build the dashboard
make build

# Run the dashboard
./bin/gearbox
```

The dashboard will be available at `http://localhost:3000`

### Step 2: Install Gearbox Agent on Your Box

Choose the installation method that works best for your environment:

#### Option A: Docker Installation (Recommended)

The easiest way to get started with minimal dependencies:

```bash
# Pull the latest image
docker pull ghcr.io/sarg3nt/gearbox/gearbox-agent:latest

# Or use Docker Compose (see Step 5 for docker-compose.yml example)
```

See [gearbox-agent/docs/docker.md](../gearbox-agent/docs/docker.md) for complete Docker setup guide.

#### Option B: Binary Installation

For traditional systemd-based deployments:

```bash
# Clone the repository
git clone https://github.com/sarg3nt/gearbox.git
cd gearbox/gearbox-agent

# Build the agent
make build

# The agent binary will be in ./bin/gearbox-agent
```

### Step 3: Generate an API Key

1. Open the Gearbox dashboard at `http://localhost:3000`
2. Navigate to **Settings > Servers** and click **Add Server**
3. Click the **Generate API Key** button
4. Copy the generated API key to your clipboard
5. **Important:** Save this key somewhere safe - you won't be able to see it again!

### Step 4: Configure the Agent

Create a configuration file or set environment variables on your Box:

#### Option A: Environment Variables (Recommended)

```bash
export GEARBOX_API_KEY="your-generated-api-key-here"
export GEARBOX_PORT="8405"  # Optional, defaults to 8405
```

Add these to your shell profile (`~/.bashrc`, `~/.zshrc`) or create a systemd service file to make them persistent.

#### Option B: Configuration File

Create `/etc/gearbox-agent/config.yaml`:

```yaml
api_key: "your-generated-api-key-here"
port: 8405
```

### Step 5: Start the Agent

#### If Using Docker

**Using docker run:**

```bash
docker run -d \
  --name gearbox-agent \
  -p 8405:8405 \
  -v gearbox-agent-data:/var/lib/gearbox-agent \
  -e HAPROXY_AGENT_LOG_LEVEL=info \
  ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
```

**Using Docker Compose:**

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  gearbox-agent:
    image: ghcr.io/sarg3nt/gearbox/gearbox-agent:latest
    container_name: gearbox-agent
    restart: unless-stopped
    ports:
      - "8405:8405"
    volumes:
      - ./data:/var/lib/gearbox-agent
    environment:
      - HAPROXY_AGENT_LOG_LEVEL=info
```

Then start:

```bash
docker-compose up -d
```

#### If Using Binary

```bash
# Start the agent
cd gearbox-agent
./bin/gearbox-agent
```

The agent will start on port 8405 and begin collecting system metrics.

### Step 6: Add the Box to Your Dashboard

1. Return to the Gearbox dashboard
2. In the **Add Server** form, fill in:
   - **Server Name**: A friendly name (e.g., "Production Web Server")
   - **Server ID**: A unique identifier (e.g., "web-prod-01")
   - **Agent URL**: The URL to your agent (e.g., `http://192.168.1.100:8405`)
   - **API Key**: Paste the API key you generated earlier
3. Click **Test Connection** to verify connectivity
4. Click **Add Server** to save

### Step 7: Enable Plugins

After adding your first Box:

1. Navigate to **Settings > Plugins**
2. Enable the plugins you want to use:
   - **HAProxy** - HAProxy monitoring and statistics
   - **Metrics** - System metrics visualization
   - **Services** - Service management and monitoring
   - **Certificates** - TLS certificate tracking
   - **Logs** - Log aggregation and viewing
   - **Traffic** - Traffic analysis and visualization
   - **Alerts** - Alert management
   - **OS Updates** - OS package updates

When you enable a plugin, its page will be added to the navigation.

## Running as a System Service

### Systemd Service (Linux)

Create `/etc/systemd/system/gearbox-agent.service`:

```ini
[Unit]
Description=Gearbox Agent - Server Monitoring
After=network.target

[Service]
Type=simple
User=gearbox
WorkingDirectory=/opt/gearbox-agent
Environment="GEARBOX_API_KEY=your-api-key-here"
Environment="GEARBOX_PORT=8405"
ExecStart=/opt/gearbox-agent/bin/gearbox-agent
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable gearbox-agent
sudo systemctl start gearbox-agent
sudo systemctl status gearbox-agent
```

## Auto-Discovery

Gearbox Agent automatically discovers services running on your Box:

- **HAProxy** - Detects running HAProxy instances and collects stats
- **Docker** - Discovers Docker containers and images
- **System Services** - Monitors systemd services

No manual configuration required! Just enable the relevant plugins in the dashboard.

## Security Considerations

1. **API Key Security**: Treat your API key like a password. Never commit it to version control.
2. **Network Security**: Use a firewall to restrict access to port 8405 to only your dashboard server.
3. **HTTPS**: In production, run both the dashboard and agent behind a reverse proxy with HTTPS.
4. **Authentication**: The dashboard supports password authentication and WebAuthn. Enable it for production use.

## Troubleshooting

### Agent Won't Start

- Verify the `GEARBOX_API_KEY` environment variable is set
- Check if port 8405 is available: `sudo netstat -tulpn | grep 8405`
- Review agent logs for errors

### Connection Test Fails

- Verify the Agent URL is correct and accessible from the dashboard server
- Check firewall rules on both the dashboard and Box
- Confirm the API key matches between dashboard and agent
- Use `curl` to test: `curl http://your-box:8405/health`

### No Data Showing

- Wait 30-60 seconds for the first data collection cycle
- Verify the plugin is enabled in **Settings > Plugins**
- Check that the service you're trying to monitor is actually running on the Box
- Review agent logs for collection errors

## Next Steps

- **Configure Alerts**: Set up alert rules for critical metrics
- **Add More Boxes**: Repeat the process to monitor additional servers

## Getting Help

- **Documentation**: [GitHub Repository](https://github.com/sarg3nt/gearbox)
- **Issues**: [Report a Bug](https://github.com/sarg3nt/gearbox/issues)
- **Architecture**: See [docs/plugins.md](plugins.md) for plugin architecture details

## What's Next?

Now that you have Gearbox running, explore:

1. **Plugin Development**: Build your own monitoring plugins
2. **Advanced Configuration**: Explore environment variables and configuration options

Happy monitoring! 📊
