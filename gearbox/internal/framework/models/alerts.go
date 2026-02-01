package models

import "time"

// AlertSeverity represents the severity level of an alert
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityCritical AlertSeverity = "critical"
)

// AlertType represents the type/category of an alert
type AlertType string

const (
	AlertTypeMetric       AlertType = "metric"
	AlertTypeBackendDown  AlertType = "backend_down"
	AlertTypeFrontendDown AlertType = "frontend_down"
	AlertTypeServiceDown  AlertType = "service_down"
	AlertTypeSecurity     AlertType = "security"
	AlertTypeCertificate  AlertType = "certificate"
	AlertTypeTraffic      AlertType = "traffic"
	AlertTypeOSUpdate     AlertType = "os_update"
)

// AlertStatus represents the current status of an alert
type AlertStatus string

const (
	AlertStatusActive      AlertStatus = "active"
	AlertStatusAcknowledged AlertStatus = "acknowledged"
	AlertStatusResolved    AlertStatus = "resolved"
	AlertStatusSilenced    AlertStatus = "silenced"
)

// AlertRule defines a rule that triggers alerts
type AlertRule struct {
	ID              int64         `json:"id"`
	BoxID           string        `json:"box_id"`
	Name            string        `json:"name"`
	Description     string        `json:"description"`
	Type            AlertType     `json:"type"`
	Enabled         bool          `json:"enabled"`
	Severity        AlertSeverity `json:"severity"`
	Condition       string        `json:"condition"`       // e.g., "cpu_percent > 90"
	Metric          string        `json:"metric"`          // e.g., "cpu_percent", "memory_percent"
	Threshold       float64       `json:"threshold"`       // threshold value
	Operator        string        `json:"operator"`        // >, <, >=, <=, ==
	Duration        int           `json:"duration"`        // seconds the condition must persist
	CooldownMinutes int           `json:"cooldown_minutes"` // minutes before re-alerting
	NotifyEmail     bool          `json:"notify_email"`
	NotifyWebhook   bool          `json:"notify_webhook"`
	NotifyUI        bool          `json:"notify_ui"`
	WebhookURL      string        `json:"webhook_url,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

// Alert represents a triggered alert instance
type Alert struct {
	ID                  int64         `json:"id"`
	RuleID              int64         `json:"rule_id"`
	BoxID               string        `json:"box_id"`
	Type                AlertType     `json:"type"`
	Severity            AlertSeverity `json:"severity"`
	Status              AlertStatus   `json:"status"`
	Title               string        `json:"title"`
	Message             string        `json:"message"`
	Metric              string        `json:"metric,omitempty"`
	MetricValue         float64       `json:"metric_value,omitempty"`
	Threshold           float64       `json:"threshold,omitempty"`
	AffectedEntity      string        `json:"affected_entity,omitempty"` // backend name, IP, etc.
	TriggeredAt         time.Time     `json:"triggered_at"`
	AcknowledgedAt      *time.Time    `json:"acknowledged_at,omitempty"`
	AcknowledgedBy      *int64        `json:"acknowledged_by,omitempty"`
	AcknowledgedByEmail string        `json:"acknowledged_by_email,omitempty"` // Populated on query
	ResolvedAt          *time.Time    `json:"resolved_at,omitempty"`
	ResolvedBy          *int64        `json:"resolved_by,omitempty"`
	ResolvedByEmail     string        `json:"resolved_by_email,omitempty"` // Populated on query
	SilencedUntil       *time.Time    `json:"silenced_until,omitempty"`
	NotifiedEmail       bool          `json:"notified_email"`
	NotifiedWebhook     bool          `json:"notified_webhook"`
	Metadata            string        `json:"metadata,omitempty"` // JSON additional data
}

// AlertNotification records notification attempts
type AlertNotification struct {
	ID           int64     `json:"id"`
	AlertID      int64     `json:"alert_id"`
	Channel      string    `json:"channel"` // "email", "webhook", "ui"
	Recipient    string    `json:"recipient,omitempty"`
	Status       string    `json:"status"` // "sent", "failed", "pending"
	ErrorMessage string    `json:"error_message,omitempty"`
	SentAt       time.Time `json:"sent_at"`
}

// AlertSummary provides aggregated alert statistics
type AlertSummary struct {
	TotalActive      int            `json:"total_active"`
	TotalAcknowledged int           `json:"total_acknowledged"`
	TotalResolved    int            `json:"total_resolved"`
	BySeverity       map[string]int `json:"by_severity"`
	ByType           map[string]int `json:"by_type"`
	RecentAlerts     []Alert        `json:"recent_alerts"`
}

// DefaultAlertRules returns the default alert rules for a server
func DefaultAlertRules(serverID string) []AlertRule {
	return []AlertRule{
		{
			BoxID:        serverID,
			Name:            "High CPU Usage",
			Description:     "CPU usage exceeds threshold",
			Type:            AlertTypeMetric,
			Enabled:         true,
			Severity:        AlertSeverityWarning,
			Metric:          "cpu_percent",
			Threshold:       80,
			Operator:        ">",
			Duration:        60,
			CooldownMinutes: 5,
			NotifyUI:        true,
		},
		{
			BoxID:        serverID,
			Name:            "Critical CPU Usage",
			Description:     "CPU usage critically high",
			Type:            AlertTypeMetric,
			Enabled:         true,
			Severity:        AlertSeverityCritical,
			Metric:          "cpu_percent",
			Threshold:       95,
			Operator:        ">",
			Duration:        30,
			CooldownMinutes: 5,
			NotifyUI:        true,
			NotifyEmail:     true,
		},
		{
			BoxID:        serverID,
			Name:            "High Memory Usage",
			Description:     "Memory usage exceeds threshold",
			Type:            AlertTypeMetric,
			Enabled:         true,
			Severity:        AlertSeverityWarning,
			Metric:          "memory_percent",
			Threshold:       85,
			Operator:        ">",
			Duration:        60,
			CooldownMinutes: 5,
			NotifyUI:        true,
		},
		{
			BoxID:        serverID,
			Name:            "Critical Memory Usage",
			Description:     "Memory usage critically high",
			Type:            AlertTypeMetric,
			Enabled:         true,
			Severity:        AlertSeverityCritical,
			Metric:          "memory_percent",
			Threshold:       95,
			Operator:        ">",
			Duration:        30,
			CooldownMinutes: 5,
			NotifyUI:        true,
			NotifyEmail:     true,
		},
		{
			BoxID:        serverID,
			Name:            "High Disk Usage",
			Description:     "Disk usage exceeds threshold",
			Type:            AlertTypeMetric,
			Enabled:         true,
			Severity:        AlertSeverityWarning,
			Metric:          "disk_percent",
			Threshold:       80,
			Operator:        ">",
			Duration:        300,
			CooldownMinutes: 60,
			NotifyUI:        true,
		},
		{
			BoxID:        serverID,
			Name:            "Backend Down",
			Description:     "A backend has gone offline",
			Type:            AlertTypeBackendDown,
			Enabled:         true,
			Severity:        AlertSeverityCritical,
			Duration:        30,
			CooldownMinutes: 1,
			NotifyUI:        true,
			NotifyEmail:     true,
		},
		{
			BoxID:        serverID,
			Name:            "Frontend Down",
			Description:     "A frontend has gone offline",
			Type:            AlertTypeFrontendDown,
			Enabled:         true,
			Severity:        AlertSeverityCritical,
			Duration:        30,
			CooldownMinutes: 1,
			NotifyUI:        true,
			NotifyEmail:     true,
		},
		{
			BoxID:        serverID,
			Name:            "High Error Rate",
			Description:     "Error rate exceeds threshold",
			Type:            AlertTypeTraffic,
			Enabled:         true,
			Severity:        AlertSeverityWarning,
			Metric:          "error_rate",
			Threshold:       5,
			Operator:        ">",
			Duration:        120,
			CooldownMinutes: 10,
			NotifyUI:        true,
		},
		{
			BoxID:        serverID,
			Name:            "Certificate Expiring",
			Description:     "SSL certificate expiring soon",
			Type:            AlertTypeCertificate,
			Enabled:         true,
			Severity:        AlertSeverityWarning,
			Metric:          "days_until_expiry",
			Threshold:       14,
			Operator:        "<",
			Duration:        0,
			CooldownMinutes: 1440, // Once per day
			NotifyUI:        true,
			NotifyEmail:     true,
		},
		{
			BoxID:        serverID,
			Name:            "Security Threat Detected",
			Description:     "High threat score IP detected",
			Type:            AlertTypeSecurity,
			Enabled:         true,
			Severity:        AlertSeverityWarning,
			Metric:          "threat_score",
			Threshold:       80,
			Operator:        ">",
			Duration:        0,
			CooldownMinutes: 5,
			NotifyUI:        true,
		},
		{
			BoxID:        serverID,
			Name:            "Service Down",
			Description:     "A monitored service has stopped",
			Type:            AlertTypeServiceDown,
			Enabled:         true,
			Severity:        AlertSeverityCritical,
			Duration:        0,
			CooldownMinutes: 5,
			NotifyUI:        true,
			NotifyEmail:     true,
		},
		{
			BoxID:        serverID,
			Name:            "Certificate Expired",
			Description:     "SSL certificate has expired",
			Type:            AlertTypeCertificate,
			Enabled:         true,
			Severity:        AlertSeverityCritical,
			Metric:          "days_until_expiry",
			Threshold:       0,
			Operator:        "<=",
			Duration:        0,
			CooldownMinutes: 1440, // Once per day
			NotifyUI:        true,
			NotifyEmail:     true,
		},
		{
			BoxID:        serverID,
			Name:            "Security Updates Available",
			Description:     "Security updates are pending installation",
			Type:            AlertTypeOSUpdate,
			Enabled:         true,
			Severity:        AlertSeverityWarning,
			Metric:          "security_updates",
			Threshold:       1,
			Operator:        ">=",
			Duration:        0,
			CooldownMinutes: 1440, // Once per day
			NotifyUI:        true,
			NotifyEmail:     true,
		},
		{
			BoxID:        serverID,
			Name:            "Reboot Required",
			Description:     "System requires a reboot to complete updates",
			Type:            AlertTypeOSUpdate,
			Enabled:         true,
			Severity:        AlertSeverityInfo,
			Metric:          "reboot_required",
			Threshold:       1,
			Operator:        "==",
			Duration:        0,
			CooldownMinutes: 1440, // Once per day
			NotifyUI:        true,
		},
		{
			BoxID:        serverID,
			Name:            "Many Updates Pending",
			Description:     "Large number of package updates available",
			Type:            AlertTypeOSUpdate,
			Enabled:         true,
			Severity:        AlertSeverityInfo,
			Metric:          "total_updates",
			Threshold:       20,
			Operator:        ">=",
			Duration:        0,
			CooldownMinutes: 1440, // Once per day
			NotifyUI:        true,
		},
	}
}

// IsActive returns true if the alert is in active state
func (a *Alert) IsActive() bool {
	return a.Status == AlertStatusActive
}

// CanNotify returns true if the alert can send notifications
func (a *Alert) CanNotify() bool {
	return a.Status == AlertStatusActive && a.SilencedUntil == nil
}

// SeverityColor returns the Tailwind CSS color class for the severity
func (a *Alert) SeverityColor() string {
	switch a.Severity {
	case AlertSeverityCritical:
		return "red"
	case AlertSeverityWarning:
		return "yellow"
	default:
		return "blue"
	}
}
