package database

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// initAlertsSchema creates the alerts tables.
func (d *DB) initAlertsSchema() error {
	schema := `
	-- Alert rules define conditions that trigger alerts
	CREATE TABLE IF NOT EXISTS alert_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		type TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		severity TEXT NOT NULL DEFAULT 'warning',
		condition TEXT,
		metric TEXT,
		threshold REAL,
		operator TEXT,
		duration INTEGER NOT NULL DEFAULT 0,
		cooldown_minutes INTEGER NOT NULL DEFAULT 5,
		notify_email INTEGER NOT NULL DEFAULT 0,
		notify_webhook INTEGER NOT NULL DEFAULT 0,
		notify_ui INTEGER NOT NULL DEFAULT 1,
		webhook_url TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_alert_rules_server
		ON alert_rules(server_id);
	CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled
		ON alert_rules(server_id, enabled);

	-- Alerts are instances of triggered alert rules
	CREATE TABLE IF NOT EXISTS alerts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		rule_id INTEGER,
		server_id TEXT NOT NULL,
		type TEXT NOT NULL,
		severity TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		title TEXT NOT NULL,
		message TEXT NOT NULL,
		metric TEXT,
		metric_value REAL,
		threshold REAL,
		affected_entity TEXT,
		triggered_at DATETIME NOT NULL,
		acknowledged_at DATETIME,
		acknowledged_by INTEGER,
		resolved_at DATETIME,
		resolved_by INTEGER,
		silenced_until DATETIME,
		notified_email INTEGER NOT NULL DEFAULT 0,
		notified_webhook INTEGER NOT NULL DEFAULT 0,
		metadata TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (rule_id) REFERENCES alert_rules(id),
		FOREIGN KEY (acknowledged_by) REFERENCES users(id),
		FOREIGN KEY (resolved_by) REFERENCES users(id)
	);

	CREATE INDEX IF NOT EXISTS idx_alerts_server_status
		ON alerts(server_id, status);
	CREATE INDEX IF NOT EXISTS idx_alerts_triggered
		ON alerts(triggered_at);
	CREATE INDEX IF NOT EXISTS idx_alerts_active
		ON alerts(server_id, status) WHERE status = 'active';

	-- Alert notifications track notification attempts
	CREATE TABLE IF NOT EXISTS alert_notifications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		alert_id INTEGER NOT NULL,
		channel TEXT NOT NULL,
		recipient TEXT,
		status TEXT NOT NULL,
		error_message TEXT,
		sent_at DATETIME NOT NULL,
		FOREIGN KEY (alert_id) REFERENCES alerts(id)
	);

	CREATE INDEX IF NOT EXISTS idx_alert_notifications_alert
		ON alert_notifications(alert_id);

	-- User alert subscriptions track which alerts users want to receive
	CREATE TABLE IF NOT EXISTS user_alert_subscriptions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		server_id TEXT,
		alert_type TEXT,
		severity TEXT,
		notify_email INTEGER NOT NULL DEFAULT 1,
		notify_ui INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, server_id, alert_type, severity),
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_user_alert_subscriptions_user
		ON user_alert_subscriptions(user_id);

	-- Alert notes track acknowledgement and resolution notes
	CREATE TABLE IF NOT EXISTS alert_notes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		alert_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		note_type TEXT NOT NULL,
		note TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (alert_id) REFERENCES alerts(id) ON DELETE CASCADE,
		FOREIGN KEY (user_id) REFERENCES users(id)
	);

	CREATE INDEX IF NOT EXISTS idx_alert_notes_alert
		ON alert_notes(alert_id);

	-- Alert retention configuration
	CREATE TABLE IF NOT EXISTS alert_retention_config (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id TEXT NOT NULL UNIQUE,
		retention_days INTEGER NOT NULL DEFAULT 30,
		auto_resolve_hours INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_by INTEGER,
		FOREIGN KEY (updated_by) REFERENCES users(id)
	);
	`

	_, err := d.db.Exec(schema)
	return err
}

// CreateAlertRule creates a new alert rule.
func (d *DB) CreateAlertRule(rule *models.AlertRule) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(`
		INSERT INTO alert_rules (
			server_id, name, description, type, enabled, severity,
			condition, metric, threshold, operator, duration,
			cooldown_minutes, notify_email, notify_webhook, notify_ui, webhook_url
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ServerID, rule.Name, rule.Description, rule.Type, rule.Enabled, rule.Severity,
		rule.Condition, rule.Metric, rule.Threshold, rule.Operator, rule.Duration,
		rule.CooldownMinutes, rule.NotifyEmail, rule.NotifyWebhook, rule.NotifyUI, rule.WebhookURL,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetAlertRules returns alert rules for a server.
func (d *DB) GetAlertRules(serverID string) ([]models.AlertRule, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, server_id, name, description, type, enabled, severity,
			condition, metric, threshold, operator, duration,
			cooldown_minutes, notify_email, notify_webhook, notify_ui, webhook_url,
			created_at, updated_at
		FROM alert_rules
		WHERE server_id = ?
		ORDER BY name`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []models.AlertRule
	for rows.Next() {
		var r models.AlertRule
		var condition, metric, operator, webhookURL sql.NullString
		var threshold sql.NullFloat64
		err := rows.Scan(
			&r.ID, &r.ServerID, &r.Name, &r.Description, &r.Type, &r.Enabled, &r.Severity,
			&condition, &metric, &threshold, &operator, &r.Duration,
			&r.CooldownMinutes, &r.NotifyEmail, &r.NotifyWebhook, &r.NotifyUI, &webhookURL,
			&r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if condition.Valid {
			r.Condition = condition.String
		}
		if metric.Valid {
			r.Metric = metric.String
		}
		if threshold.Valid {
			r.Threshold = threshold.Float64
		}
		if operator.Valid {
			r.Operator = operator.String
		}
		if webhookURL.Valid {
			r.WebhookURL = webhookURL.String
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// GetEnabledAlertRules returns only enabled alert rules.
func (d *DB) GetEnabledAlertRules(serverID string) ([]models.AlertRule, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, server_id, name, description, type, enabled, severity,
			condition, metric, threshold, operator, duration,
			cooldown_minutes, notify_email, notify_webhook, notify_ui, webhook_url,
			created_at, updated_at
		FROM alert_rules
		WHERE server_id = ? AND enabled = 1
		ORDER BY name`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []models.AlertRule
	for rows.Next() {
		var r models.AlertRule
		var condition, metric, operator, webhookURL sql.NullString
		var threshold sql.NullFloat64
		err := rows.Scan(
			&r.ID, &r.ServerID, &r.Name, &r.Description, &r.Type, &r.Enabled, &r.Severity,
			&condition, &metric, &threshold, &operator, &r.Duration,
			&r.CooldownMinutes, &r.NotifyEmail, &r.NotifyWebhook, &r.NotifyUI, &webhookURL,
			&r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if condition.Valid {
			r.Condition = condition.String
		}
		if metric.Valid {
			r.Metric = metric.String
		}
		if threshold.Valid {
			r.Threshold = threshold.Float64
		}
		if operator.Valid {
			r.Operator = operator.String
		}
		if webhookURL.Valid {
			r.WebhookURL = webhookURL.String
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// UpdateAlertRule updates an existing alert rule.
func (d *DB) UpdateAlertRule(rule *models.AlertRule) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE alert_rules SET
			name = ?, description = ?, type = ?, enabled = ?, severity = ?,
			condition = ?, metric = ?, threshold = ?, operator = ?, duration = ?,
			cooldown_minutes = ?, notify_email = ?, notify_webhook = ?, notify_ui = ?,
			webhook_url = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		rule.Name, rule.Description, rule.Type, rule.Enabled, rule.Severity,
		rule.Condition, rule.Metric, rule.Threshold, rule.Operator, rule.Duration,
		rule.CooldownMinutes, rule.NotifyEmail, rule.NotifyWebhook, rule.NotifyUI,
		rule.WebhookURL, rule.ID,
	)
	return err
}

// DeleteAlertRule deletes an alert rule.
func (d *DB) DeleteAlertRule(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM alert_rules WHERE id = ?`, id)
	return err
}

// CreateAlert creates a new alert instance.
func (d *DB) CreateAlert(alert *models.Alert) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var metadataJSON string
	if alert.Metadata != "" {
		metadataJSON = alert.Metadata
	}

	result, err := d.db.Exec(`
		INSERT INTO alerts (
			rule_id, server_id, type, severity, status, title, message,
			metric, metric_value, threshold, affected_entity, triggered_at,
			notified_email, notified_webhook, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		alert.RuleID, alert.ServerID, alert.Type, alert.Severity, alert.Status,
		alert.Title, alert.Message, alert.Metric, alert.MetricValue, alert.Threshold,
		alert.AffectedEntity, alert.TriggeredAt, alert.NotifiedEmail, alert.NotifiedWebhook,
		metadataJSON,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetActiveAlerts returns active alerts for a server.
func (d *DB) GetActiveAlerts(serverID string) ([]models.Alert, error) {
	return d.getAlertsByStatus(serverID, "active")
}

// GetAlertsByStatusPublic returns alerts by status (public wrapper).
func (d *DB) GetAlertsByStatusPublic(serverID, status string) ([]models.Alert, error) {
	return d.getAlertsByStatus(serverID, status)
}

// GetAlertsByStatus returns alerts by status.
func (d *DB) getAlertsByStatus(serverID, status string) ([]models.Alert, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT a.id, a.rule_id, a.server_id, a.type, a.severity, a.status, a.title, a.message,
			a.metric, a.metric_value, a.threshold, a.affected_entity, a.triggered_at,
			a.acknowledged_at, a.acknowledged_by, ack_user.email,
			a.resolved_at, a.resolved_by, res_user.email,
			a.silenced_until, a.notified_email, a.notified_webhook, a.metadata
		FROM alerts a
		LEFT JOIN users ack_user ON a.acknowledged_by = ack_user.id
		LEFT JOIN users res_user ON a.resolved_by = res_user.id
		WHERE a.server_id = ? AND a.status = ?
		ORDER BY a.triggered_at DESC`, serverID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanAlertsWithEmails(rows)
}

// GetRecentAlerts returns recent alerts.
func (d *DB) GetRecentAlerts(serverID string, limit int) ([]models.Alert, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT a.id, a.rule_id, a.server_id, a.type, a.severity, a.status, a.title, a.message,
			a.metric, a.metric_value, a.threshold, a.affected_entity, a.triggered_at,
			a.acknowledged_at, a.acknowledged_by, ack_user.email,
			a.resolved_at, a.resolved_by, res_user.email,
			a.silenced_until, a.notified_email, a.notified_webhook, a.metadata
		FROM alerts a
		LEFT JOIN users ack_user ON a.acknowledged_by = ack_user.id
		LEFT JOIN users res_user ON a.resolved_by = res_user.id
		WHERE a.server_id = ?
		ORDER BY a.triggered_at DESC
		LIMIT ?`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanAlertsWithEmails(rows)
}

// scanAlerts scans alert rows into Alert structs (legacy, without user emails).
func (d *DB) scanAlerts(rows *sql.Rows) ([]models.Alert, error) {
	var alerts []models.Alert
	for rows.Next() {
		var a models.Alert
		var ruleID, acknowledgedBy sql.NullInt64
		var metric, affectedEntity, metadata sql.NullString
		var metricValue, threshold sql.NullFloat64
		var acknowledgedAt, resolvedAt, silencedUntil sql.NullTime

		err := rows.Scan(
			&a.ID, &ruleID, &a.ServerID, &a.Type, &a.Severity, &a.Status,
			&a.Title, &a.Message, &metric, &metricValue, &threshold,
			&affectedEntity, &a.TriggeredAt, &acknowledgedAt, &acknowledgedBy,
			&resolvedAt, &silencedUntil, &a.NotifiedEmail, &a.NotifiedWebhook, &metadata,
		)
		if err != nil {
			return nil, err
		}

		if ruleID.Valid {
			a.RuleID = ruleID.Int64
		}
		if metric.Valid {
			a.Metric = metric.String
		}
		if metricValue.Valid {
			a.MetricValue = metricValue.Float64
		}
		if threshold.Valid {
			a.Threshold = threshold.Float64
		}
		if affectedEntity.Valid {
			a.AffectedEntity = affectedEntity.String
		}
		if acknowledgedAt.Valid {
			a.AcknowledgedAt = &acknowledgedAt.Time
		}
		if acknowledgedBy.Valid {
			a.AcknowledgedBy = &acknowledgedBy.Int64
		}
		if resolvedAt.Valid {
			a.ResolvedAt = &resolvedAt.Time
		}
		if silencedUntil.Valid {
			a.SilencedUntil = &silencedUntil.Time
		}
		if metadata.Valid {
			a.Metadata = metadata.String
		}

		alerts = append(alerts, a)
	}
	return alerts, nil
}

// scanAlertsWithEmails scans alert rows with user email joins into Alert structs.
func (d *DB) scanAlertsWithEmails(rows *sql.Rows) ([]models.Alert, error) {
	var alerts []models.Alert
	for rows.Next() {
		var a models.Alert
		var ruleID, acknowledgedBy, resolvedBy sql.NullInt64
		var metric, affectedEntity, metadata, ackEmail, resEmail sql.NullString
		var metricValue, threshold sql.NullFloat64
		var acknowledgedAt, resolvedAt, silencedUntil sql.NullTime

		err := rows.Scan(
			&a.ID, &ruleID, &a.ServerID, &a.Type, &a.Severity, &a.Status,
			&a.Title, &a.Message, &metric, &metricValue, &threshold,
			&affectedEntity, &a.TriggeredAt, &acknowledgedAt, &acknowledgedBy, &ackEmail,
			&resolvedAt, &resolvedBy, &resEmail,
			&silencedUntil, &a.NotifiedEmail, &a.NotifiedWebhook, &metadata,
		)
		if err != nil {
			return nil, err
		}

		if ruleID.Valid {
			a.RuleID = ruleID.Int64
		}
		if metric.Valid {
			a.Metric = metric.String
		}
		if metricValue.Valid {
			a.MetricValue = metricValue.Float64
		}
		if threshold.Valid {
			a.Threshold = threshold.Float64
		}
		if affectedEntity.Valid {
			a.AffectedEntity = affectedEntity.String
		}
		if acknowledgedAt.Valid {
			a.AcknowledgedAt = &acknowledgedAt.Time
		}
		if acknowledgedBy.Valid {
			a.AcknowledgedBy = &acknowledgedBy.Int64
		}
		if ackEmail.Valid {
			a.AcknowledgedByEmail = ackEmail.String
		}
		if resolvedAt.Valid {
			a.ResolvedAt = &resolvedAt.Time
		}
		if resolvedBy.Valid {
			a.ResolvedBy = &resolvedBy.Int64
		}
		if resEmail.Valid {
			a.ResolvedByEmail = resEmail.String
		}
		if silencedUntil.Valid {
			a.SilencedUntil = &silencedUntil.Time
		}
		if metadata.Valid {
			a.Metadata = metadata.String
		}

		alerts = append(alerts, a)
	}
	return alerts, nil
}

// AcknowledgeAlert marks an alert as acknowledged.
func (d *DB) AcknowledgeAlert(alertID int64, userID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE alerts SET
			status = 'acknowledged',
			acknowledged_at = CURRENT_TIMESTAMP,
			acknowledged_by = ?
		WHERE id = ? AND status = 'active'`, userID, alertID)
	return err
}

// ResolveAlert marks an alert as resolved.
func (d *DB) ResolveAlert(alertID int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE alerts SET
			status = 'resolved',
			resolved_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN ('active', 'acknowledged')`, alertID)
	return err
}

// SilenceAlert silences an alert until a specific time.
func (d *DB) SilenceAlert(alertID int64, until time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE alerts SET
			status = 'silenced',
			silenced_until = ?
		WHERE id = ?`, until, alertID)
	return err
}

// GetAlertSummary returns aggregated alert statistics.
func (d *DB) GetAlertSummary(serverID string) (*models.AlertSummary, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	summary := &models.AlertSummary{
		BySeverity: make(map[string]int),
		ByType:     make(map[string]int),
	}

	// Count by status
	rows, err := d.db.Query(`
		SELECT status, COUNT(*) FROM alerts
		WHERE server_id = ?
		GROUP BY status`, serverID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			rows.Close()
			return nil, err
		}
		switch status {
		case "active":
			summary.TotalActive = count
		case "acknowledged":
			summary.TotalAcknowledged = count
		case "resolved":
			summary.TotalResolved = count
		}
	}
	rows.Close()

	// Count by severity (active only)
	rows, err = d.db.Query(`
		SELECT severity, COUNT(*) FROM alerts
		WHERE server_id = ? AND status = 'active'
		GROUP BY severity`, serverID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var severity string
		var count int
		if err := rows.Scan(&severity, &count); err != nil {
			rows.Close()
			return nil, err
		}
		summary.BySeverity[severity] = count
	}
	rows.Close()

	// Count by type (active only)
	rows, err = d.db.Query(`
		SELECT type, COUNT(*) FROM alerts
		WHERE server_id = ? AND status = 'active'
		GROUP BY type`, serverID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var alertType string
		var count int
		if err := rows.Scan(&alertType, &count); err != nil {
			rows.Close()
			return nil, err
		}
		summary.ByType[alertType] = count
	}
	rows.Close()

	// Get recent alerts
	summary.RecentAlerts, _ = d.GetRecentAlerts(serverID, 10)

	return summary, nil
}

// EnsureDefaultAlertRules creates default alert rules if none exist.
func (d *DB) EnsureDefaultAlertRules(serverID string) error {
	rules, err := d.GetAlertRules(serverID)
	if err != nil {
		return err
	}

	if len(rules) > 0 {
		return nil // Rules already exist
	}

	defaults := models.DefaultAlertRules(serverID)
	for _, rule := range defaults {
		if _, err := d.CreateAlertRule(&rule); err != nil {
			return err
		}
	}

	return nil
}

// CreateAlertNotification records a notification attempt.
func (d *DB) CreateAlertNotification(notification *models.AlertNotification) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO alert_notifications (alert_id, channel, recipient, status, error_message, sent_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		notification.AlertID, notification.Channel, notification.Recipient,
		notification.Status, notification.ErrorMessage, notification.SentAt,
	)
	return err
}

// GetLastAlertForRule returns the most recent alert for a given rule.
func (d *DB) GetLastAlertForRule(ruleID int64) (*models.Alert, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.db.QueryRow(`
		SELECT id, rule_id, server_id, type, severity, status, title, message,
			metric, metric_value, threshold, affected_entity, triggered_at,
			acknowledged_at, acknowledged_by, resolved_at, silenced_until,
			notified_email, notified_webhook, metadata
		FROM alerts
		WHERE rule_id = ?
		ORDER BY triggered_at DESC
		LIMIT 1`, ruleID)

	var a models.Alert
	var ruleIDVal, acknowledgedBy sql.NullInt64
	var metric, affectedEntity, metadata sql.NullString
	var metricValue, threshold sql.NullFloat64
	var acknowledgedAt, resolvedAt, silencedUntil sql.NullTime

	err := row.Scan(
		&a.ID, &ruleIDVal, &a.ServerID, &a.Type, &a.Severity, &a.Status,
		&a.Title, &a.Message, &metric, &metricValue, &threshold,
		&affectedEntity, &a.TriggeredAt, &acknowledgedAt, &acknowledgedBy,
		&resolvedAt, &silencedUntil, &a.NotifiedEmail, &a.NotifiedWebhook, &metadata,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if ruleIDVal.Valid {
		a.RuleID = ruleIDVal.Int64
	}
	if metric.Valid {
		a.Metric = metric.String
	}
	if metricValue.Valid {
		a.MetricValue = metricValue.Float64
	}
	if threshold.Valid {
		a.Threshold = threshold.Float64
	}
	if affectedEntity.Valid {
		a.AffectedEntity = affectedEntity.String
	}
	if acknowledgedAt.Valid {
		a.AcknowledgedAt = &acknowledgedAt.Time
	}
	if acknowledgedBy.Valid {
		a.AcknowledgedBy = &acknowledgedBy.Int64
	}
	if resolvedAt.Valid {
		a.ResolvedAt = &resolvedAt.Time
	}
	if silencedUntil.Valid {
		a.SilencedUntil = &silencedUntil.Time
	}
	if metadata.Valid {
		a.Metadata = metadata.String
	}

	return &a, nil
}

// CleanupOldAlerts removes resolved alerts older than retention period.
func (d *DB) CleanupOldAlerts(retention time.Duration) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-retention)

	// First delete notifications for old resolved alerts
	_, err := d.db.Exec(`
		DELETE FROM alert_notifications
		WHERE alert_id IN (
			SELECT id FROM alerts
			WHERE status = 'resolved' AND resolved_at < ?
		)`, cutoff)
	if err != nil {
		return 0, err
	}

	// Then delete the alerts
	result, err := d.db.Exec(`
		DELETE FROM alerts
		WHERE status = 'resolved' AND resolved_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// AlertMetadata helper for encoding/decoding JSON metadata
type AlertMetadata map[string]interface{}

func (m AlertMetadata) String() string {
	b, _ := json.Marshal(m)
	return string(b)
}

func ParseAlertMetadata(s string) AlertMetadata {
	var m AlertMetadata
	json.Unmarshal([]byte(s), &m)
	return m
}

// AlertNote represents a note attached to an alert.
type AlertNote struct {
	ID        int64     `json:"id"`
	AlertID   int64     `json:"alert_id"`
	UserID    int64     `json:"user_id"`
	UserEmail string    `json:"user_email,omitempty"` // Populated on read
	NoteType  string    `json:"note_type"`            // acknowledge, resolve, comment
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// AddAlertNote adds a note to an alert.
func (d *DB) AddAlertNote(alertID int64, userID string, noteType, note string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO alert_notes (alert_id, user_id, note_type, note)
		VALUES (?, ?, ?, ?)`, alertID, userID, noteType, note)
	return err
}

// GetAlertNotes returns all notes for an alert.
func (d *DB) GetAlertNotes(alertID int64) ([]AlertNote, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT n.id, n.alert_id, n.user_id, u.email, n.note_type, n.note, n.created_at
		FROM alert_notes n
		LEFT JOIN users u ON n.user_id = u.id
		WHERE n.alert_id = ?
		ORDER BY n.created_at ASC`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []AlertNote
	for rows.Next() {
		var n AlertNote
		var userEmail sql.NullString
		if err := rows.Scan(&n.ID, &n.AlertID, &n.UserID, &userEmail, &n.NoteType, &n.Note, &n.CreatedAt); err != nil {
			return nil, err
		}
		if userEmail.Valid {
			n.UserEmail = userEmail.String
		}
		notes = append(notes, n)
	}
	return notes, nil
}

// AcknowledgeAlertWithNote marks an alert as acknowledged and adds a note.
func (d *DB) AcknowledgeAlertWithNote(alertID int64, userID string, note string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update alert status
	_, err = tx.Exec(`
		UPDATE alerts SET
			status = 'acknowledged',
			acknowledged_at = CURRENT_TIMESTAMP,
			acknowledged_by = ?
		WHERE id = ? AND status = 'active'`, userID, alertID)
	if err != nil {
		return err
	}

	// Add note if provided
	if note != "" {
		_, err = tx.Exec(`
			INSERT INTO alert_notes (alert_id, user_id, note_type, note)
			VALUES (?, ?, 'acknowledge', ?)`, alertID, userID, note)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ResolveAlertWithNote marks an alert as resolved and adds a note.
func (d *DB) ResolveAlertWithNote(alertID int64, userID string, note string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update alert status with resolved_by
	_, err = tx.Exec(`
		UPDATE alerts SET
			status = 'resolved',
			resolved_at = CURRENT_TIMESTAMP,
			resolved_by = ?
		WHERE id = ? AND status IN ('active', 'acknowledged')`, userID, alertID)
	if err != nil {
		return err
	}

	// Add note if provided
	if note != "" {
		_, err = tx.Exec(`
			INSERT INTO alert_notes (alert_id, user_id, note_type, note)
			VALUES (?, ?, 'resolve', ?)`, alertID, userID, note)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UserAlertSubscription represents a user's alert subscription preferences.
type UserAlertSubscription struct {
	ID          int64   `json:"id"`
	UserID      int64   `json:"user_id"`
	ServerID    *string `json:"server_id,omitempty"`    // nil = all servers
	AlertType   *string `json:"alert_type,omitempty"`   // nil = all types
	Severity    *string `json:"severity,omitempty"`     // nil = all severities
	NotifyEmail bool    `json:"notify_email"`
	NotifyUI    bool    `json:"notify_ui"`
}

// GetUserAlertSubscriptions returns all alert subscriptions for a user.
func (d *DB) GetUserAlertSubscriptions(userID int64) ([]UserAlertSubscription, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, user_id, server_id, alert_type, severity, notify_email, notify_ui
		FROM user_alert_subscriptions
		WHERE user_id = ?
		ORDER BY server_id, alert_type, severity`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []UserAlertSubscription
	for rows.Next() {
		var s UserAlertSubscription
		var serverID, alertType, severity sql.NullString
		if err := rows.Scan(&s.ID, &s.UserID, &serverID, &alertType, &severity, &s.NotifyEmail, &s.NotifyUI); err != nil {
			return nil, err
		}
		if serverID.Valid {
			s.ServerID = &serverID.String
		}
		if alertType.Valid {
			s.AlertType = &alertType.String
		}
		if severity.Valid {
			s.Severity = &severity.String
		}
		subs = append(subs, s)
	}
	return subs, nil
}

// CreateUserAlertSubscription creates a new alert subscription.
func (d *DB) CreateUserAlertSubscription(sub *UserAlertSubscription) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO user_alert_subscriptions
		(user_id, server_id, alert_type, severity, notify_email, notify_ui)
		VALUES (?, ?, ?, ?, ?, ?)`,
		sub.UserID, sub.ServerID, sub.AlertType, sub.Severity, sub.NotifyEmail, sub.NotifyUI)
	return err
}

// DeleteUserAlertSubscription deletes an alert subscription.
func (d *DB) DeleteUserAlertSubscription(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM user_alert_subscriptions WHERE id = ?`, id)
	return err
}

// GetUsersToNotify returns users who should be notified for an alert.
func (d *DB) GetUsersToNotify(serverID, alertType, severity string) ([]int64, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT DISTINCT user_id FROM user_alert_subscriptions
		WHERE notify_email = 1
		  AND (server_id IS NULL OR server_id = ?)
		  AND (alert_type IS NULL OR alert_type = ?)
		  AND (severity IS NULL OR severity = ?)`, serverID, alertType, severity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userIDs []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, userID)
	}
	return userIDs, nil
}

// AlertRetentionConfig represents alert retention settings.
type AlertRetentionConfig struct {
	ServerID         string `json:"server_id"`
	RetentionDays    int    `json:"retention_days"`
	AutoResolveHours int    `json:"auto_resolve_hours"` // 0 = disabled
}

// GetAlertRetentionConfig returns the alert retention config for a server.
func (d *DB) GetAlertRetentionConfig(serverID string) (*AlertRetentionConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var config AlertRetentionConfig
	config.ServerID = serverID

	err := d.db.QueryRow(`
		SELECT retention_days, auto_resolve_hours
		FROM alert_retention_config
		WHERE server_id = ?`, serverID).Scan(&config.RetentionDays, &config.AutoResolveHours)

	if err == sql.ErrNoRows {
		// Return defaults
		config.RetentionDays = 30
		config.AutoResolveHours = 0
		return &config, nil
	}
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// SetAlertRetentionConfig updates the alert retention config for a server.
func (d *DB) SetAlertRetentionConfig(config *AlertRetentionConfig, userID *string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO alert_retention_config
		(server_id, retention_days, auto_resolve_hours, updated_at, updated_by)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		config.ServerID, config.RetentionDays, config.AutoResolveHours, userID)
	return err
}

// GetGlobalActiveAlertCount returns the total count of active alerts across all servers.
func (d *DB) GetGlobalActiveAlertCount() (int, int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var totalActive int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM alerts WHERE status = 'active'`).Scan(&totalActive)
	if err != nil {
		return 0, 0, err
	}

	var criticalCount int
	err = d.db.QueryRow(`SELECT COUNT(*) FROM alerts WHERE status = 'active' AND severity = 'critical'`).Scan(&criticalCount)
	if err != nil {
		return totalActive, 0, err
	}

	return totalActive, criticalCount, nil
}

// CleanupOldAlertsByServerConfig removes resolved alerts based on per-server retention config.
func (d *DB) CleanupOldAlertsByServerConfig(serverID string) (int64, error) {
	config, err := d.GetAlertRetentionConfig(serverID)
	if err != nil {
		return 0, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -config.RetentionDays)

	// Delete notes for old resolved alerts first
	_, err = d.db.Exec(`
		DELETE FROM alert_notes
		WHERE alert_id IN (
			SELECT id FROM alerts
			WHERE server_id = ? AND status = 'resolved' AND resolved_at < ?
		)`, serverID, cutoff)
	if err != nil {
		return 0, err
	}

	// Delete notifications for old resolved alerts
	_, err = d.db.Exec(`
		DELETE FROM alert_notifications
		WHERE alert_id IN (
			SELECT id FROM alerts
			WHERE server_id = ? AND status = 'resolved' AND resolved_at < ?
		)`, serverID, cutoff)
	if err != nil {
		return 0, err
	}

	// Delete the alerts
	result, err := d.db.Exec(`
		DELETE FROM alerts
		WHERE server_id = ? AND status = 'resolved' AND resolved_at < ?`, serverID, cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// AutoResolveStaleAlerts auto-resolves alerts that have been active too long.
func (d *DB) AutoResolveStaleAlerts(serverID string) (int64, error) {
	config, err := d.GetAlertRetentionConfig(serverID)
	if err != nil {
		return 0, err
	}

	if config.AutoResolveHours == 0 {
		return 0, nil // Auto-resolve disabled
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(config.AutoResolveHours) * time.Hour)

	result, err := d.db.Exec(`
		UPDATE alerts SET
			status = 'resolved',
			resolved_at = CURRENT_TIMESTAMP
		WHERE server_id = ? AND status IN ('active', 'acknowledged') AND triggered_at < ?`,
		serverID, cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// GetAlert returns a single alert by ID.
func (d *DB) GetAlert(alertID int64) (*models.Alert, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.db.QueryRow(`
		SELECT id, rule_id, server_id, type, severity, status, title, message,
			metric, metric_value, threshold, affected_entity, triggered_at,
			acknowledged_at, acknowledged_by, resolved_at, silenced_until,
			notified_email, notified_webhook, metadata
		FROM alerts
		WHERE id = ?`, alertID)

	var a models.Alert
	var ruleID, acknowledgedBy sql.NullInt64
	var metric, affectedEntity, metadata sql.NullString
	var metricValue, threshold sql.NullFloat64
	var acknowledgedAt, resolvedAt, silencedUntil sql.NullTime

	err := row.Scan(
		&a.ID, &ruleID, &a.ServerID, &a.Type, &a.Severity, &a.Status,
		&a.Title, &a.Message, &metric, &metricValue, &threshold,
		&affectedEntity, &a.TriggeredAt, &acknowledgedAt, &acknowledgedBy,
		&resolvedAt, &silencedUntil, &a.NotifiedEmail, &a.NotifiedWebhook, &metadata,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if ruleID.Valid {
		a.RuleID = ruleID.Int64
	}
	if metric.Valid {
		a.Metric = metric.String
	}
	if metricValue.Valid {
		a.MetricValue = metricValue.Float64
	}
	if threshold.Valid {
		a.Threshold = threshold.Float64
	}
	if affectedEntity.Valid {
		a.AffectedEntity = affectedEntity.String
	}
	if acknowledgedAt.Valid {
		a.AcknowledgedAt = &acknowledgedAt.Time
	}
	if acknowledgedBy.Valid {
		a.AcknowledgedBy = &acknowledgedBy.Int64
	}
	if resolvedAt.Valid {
		a.ResolvedAt = &resolvedAt.Time
	}
	if silencedUntil.Valid {
		a.SilencedUntil = &silencedUntil.Time
	}
	if metadata.Valid {
		a.Metadata = metadata.String
	}

	return &a, nil
}

// GetActiveAlertForRuleAndEntity returns an existing active or acknowledged alert for a rule/entity combination.
// This is used for deduplication - we don't want to create multiple alerts for the same ongoing incident.
func (d *DB) GetActiveAlertForRuleAndEntity(ruleID int64, affectedEntity string) (*models.Alert, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.db.QueryRow(`
		SELECT id, rule_id, server_id, type, severity, status, title, message,
			metric, metric_value, threshold, affected_entity, triggered_at,
			acknowledged_at, acknowledged_by, resolved_at, silenced_until,
			notified_email, notified_webhook, metadata
		FROM alerts
		WHERE rule_id = ? AND affected_entity = ? AND status IN ('active', 'acknowledged')
		ORDER BY triggered_at DESC
		LIMIT 1`, ruleID, affectedEntity)

	var a models.Alert
	var ruleIDVal, acknowledgedBy sql.NullInt64
	var metric, affectedEntityVal, metadata sql.NullString
	var metricValue, threshold sql.NullFloat64
	var acknowledgedAt, resolvedAt, silencedUntil sql.NullTime

	err := row.Scan(
		&a.ID, &ruleIDVal, &a.ServerID, &a.Type, &a.Severity, &a.Status,
		&a.Title, &a.Message, &metric, &metricValue, &threshold,
		&affectedEntityVal, &a.TriggeredAt, &acknowledgedAt, &acknowledgedBy,
		&resolvedAt, &silencedUntil, &a.NotifiedEmail, &a.NotifiedWebhook, &metadata,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if ruleIDVal.Valid {
		a.RuleID = ruleIDVal.Int64
	}
	if metric.Valid {
		a.Metric = metric.String
	}
	if metricValue.Valid {
		a.MetricValue = metricValue.Float64
	}
	if threshold.Valid {
		a.Threshold = threshold.Float64
	}
	if affectedEntityVal.Valid {
		a.AffectedEntity = affectedEntityVal.String
	}
	if acknowledgedAt.Valid {
		a.AcknowledgedAt = &acknowledgedAt.Time
	}
	if acknowledgedBy.Valid {
		a.AcknowledgedBy = &acknowledgedBy.Int64
	}
	if resolvedAt.Valid {
		a.ResolvedAt = &resolvedAt.Time
	}
	if silencedUntil.Valid {
		a.SilencedUntil = &silencedUntil.Time
	}
	if metadata.Valid {
		a.Metadata = metadata.String
	}

	return &a, nil
}

// GetRecentlyAcknowledgedAlertForRule returns an acknowledged alert for a rule
// that was acknowledged within the specified number of minutes.
// This is used to implement alert suppression after acknowledgement.
func (d *DB) GetRecentlyAcknowledgedAlertForRule(ruleID int64, affectedEntity string, withinMinutes int) (*models.Alert, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(withinMinutes) * time.Minute)

	row := d.db.QueryRow(`
		SELECT id, rule_id, server_id, type, severity, status, title, message,
			metric, metric_value, threshold, affected_entity, triggered_at,
			acknowledged_at, acknowledged_by, resolved_at, silenced_until,
			notified_email, notified_webhook, metadata
		FROM alerts
		WHERE rule_id = ? AND (affected_entity = ? OR (affected_entity IS NULL AND ? = ''))
		  AND status = 'acknowledged'
		  AND acknowledged_at >= ?
		ORDER BY acknowledged_at DESC
		LIMIT 1`, ruleID, affectedEntity, affectedEntity, cutoff)

	var a models.Alert
	var ruleIDVal, acknowledgedBy sql.NullInt64
	var metric, affectedEntityVal, metadata sql.NullString
	var metricValue, threshold sql.NullFloat64
	var acknowledgedAt, resolvedAt, silencedUntil sql.NullTime

	err := row.Scan(
		&a.ID, &ruleIDVal, &a.ServerID, &a.Type, &a.Severity, &a.Status,
		&a.Title, &a.Message, &metric, &metricValue, &threshold,
		&affectedEntityVal, &a.TriggeredAt, &acknowledgedAt, &acknowledgedBy,
		&resolvedAt, &silencedUntil, &a.NotifiedEmail, &a.NotifiedWebhook, &metadata,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if ruleIDVal.Valid {
		a.RuleID = ruleIDVal.Int64
	}
	if metric.Valid {
		a.Metric = metric.String
	}
	if metricValue.Valid {
		a.MetricValue = metricValue.Float64
	}
	if threshold.Valid {
		a.Threshold = threshold.Float64
	}
	if affectedEntityVal.Valid {
		a.AffectedEntity = affectedEntityVal.String
	}
	if acknowledgedAt.Valid {
		a.AcknowledgedAt = &acknowledgedAt.Time
	}
	if acknowledgedBy.Valid {
		a.AcknowledgedBy = &acknowledgedBy.Int64
	}
	if resolvedAt.Valid {
		a.ResolvedAt = &resolvedAt.Time
	}
	if silencedUntil.Valid {
		a.SilencedUntil = &silencedUntil.Time
	}
	if metadata.Valid {
		a.Metadata = metadata.String
	}

	return &a, nil
}

// GetAlertRule returns a single alert rule by ID.
func (d *DB) GetAlertRule(ruleID int64) (*models.AlertRule, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var r models.AlertRule
	var condition, metric, operator, webhookURL sql.NullString
	var threshold sql.NullFloat64
	err := d.db.QueryRow(`
		SELECT id, server_id, name, description, type, enabled, severity,
			condition, metric, threshold, operator, duration,
			cooldown_minutes, notify_email, notify_webhook, notify_ui, webhook_url,
			created_at, updated_at
		FROM alert_rules
		WHERE id = ?`, ruleID).Scan(
		&r.ID, &r.ServerID, &r.Name, &r.Description, &r.Type, &r.Enabled, &r.Severity,
		&condition, &metric, &threshold, &operator, &r.Duration,
		&r.CooldownMinutes, &r.NotifyEmail, &r.NotifyWebhook, &r.NotifyUI, &webhookURL,
		&r.CreatedAt, &r.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if condition.Valid {
		r.Condition = condition.String
	}
	if metric.Valid {
		r.Metric = metric.String
	}
	if threshold.Valid {
		r.Threshold = threshold.Float64
	}
	if operator.Valid {
		r.Operator = operator.String
	}
	if webhookURL.Valid {
		r.WebhookURL = webhookURL.String
	}

	return &r, nil
}
