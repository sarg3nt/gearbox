# Configuration Management API

The HAProxy Agent provides REST API endpoints for managing both HAProxy and firewall (nftables) configurations. These endpoints allow reading, validating, updating, and restoring configurations with built-in backup functionality.

## HAProxy Configuration

### Read Configuration

```http
GET /api/v1/haproxy/config
```

Returns the current HAProxy configuration with section parsing and metadata.

**Response:**

```json
{
  "content": "global\n  log /dev/log local0\n  ...",
  "sections": [
    {
      "type": "global",
      "name": "",
      "start_line": 1,
      "end_line": 25,
      "is_auto_gen": false
    },
    {
      "type": "frontend",
      "name": "https_front",
      "start_line": 50,
      "end_line": 85,
      "is_auto_gen": false
    }
  ],
  "marker_ranges": [
    {
      "type": "routing",
      "start_line": 70,
      "end_line": 75
    },
    {
      "type": "backend",
      "start_line": 90,
      "end_line": 150
    }
  ],
  "sha256": "abc123...",
  "last_modified": "2024-01-15T10:30:00Z",
  "file_path": "/etc/haproxy/haproxy.cfg"
}
```

**Notes:**

- `sections` identifies configuration blocks (global, defaults, frontend, backend, listen)
- `marker_ranges` identifies auto-generated sections that should not be manually edited
- `sha256` is used for optimistic locking when updating

### Update Configuration

```http
POST /api/v1/haproxy/config
```

Updates the HAProxy configuration with validation and automatic backup.

**Request:**

```json
{
  "content": "global\n  log /dev/log local0\n  ...",
  "expected_sha": "abc123...",
  "backup_reason": "Added new backend",
  "dry_run": false
}
```

**Parameters:**

- `content` (required): The new configuration content
- `expected_sha` (optional): SHA256 of the expected current config for optimistic locking
- `backup_reason` (optional): Reason for the change (for audit trail)
- `dry_run` (optional): If true, validates only without applying

**Response:**

```json
{
  "success": true,
  "validation_output": "Configuration file is valid",
  "backup_path": "/etc/haproxy/backups/haproxy_20240115_103000_Added_new_backend.cfg",
  "warnings": [],
  "message": "Configuration updated and applied successfully",
  "new_sha256": "def456..."
}
```

**Validation:**

- Configuration is validated using `haproxy -c -f <temp_file>`
- If validation fails, the configuration is not applied
- If `expected_sha` doesn't match, a 409 Conflict is returned

### List Backups

```http
GET /api/v1/haproxy/config/backups
```

Returns a list of available configuration backups.

**Response:**

```json
{
  "backups": [
    {
      "id": "haproxy_20240115_103000_Added_new_backend.cfg",
      "filename": "haproxy_20240115_103000_Added_new_backend.cfg",
      "created_at": "2024-01-15T10:30:00Z",
      "reason": "Added_new_backend",
      "sha256": "abc123...",
      "size_bytes": 4096
    }
  ]
}
```

**Notes:**

- Backups are stored in `/etc/haproxy/backups/`
- The system keeps the last 20 backups automatically

### Restore from Backup

```http
POST /api/v1/haproxy/config/restore
```

Restores configuration from a backup file.

**Request:**

```json
{
  "backup_id": "haproxy_20240115_103000_Added_new_backend.cfg",
  "dry_run": false
}
```

**Response:**

```json
{
  "success": true,
  "message": "Configuration restored successfully",
  "validation_output": "Configuration file is valid",
  "new_sha256": "abc123..."
}
```

**Notes:**

- Before restoring, a backup of the current config is created with reason "pre-restore"
- The backup content is validated before being applied

## Firewall Configuration (nftables)

### Read Configuration

```http
GET /api/v1/firewall/config
```

Returns the current nftables configuration.

**Response:**

```json
{
  "content": "#!/usr/sbin/nft -f\n\ntable inet filter {\n  ...",
  "sections": [
    {
      "type": "table",
      "name": "inet filter",
      "start_line": 3,
      "end_line": 50
    },
    {
      "type": "chain",
      "name": "input",
      "start_line": 5,
      "end_line": 25
    }
  ],
  "sha256": "abc123...",
  "last_modified": "2024-01-15T10:30:00Z",
  "file_path": "/etc/nftables.conf"
}
```

### Update Configuration

```http
POST /api/v1/firewall/config
```

Updates the nftables configuration with validation.

**Request:**

```json
{
  "content": "#!/usr/sbin/nft -f\n\ntable inet filter {\n  ...",
  "expected_sha": "abc123...",
  "backup_reason": "Added rate limiting rules",
  "dry_run": false
}
```

**Validation:**

- Configuration is validated using `nft -c -f <temp_file>`
- Applied using `nft -f /etc/nftables.conf`

**Response:**

```json
{
  "success": true,
  "validation_output": "",
  "backup_path": "/etc/backups/nftables_20240115_103000_Added_rate_limiting_rules.conf",
  "warnings": [],
  "message": "Configuration updated and applied successfully",
  "new_sha256": "def456..."
}
```

### List Backups

```http
GET /api/v1/firewall/config/backups
```

Returns a list of available firewall configuration backups.

### Restore from Backup

```http
POST /api/v1/firewall/config/restore
```

Restores firewall configuration from a backup file.

## Error Handling

### Conflict (409)

Returned when `expected_sha` doesn't match the current configuration:

```json
{
  "success": false,
  "message": "Configuration has been modified since last read. Please reload and try again."
}
```

### Validation Failure

Returned when configuration validation fails:

```json
{
  "success": false,
  "validation_output": "[ALERT] 001/103015 (1234) : parsing [/tmp/haproxy-validate-123.cfg:45] : unknown keyword 'invalid_directive'",
  "message": "Configuration validation failed"
}
```

## Security Considerations

1. **Authentication**: All endpoints require API key authentication via `X-API-Key` header
2. **Optimistic Locking**: Use `expected_sha` to prevent concurrent edit conflicts
3. **Automatic Backups**: Changes always create a backup first
4. **Validation**: Configurations are always validated before being applied
5. **Rollback**: If applying a configuration fails, the system attempts to restore the backup

## Auto-Generated Sections

HAProxy configurations may contain auto-generated sections managed by `haproxy-autoconfig.py`:

```text
# BEGIN AUTO-GENERATED ROUTING RULES - DO NOT EDIT MANUALLY
# This section is managed by haproxy-autoconfig.py
# END AUTO-GENERATED ROUTING RULES
```

The API identifies these sections in the `marker_ranges` response field. The monitoring UI displays these as read-only to prevent accidental manual edits.
