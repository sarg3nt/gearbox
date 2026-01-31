#!/bin/bash
#
# install-gearbox-agent.sh
#
# Downloads and installs gearbox-agent from GitHub releases or a local web server.
# Supports both initial installation and updates.
#
# Usage:
#   # Install latest from GitHub (when repo is public)
#   ./install-gearbox-agent.sh
#
#   # Install specific version from GitHub
#   ./install-gearbox-agent.sh v1.0.0
#
#   # Install from local web server (for private repo / provisioning)
#   GEARBOX_AGENT_URL="http://10.0.0.1/gearbox-agent" ./install-gearbox-agent.sh
#
# Environment variables:
#   GEARBOX_AGENT_URL     - Direct URL to binary (bypasses GitHub release download)
#   GEARBOX_AGENT_VERSION - Version to install (default: latest)
#   GITHUB_REPO           - GitHub repository (default: sarg3nt/gearbox-agent)
#   INSTALL_DIR           - Installation directory (default: /usr/local/bin)
#   SERVICE_DIR           - Systemd service directory (default: /etc/systemd/system)
#   DATA_DIR              - Data directory (default: /var/lib/gearbox-agent)
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
GITHUB_REPO="${GITHUB_REPO:-sarg3nt/gearbox-agent}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
SERVICE_DIR="${SERVICE_DIR:-/etc/systemd/system}"
DATA_DIR="${DATA_DIR:-/var/lib/gearbox-agent}"
BINARY_NAME="gearbox-agent"
SERVICE_NAME="gearbox-agent"

# Detect architecture
detect_arch() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64)  echo "amd64" ;;
        aarch64) echo "arm64" ;;
        arm64)   echo "arm64" ;;
        *)
            echo -e "${RED}Error: Unsupported architecture: $arch${NC}" >&2
            exit 1
            ;;
    esac
}

# Detect OS
detect_os() {
    local os
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$os" in
        linux) echo "linux" ;;
        *)
            echo -e "${RED}Error: Unsupported OS: $os (only Linux is supported)${NC}" >&2
            exit 1
            ;;
    esac
}

# Get latest version from GitHub API
get_latest_version() {
    local version
    version=$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$version" ]; then
        echo -e "${RED}Error: Could not determine latest version from GitHub${NC}" >&2
        echo -e "${YELLOW}Hint: If the repo is private, set GEARBOX_AGENT_URL to a direct download URL${NC}" >&2
        exit 1
    fi
    echo "$version"
}

# Download and verify from GitHub releases
download_from_github() {
    local version="$1"
    local os="$2"
    local arch="$3"
    local version_num="${version#v}"  # Strip 'v' prefix
    local archive_name="gearbox-agent_${version_num}_${os}_${arch}.tar.gz"
    local download_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/${archive_name}"
    local checksums_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/checksums.txt"

    echo -e "${GREEN}Downloading ${archive_name}...${NC}"
    curl -fsSL -o "/tmp/${archive_name}" "$download_url"

    echo -e "${GREEN}Downloading checksums...${NC}"
    curl -fsSL -o "/tmp/checksums.txt" "$checksums_url"

    echo -e "${GREEN}Verifying checksum...${NC}"
    cd /tmp
    if grep "${archive_name}" checksums.txt | sha256sum --check --status; then
        echo -e "${GREEN}Checksum verified${NC}"
    else
        echo -e "${RED}Error: Checksum verification failed!${NC}" >&2
        rm -f "/tmp/${archive_name}" "/tmp/checksums.txt"
        exit 1
    fi

    echo -e "${GREEN}Extracting...${NC}"
    tar -xzf "/tmp/${archive_name}" -C /tmp

    # Move files to their destinations
    local extract_dir="/tmp/gearbox-agent_${version_num}_${os}_${arch}"

    echo "$extract_dir"
}

# Download from direct URL (for private repo / local web server)
download_from_url() {
    local url="$1"

    echo -e "${GREEN}Downloading from ${url}...${NC}"
    curl -fsSL -o "/tmp/${BINARY_NAME}" "$url"
    chmod +x "/tmp/${BINARY_NAME}"

    echo "/tmp"
}

# Install the binary
install_binary() {
    local source_dir="$1"
    local binary_path="${source_dir}/${BINARY_NAME}"

    if [ ! -f "$binary_path" ]; then
        echo -e "${RED}Error: Binary not found at ${binary_path}${NC}" >&2
        exit 1
    fi

    echo -e "${GREEN}Installing ${BINARY_NAME} to ${INSTALL_DIR}...${NC}"

    # Stop service if running
    if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
        echo -e "${YELLOW}Stopping ${SERVICE_NAME} service...${NC}"
        systemctl stop "${SERVICE_NAME}"
    fi

    # Install binary
    cp "$binary_path" "${INSTALL_DIR}/${BINARY_NAME}"
    chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

    echo -e "${GREEN}Binary installed to ${INSTALL_DIR}/${BINARY_NAME}${NC}"
}

# Install systemd service
install_service() {
    local source_dir="$1"
    local service_file="${source_dir}/${SERVICE_NAME}.service"

    # Check if service file exists in the extracted archive
    if [ -f "$service_file" ]; then
        echo -e "${GREEN}Installing systemd service...${NC}"
        cp "$service_file" "${SERVICE_DIR}/${SERVICE_NAME}.service"
    elif [ ! -f "${SERVICE_DIR}/${SERVICE_NAME}.service" ]; then
        # Create a default service file if none exists
        echo -e "${YELLOW}Creating default systemd service file...${NC}"
        cat > "${SERVICE_DIR}/${SERVICE_NAME}.service" << 'EOF'
[Unit]
Description=Gearbox Agent - Server monitoring and management agent
Documentation=https://github.com/sarg3nt/ubuntu-ha-proxy-install
After=network-online.target haproxy.service
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root

# Main executable
ExecStart=/usr/local/bin/gearbox-agent

# Environment variables (can be overridden in /etc/default/gearbox-agent)
Environment=GEARBOX_AGENT_LISTEN=0.0.0.0:8405
Environment=GEARBOX_AGENT_DATA_DIR=/var/lib/gearbox-agent
Environment=GEARBOX_AGENT_LOG_LEVEL=info

# Optional: Load additional environment from file
EnvironmentFile=-/etc/default/gearbox-agent

# Restart policy
Restart=always
RestartSec=5

# Security hardening
NoNewPrivileges=false
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=/var/lib/gearbox-agent /etc/haproxy /etc/letsencrypt

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=gearbox-agent

[Install]
WantedBy=multi-user.target
EOF
    fi

    # Create data directory
    mkdir -p "${DATA_DIR}"

    # Reload systemd
    systemctl daemon-reload
}

# Enable and start service
enable_service() {
    echo -e "${GREEN}Enabling ${SERVICE_NAME} service...${NC}"
    systemctl enable "${SERVICE_NAME}"

    echo -e "${GREEN}Starting ${SERVICE_NAME} service...${NC}"
    systemctl start "${SERVICE_NAME}"

    # Check status
    if systemctl is-active --quiet "${SERVICE_NAME}"; then
        echo -e "${GREEN}${SERVICE_NAME} is running${NC}"
    else
        echo -e "${RED}Warning: ${SERVICE_NAME} failed to start. Check logs with: journalctl -u ${SERVICE_NAME}${NC}"
    fi
}

# Cleanup temporary files
cleanup() {
    echo -e "${GREEN}Cleaning up...${NC}"
    rm -rf /tmp/gearbox-agent* /tmp/checksums.txt
}

# Print version info
print_version() {
    local installed_version
    if [ -x "${INSTALL_DIR}/${BINARY_NAME}" ]; then
        installed_version=$("${INSTALL_DIR}/${BINARY_NAME}" --version 2>/dev/null || echo "unknown")
        echo -e "${GREEN}Installed version: ${installed_version}${NC}"
    fi
}

# Main
main() {
    local version="${1:-${GEARBOX_AGENT_VERSION:-latest}}"
    local os
    local arch
    local source_dir

    os=$(detect_os)
    arch=$(detect_arch)

    echo -e "${GREEN}=== Gearbox Agent Installer ===${NC}"
    echo -e "OS: ${os}, Arch: ${arch}"

    # Check if we're using a direct URL or GitHub releases
    if [ -n "${GEARBOX_AGENT_URL:-}" ]; then
        echo -e "${GREEN}Using direct URL: ${GEARBOX_AGENT_URL}${NC}"
        source_dir=$(download_from_url "$GEARBOX_AGENT_URL")
    else
        # Get version if 'latest' was specified
        if [ "$version" = "latest" ]; then
            echo -e "${GREEN}Fetching latest version...${NC}"
            version=$(get_latest_version)
        fi
        echo -e "${GREEN}Version: ${version}${NC}"
        source_dir=$(download_from_github "$version" "$os" "$arch")
    fi

    # Install
    install_binary "$source_dir"
    install_service "$source_dir"
    enable_service

    # Cleanup
    cleanup

    # Print version
    print_version

    echo -e "${GREEN}=== Installation complete ===${NC}"
}

main "$@"
