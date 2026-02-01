package alerts

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/collector"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/events"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// Evaluator evaluates alert rules against current metrics and triggers alerts.
type Evaluator struct {
	db         *database.DB
	registry   *collector.Registry
	eventHub   *events.Hub
	logger     *slog.Logger
	stopCh     chan struct{}
	wg         sync.WaitGroup

	// Track rule state for duration-based alerts
	ruleState  map[int64]*ruleState
	stateMu    sync.RWMutex
}

// ruleState tracks the state of a rule for duration evaluation.
type ruleState struct {
	conditionMetSince *time.Time
	lastAlertTime     *time.Time
}

// NewEvaluator creates a new alert evaluator.
func NewEvaluator(db *database.DB, registry *collector.Registry, eventHub *events.Hub, logger *slog.Logger) *Evaluator {
	return &Evaluator{
		db:        db,
		registry:  registry,
		eventHub:  eventHub,
		logger:    logger,
		stopCh:    make(chan struct{}),
		ruleState: make(map[int64]*ruleState),
	}
}

// Start begins the alert evaluation loop.
func (e *Evaluator) Start(interval time.Duration) {
	e.logger.Info("Starting alert evaluator")
	e.wg.Add(1)
	go e.evaluationLoop(interval)
}

// Stop halts the alert evaluator.
func (e *Evaluator) Stop() {
	e.logger.Info("Stopping alert evaluator")
	close(e.stopCh)
	e.wg.Wait()
}

func (e *Evaluator) evaluationLoop(interval time.Duration) {
	defer e.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial evaluation
	e.evaluateAllServers()

	for {
		select {
		case <-ticker.C:
			e.evaluateAllServers()
		case <-e.stopCh:
			return
		}
	}
}

func (e *Evaluator) evaluateAllServers() {
	collectors := e.registry.GetAllCollectors()
	for serverID, mgr := range collectors {
		if err := e.evaluateServer(serverID, mgr); err != nil {
			e.logger.Error("error evaluating alerts for server",
				"server_id", serverID,
				"error", err)
		}
	}
}

func (e *Evaluator) evaluateServer(serverID string, mgr *collector.Manager) error {
	// Ensure default rules exist
	if err := e.db.EnsureDefaultAlertRules(serverID); err != nil {
		return fmt.Errorf("failed to ensure default rules: %w", err)
	}

	// Get enabled rules
	rules, err := e.db.GetEnabledAlertRules(serverID)
	if err != nil {
		return fmt.Errorf("failed to get rules: %w", err)
	}

	// Get current metrics (also contains ServiceStatuses)
	metrics, _, ok := mgr.GetSystemMetrics()
	if !ok {
		return nil // No metrics available yet
	}

	// Get current stats
	stats, _, _ := mgr.GetStats()

	// Get certificates from cache for certificate expiry alerts
	cache := mgr.GetCache()
	certificates, _, _ := cache.GetCertificates()

	// Evaluate each rule
	for _, rule := range rules {
		if err := e.evaluateRule(&rule, metrics, stats, certificates); err != nil {
			e.logger.Error("error evaluating rule",
				"rule_name", rule.Name,
				"error", err)
		}
	}

	return nil
}

func (e *Evaluator) evaluateRule(rule *models.AlertRule, metrics *models.SystemMetrics, stats *models.HAProxyStats, certs *models.CertificateMetrics) error {
	// Check if in cooldown period
	if e.isInCooldown(rule) {
		return nil
	}

	// Evaluate based on rule type
	var conditionMet bool
	var currentValue float64
	var affectedEntity string

	switch rule.Type {
	case models.AlertTypeMetric:
		conditionMet, currentValue = e.evaluateMetricRule(rule, metrics)
	case models.AlertTypeBackendDown:
		conditionMet, affectedEntity = e.evaluateBackendDownRule(rule, stats)
	case models.AlertTypeFrontendDown:
		conditionMet, affectedEntity = e.evaluateFrontendDownRule(rule, stats)
	case models.AlertTypeTraffic:
		conditionMet, currentValue = e.evaluateTrafficRule(rule, stats)
	case models.AlertTypeCertificate:
		conditionMet, currentValue, affectedEntity = e.evaluateCertificateRule(rule, certs)
	case models.AlertTypeServiceDown:
		conditionMet, affectedEntity = e.evaluateServiceDownRule(rule, metrics)
	default:
		return nil // Skip unknown rule types
	}

	// Handle duration requirement
	if conditionMet {
		if rule.Duration > 0 {
			conditionMet = e.checkDuration(rule)
		}
	} else {
		e.clearDurationState(rule.ID)
	}

	if conditionMet {
		return e.triggerAlert(rule, currentValue, affectedEntity)
	}

	return nil
}

func (e *Evaluator) evaluateMetricRule(rule *models.AlertRule, metrics *models.SystemMetrics) (bool, float64) {
	if metrics == nil {
		return false, 0
	}

	var value float64
	switch rule.Metric {
	case "cpu_percent":
		value = metrics.CPUUsagePercent
	case "memory_percent":
		value = metrics.MemoryUsagePercent
	case "disk_percent":
		value = metrics.DiskUsagePercent
	case "load_average_1":
		value = metrics.LoadAverage1
	case "load_average_5":
		value = metrics.LoadAverage5
	case "load_average_15":
		value = metrics.LoadAverage15
	default:
		return false, 0
	}

	return e.compareValue(value, rule.Operator, rule.Threshold), value
}

func (e *Evaluator) evaluateBackendDownRule(rule *models.AlertRule, stats *models.HAProxyStats) (bool, string) {
	if stats == nil {
		return false, ""
	}

	for _, backend := range stats.Backends {
		if backend.Status == "DOWN" {
			// Check if this backend has health monitoring disabled
			disabled, err := e.db.IsEntityDisabled(rule.BoxID, database.EntityTypeBackend, backend.Name)
			if err != nil {
				e.logger.Error("Error checking if backend %s is disabled: %v", backend.Name, err)
			}
			if disabled {
				continue // Skip disabled backends
			}
			return true, backend.Name
		}
	}

	return false, ""
}

func (e *Evaluator) evaluateFrontendDownRule(rule *models.AlertRule, stats *models.HAProxyStats) (bool, string) {
	if stats == nil {
		return false, ""
	}

	for _, frontend := range stats.Frontends {
		if frontend.Status == "STOP" || frontend.Status == "DOWN" {
			// Check if this frontend has health monitoring disabled
			disabled, err := e.db.IsEntityDisabled(rule.BoxID, database.EntityTypeFrontend, frontend.Name)
			if err != nil {
				e.logger.Error("Error checking if frontend %s is disabled: %v", frontend.Name, err)
			}
			if disabled {
				continue // Skip disabled frontends
			}
			return true, frontend.Name
		}
	}

	return false, ""
}

func (e *Evaluator) evaluateTrafficRule(rule *models.AlertRule, stats *models.HAProxyStats) (bool, float64) {
	if stats == nil {
		return false, 0
	}

	var totalRequests, totalErrors int64
	for _, backend := range stats.Backends {
		totalRequests += backend.RequestTotal
		totalErrors += backend.Response4xx + backend.Response5xx
	}

	if totalRequests == 0 {
		return false, 0
	}

	errorRate := float64(totalErrors) / float64(totalRequests) * 100
	return e.compareValue(errorRate, rule.Operator, rule.Threshold), errorRate
}

func (e *Evaluator) evaluateCertificateRule(rule *models.AlertRule, certs *models.CertificateMetrics) (bool, float64, string) {
	if certs == nil || len(certs.Certificates) == 0 {
		return false, 0, ""
	}

	// Check each certificate for expiry
	for _, cert := range certs.Certificates {
		daysUntilExpiry := float64(cert.DaysUntilExpiry)

		// Evaluate based on the rule's operator and threshold
		if e.compareValue(daysUntilExpiry, rule.Operator, rule.Threshold) {
			return true, daysUntilExpiry, cert.Domain
		}
	}

	return false, 0, ""
}

func (e *Evaluator) evaluateServiceDownRule(rule *models.AlertRule, metrics *models.SystemMetrics) (bool, string) {
	if metrics == nil || len(metrics.ServiceStatuses) == 0 {
		return false, ""
	}

	// Check each monitored service
	for name, status := range metrics.ServiceStatuses {
		// A service is considered "down" if it's not active
		if !status.Active || status.Status == "failed" || status.Status == "inactive" {
			// Check if this service has health monitoring disabled
			disabled, err := e.db.IsEntityDisabled(rule.BoxID, database.EntityTypeService, name)
			if err != nil {
				e.logger.Error("Error checking if service %s is disabled: %v", name, err)
			}
			if disabled {
				continue // Skip disabled services
			}
			return true, name
		}
	}

	return false, ""
}

func (e *Evaluator) compareValue(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	default:
		return false
	}
}

func (e *Evaluator) checkDuration(rule *models.AlertRule) bool {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	state, exists := e.ruleState[rule.ID]
	if !exists {
		now := time.Now()
		e.ruleState[rule.ID] = &ruleState{conditionMetSince: &now}
		return false
	}

	if state.conditionMetSince == nil {
		now := time.Now()
		state.conditionMetSince = &now
		return false
	}

	elapsed := time.Since(*state.conditionMetSince)
	return elapsed >= time.Duration(rule.Duration)*time.Second
}

func (e *Evaluator) clearDurationState(ruleID int64) {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	if state, exists := e.ruleState[ruleID]; exists {
		state.conditionMetSince = nil
	}
}

func (e *Evaluator) isInCooldown(rule *models.AlertRule) bool {
	// Get last alert for this rule
	lastAlert, err := e.db.GetLastAlertForRule(rule.ID)
	if err != nil || lastAlert == nil {
		return false
	}

	cooldown := time.Duration(rule.CooldownMinutes) * time.Minute
	return time.Since(lastAlert.TriggeredAt) < cooldown
}

// isSuppressedByAck checks if alerts for this rule should be suppressed due to
// a recently acknowledged alert (based on the alerts integration config).
func (e *Evaluator) isSuppressedByAck(rule *models.AlertRule, affectedEntity string) bool {
	// Get alerts integration config for this server
	config, err := e.db.GetAlertsConfig(rule.BoxID)
	if err != nil || config == nil {
		return false
	}

	// Check if suppression is enabled
	if !config.SuppressAfterAck || config.SuppressMinutes <= 0 {
		return false
	}

	// Get recently acknowledged alerts for this rule and entity
	recentAckAlert, err := e.db.GetRecentlyAcknowledgedAlertForRule(rule.ID, affectedEntity, config.SuppressMinutes)
	if err != nil {
		e.logger.Error("Error checking for recently acknowledged alert: %v", err)
		return false
	}

	return recentAckAlert != nil
}

func (e *Evaluator) triggerAlert(rule *models.AlertRule, value float64, affectedEntity string) error {
	// Check for existing active/acknowledged alert for the same rule and entity (deduplication)
	existingAlert, err := e.db.GetActiveAlertForRuleAndEntity(rule.ID, affectedEntity)
	if err != nil {
		e.logger.Error("Error checking for existing alert: %v", err)
	}
	if existingAlert != nil {
		// An alert already exists for this incident - don't create a duplicate
		// However, for metric-based alerts, check if the condition has significantly worsened
		if existingAlert.Status == models.AlertStatusAcknowledged && rule.Type == models.AlertTypeMetric {
			// Check if value is significantly worse (20% above the acknowledged value)
			if existingAlert.MetricValue > 0 && value > existingAlert.MetricValue*1.2 {
				// Condition worsened significantly - create a new alert
				e.logger.Error("Metric worsened significantly from %.2f to %.2f, creating new alert", existingAlert.MetricValue, value)
			} else {
				return nil
			}
		} else {
			return nil
		}
	}

	// Check for suppression after acknowledgement
	if e.isSuppressedByAck(rule, affectedEntity) {
		return nil
	}

	alert := &models.Alert{
		RuleID:         rule.ID,
		BoxID:          rule.BoxID,
		Type:           rule.Type,
		Severity:       rule.Severity,
		Status:         models.AlertStatusActive,
		Title:          rule.Name,
		Message:        e.formatAlertMessage(rule, value, affectedEntity),
		Metric:         rule.Metric,
		MetricValue:    value,
		Threshold:      rule.Threshold,
		AffectedEntity: affectedEntity,
		TriggeredAt:    time.Now(),
	}

	id, err := e.db.CreateAlert(alert)
	if err != nil {
		return fmt.Errorf("failed to create alert: %w", err)
	}
	alert.ID = id

	e.logger.Error("Alert triggered: %s (severity: %s)", alert.Title, alert.Severity)

	// Publish event for real-time updates
	if e.eventHub != nil {
		e.eventHub.Publish(events.Event{
			Type:     "alert.triggered",
			ServerID: rule.BoxID,
			Data: map[string]any{
				"alert_id":        alert.ID,
				"title":           alert.Title,
				"message":         alert.Message,
				"severity":        alert.Severity,
				"type":            alert.Type,
				"affected_entity": alert.AffectedEntity,
			},
		})
	}

	// Update last alert time in state
	e.stateMu.Lock()
	if state, exists := e.ruleState[rule.ID]; exists {
		now := time.Now()
		state.lastAlertTime = &now
		state.conditionMetSince = nil // Reset duration tracking
	}
	e.stateMu.Unlock()

	// TODO: Send notifications (email, webhook, etc.)

	return nil
}

func (e *Evaluator) formatAlertMessage(rule *models.AlertRule, value float64, affectedEntity string) string {
	switch rule.Type {
	case models.AlertTypeMetric:
		return fmt.Sprintf("%s is at %.2f%% (threshold: %.2f%%)", rule.Metric, value, rule.Threshold)
	case models.AlertTypeBackendDown:
		return fmt.Sprintf("Backend '%s' is DOWN", affectedEntity)
	case models.AlertTypeFrontendDown:
		return fmt.Sprintf("Frontend '%s' is DOWN", affectedEntity)
	case models.AlertTypeTraffic:
		return fmt.Sprintf("Error rate is %.2f%% (threshold: %.2f%%)", value, rule.Threshold)
	case models.AlertTypeCertificate:
		if value <= 0 {
			return fmt.Sprintf("Certificate for '%s' has EXPIRED", affectedEntity)
		}
		return fmt.Sprintf("Certificate for '%s' expires in %.0f days (threshold: %.0f days)", affectedEntity, value, rule.Threshold)
	case models.AlertTypeServiceDown:
		return fmt.Sprintf("Service '%s' is not running", affectedEntity)
	default:
		return rule.Description
	}
}

// ResolveAlertIfConditionCleared auto-resolves alerts when condition clears.
// This should be called periodically or when metrics change.
func (e *Evaluator) ResolveAlertIfConditionCleared(serverID string, mgr *collector.Manager) error {
	activeAlerts, err := e.db.GetActiveAlerts(serverID)
	if err != nil {
		return err
	}

	metrics, _, _ := mgr.GetSystemMetrics()
	stats, _, _ := mgr.GetStats()

	// Get certificates from cache
	cache := mgr.GetCache()
	certs, _, _ := cache.GetCertificates()

	for _, alert := range activeAlerts {
		if alert.RuleID == 0 {
			continue // Manual alert without a rule
		}

		// Get the rule
		rules, err := e.db.GetAlertRules(serverID)
		if err != nil {
			continue
		}

		var rule *models.AlertRule
		for i := range rules {
			if rules[i].ID == alert.RuleID {
				rule = &rules[i]
				break
			}
		}
		if rule == nil {
			continue
		}

		// Check if condition is still met
		conditionMet := false
		switch rule.Type {
		case models.AlertTypeMetric:
			conditionMet, _ = e.evaluateMetricRule(rule, metrics)
		case models.AlertTypeBackendDown:
			conditionMet, _ = e.evaluateBackendDownRule(rule, stats)
		case models.AlertTypeFrontendDown:
			conditionMet, _ = e.evaluateFrontendDownRule(rule, stats)
		case models.AlertTypeTraffic:
			conditionMet, _ = e.evaluateTrafficRule(rule, stats)
		case models.AlertTypeCertificate:
			conditionMet, _, _ = e.evaluateCertificateRule(rule, certs)
		case models.AlertTypeServiceDown:
			conditionMet, _ = e.evaluateServiceDownRule(rule, metrics)
		}

		// Auto-resolve if condition is no longer met
		if !conditionMet {
			if err := e.db.ResolveAlert(alert.ID); err != nil {
				e.logger.Error("Failed to auto-resolve alert %d: %v", alert.ID, err)
			} else {
				e.logger.Error("Alert auto-resolved: %s", alert.Title)
				if e.eventHub != nil {
					e.eventHub.Publish(events.Event{
						Type:     "alert.resolved",
						ServerID: serverID,
						Data: map[string]any{
							"alert_id": alert.ID,
							"title":    alert.Title,
						},
					})
				}
			}
		}
	}

	return nil
}
