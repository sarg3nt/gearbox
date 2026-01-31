package handler

import (
	"strings"
	"testing"
)

func TestRedactConfig_StatsAuth(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
listen stats
    bind *:8404
    stats enable
    stats uri /stats
    stats auth admin:supersecretpassword
    stats refresh 10s
`

	result := r.RedactConfig(serverID, input)

	// Password should be redacted
	if strings.Contains(result, "supersecretpassword") {
		t.Error("Password was not redacted")
	}

	// Should contain redaction marker
	if !strings.Contains(result, "<redacted:stats_auth:0>") {
		t.Errorf("Expected redaction marker, got: %s", result)
	}

	// Verify restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("StatsAuth restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_MultipleStatsAuth(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
listen stats
    stats auth admin:password1
    stats auth viewer:password2
    stats auth operator:password3
`

	result := r.RedactConfig(serverID, input)

	// All passwords should be redacted
	if strings.Contains(result, "password1") || strings.Contains(result, "password2") || strings.Contains(result, "password3") {
		t.Errorf("Not all passwords were redacted: %s", result)
	}

	// Should have unique markers
	if !strings.Contains(result, "<redacted:stats_auth:0>") ||
		!strings.Contains(result, "<redacted:stats_auth:1>") ||
		!strings.Contains(result, "<redacted:stats_auth:2>") {
		t.Errorf("Expected unique markers, got: %s", result)
	}

	// Verify full restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("Multiple StatsAuth restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_UserPassword(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
userlist myusers
    user admin password mysecretpwd groups admins
    user viewer insecure-password viewerpass groups viewers
`

	result := r.RedactConfig(serverID, input)

	if strings.Contains(result, "mysecretpwd") {
		t.Error("User password was not redacted")
	}
	if strings.Contains(result, "viewerpass") {
		t.Error("User insecure-password was not redacted")
	}

	// Verify restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("UserPassword restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_HTTPAuthHeader(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
frontend http_front
    http-request set-header Authorization Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9
    http-request add-header X-Api-Key sk-1234567890abcdef
`

	result := r.RedactConfig(serverID, input)

	if strings.Contains(result, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
		t.Error("Authorization header value was not redacted")
	}
	if strings.Contains(result, "sk-1234567890abcdef") {
		t.Error("X-Api-Key header value was not redacted")
	}

	// Verify restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("HTTPAuthHeader restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_AWSAccessKey(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
# AWS credentials for something
# AKIAIOSFODNN7EXAMPLE
`

	result := r.RedactConfig(serverID, input)

	if strings.Contains(result, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("AWS Access Key was not redacted")
	}

	// Verify restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("AWSAccessKey restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_GitHubPAT(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
# GitHub token: ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
`

	result := r.RedactConfig(serverID, input)

	if strings.Contains(result, "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx") {
		t.Error("GitHub PAT was not redacted")
	}

	// Verify restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("GitHubPAT restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_GenericPasswordAssignment(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
# Configuration
# password=mysecret123
# secret: anothersecret
# token = mytoken456
`

	result := r.RedactConfig(serverID, input)

	if strings.Contains(result, "mysecret123") {
		t.Error("password= value was not redacted")
	}
	if strings.Contains(result, "anothersecret") {
		t.Error("secret: value was not redacted")
	}
	if strings.Contains(result, "mytoken456") {
		t.Error("token= value was not redacted")
	}

	// Verify restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("GenericPasswordAssignment restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_DatabaseConnectionString(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
# Database: mysql://user:mydbpassword@localhost:3306/db
# Redis: redis://default:redispass@cache.example.com:6379
`

	result := r.RedactConfig(serverID, input)

	if strings.Contains(result, "mydbpassword") {
		t.Error("MySQL password was not redacted")
	}
	if strings.Contains(result, "redispass") {
		t.Error("Redis password was not redacted")
	}

	// Verify restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("DatabaseConnectionString restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_HTTPBasicAuthURL(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
# Backend: https://apiuser:apipassword123@api.example.com/v1
`

	result := r.RedactConfig(serverID, input)

	if strings.Contains(result, "apipassword123") {
		t.Error("HTTP Basic Auth password in URL was not redacted")
	}

	// Verify restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("HTTPBasicAuthURL restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_PrivateKeyInline(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy
base64encodedkeydata1234567890abcdefghijklmnop
-----END RSA PRIVATE KEY-----
`

	result := r.RedactConfig(serverID, input)

	// The key content should be redacted, but headers preserved
	if strings.Contains(result, "MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn") {
		t.Error("Private key content was not redacted")
	}
	if !strings.Contains(result, "-----BEGIN RSA PRIVATE KEY-----") {
		t.Error("Private key header should be preserved")
	}
	if !strings.Contains(result, "-----END RSA PRIVATE KEY-----") {
		t.Error("Private key footer should be preserved")
	}

	// Verify restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("PrivateKeyInline restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_UserChangedValue(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
listen stats
    stats auth admin:oldpassword
`

	// First, redact the config
	redacted := r.RedactConfig(serverID, input)

	// Simulate user changing the password (replacing the marker)
	modified := strings.Replace(redacted, "<redacted:stats_auth:0>", "newpassword", 1)

	// Restore should keep the new password (marker is gone, so nothing to restore)
	restored, err := r.RestoreConfig(serverID, modified)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}

	if !strings.Contains(restored, "newpassword") {
		t.Error("User's new password should be preserved")
	}
	if strings.Contains(restored, "oldpassword") {
		t.Error("Old password should not be restored when user changed it")
	}
}

func TestRedactConfig_PartialUserChange(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
listen stats
    stats auth admin:password1
    stats auth viewer:password2
`

	// Redact
	redacted := r.RedactConfig(serverID, input)

	// User changes only the first password, leaves second as marker
	modified := strings.Replace(redacted, "<redacted:stats_auth:0>", "newadminpass", 1)

	// Restore
	restored, err := r.RestoreConfig(serverID, modified)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}

	// First password should be the new one
	if !strings.Contains(restored, "newadminpass") {
		t.Error("Changed admin password should be kept")
	}
	// Second password should be restored to original
	if !strings.Contains(restored, "password2") {
		t.Error("Unchanged viewer password should be restored")
	}
}

func TestRedactConfig_NoSensitiveData(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
global
    maxconn 4096
    log /dev/log local0

defaults
    mode http
    timeout connect 5s
    timeout client 30s
    timeout server 30s

frontend http_front
    bind *:80
    default_backend servers

backend servers
    balance roundrobin
    server srv1 10.0.0.1:8080 check
`

	result := r.RedactConfig(serverID, input)

	// Should be unchanged
	if result != input {
		t.Errorf("Config without sensitive data should be unchanged.\nExpected:\n%s\nGot:\n%s", input, result)
	}
}

func TestGetRedactedCount(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
listen stats
    stats auth admin:pass1
    stats auth viewer:pass2
# password=secret123
`

	r.RedactConfig(serverID, input)

	count := r.GetRedactedCount(serverID)
	if count != 3 {
		t.Errorf("Expected 3 redacted values, got %d", count)
	}
}

func TestGetRedactedPatterns(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `
listen stats
    stats auth admin:pass1
# password=secret123
`

	r.RedactConfig(serverID, input)

	patterns := r.GetRedactedPatterns(serverID)
	if len(patterns) != 2 {
		t.Errorf("Expected 2 pattern types, got %d: %v", len(patterns), patterns)
	}

	// Check that expected patterns are present
	hasStatsAuth := false
	hasGenericPassword := false
	for _, p := range patterns {
		if p == "stats_auth" {
			hasStatsAuth = true
		}
		if p == "generic_password_assignment" {
			hasGenericPassword = true
		}
	}
	if !hasStatsAuth {
		t.Error("Expected stats_auth pattern to be in results")
	}
	if !hasGenericPassword {
		t.Error("Expected generic_password_assignment pattern to be in results")
	}
}

func TestClearServerCache(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `stats auth admin:password`
	r.RedactConfig(serverID, input)

	if r.GetRedactedCount(serverID) == 0 {
		t.Error("Should have redacted values before clear")
	}

	r.ClearServerCache(serverID)

	if r.GetRedactedCount(serverID) != 0 {
		t.Error("Should have no redacted values after clear")
	}
}

func TestRedactConfig_MultipleServers(t *testing.T) {
	r := NewConfigRedactor()

	input1 := `stats auth admin:server1pass`
	input2 := `stats auth admin:server2pass`

	r.RedactConfig("server1", input1)
	r.RedactConfig("server2", input2)

	// Each server should have its own cache
	if r.GetRedactedCount("server1") != 1 {
		t.Error("Server1 should have 1 redacted value")
	}
	if r.GetRedactedCount("server2") != 1 {
		t.Error("Server2 should have 1 redacted value")
	}

	// Restore should work independently
	redacted1 := `stats auth admin:<redacted:stats_auth:0>`
	restored1, err := r.RestoreConfig("server1", redacted1)
	if err != nil {
		t.Fatalf("RestoreConfig for server1 failed: %v", err)
	}
	if !strings.Contains(restored1, "server1pass") {
		t.Error("Server1 password should be restored correctly")
	}

	restored2, err := r.RestoreConfig("server2", redacted1)
	if err != nil {
		t.Fatalf("RestoreConfig for server2 failed: %v", err)
	}
	if !strings.Contains(restored2, "server2pass") {
		t.Error("Server2 password should be restored correctly")
	}
}

func TestRedactConfig_JWTToken(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	// A valid JWT structure (header.payload.signature)
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	input := `# Token: ` + jwt

	result := r.RedactConfig(serverID, input)

	if strings.Contains(result, jwt) {
		t.Error("JWT token was not redacted")
	}

	// Verify restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("JWT restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_SlackToken(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	input := `# Slack: xoxb-1234567890-1234567890123-abcdefghijABCDEFGHIJ`

	result := r.RedactConfig(serverID, input)

	if strings.Contains(result, "xoxb-1234567890-1234567890123") {
		t.Error("Slack token was not redacted")
	}

	// Verify restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("Slack token restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_AlreadyRedacted(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	// Input already has redacted marker
	input := `
listen stats
    stats auth admin:<redacted:stats_auth:0>
`

	result := r.RedactConfig(serverID, input)

	// Should not double-redact
	if result != input {
		t.Errorf("Already redacted content should not change.\nExpected:\n%s\nGot:\n%s", input, result)
	}

	// Count should be 0 since nothing was actually redacted
	if r.GetRedactedCount(serverID) != 0 {
		t.Error("Should not store already-redacted values")
	}
}

func TestRedactConfig_MarkerIntegrity(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	// Multiple different types of secrets
	input := `
listen stats
    stats auth admin:statspass
# password=commentpass
# api_key=myapikey123
`

	result := r.RedactConfig(serverID, input)

	// Each type should have its own marker
	if !strings.Contains(result, "<redacted:stats_auth:0>") {
		t.Error("Expected stats_auth marker")
	}
	if !strings.Contains(result, "<redacted:generic_password_assignment:0>") {
		t.Error("Expected generic_password_assignment marker")
	}
	if !strings.Contains(result, "<redacted:generic_api_key:0>") {
		t.Error("Expected generic_api_key marker")
	}

	// Full restoration
	restored, err := r.RestoreConfig(serverID, result)
	if err != nil {
		t.Fatalf("RestoreConfig failed: %v", err)
	}
	if restored != input {
		t.Errorf("Multi-type restoration failed.\nExpected:\n%s\nGot:\n%s", input, restored)
	}
}

func TestRedactConfig_ConcurrentAccess(t *testing.T) {
	r := NewConfigRedactor()

	// Simulate concurrent access from different servers
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(serverNum int) {
			serverID := "server-" + string(rune('A'+serverNum))
			input := "stats auth admin:password" + string(rune('0'+serverNum))

			// Redact
			redacted := r.RedactConfig(serverID, input)

			// Verify it was redacted
			if strings.Contains(redacted, "password") {
				t.Errorf("Server %s password not redacted", serverID)
			}

			// Restore
			restored, err := r.RestoreConfig(serverID, redacted)
			if err != nil {
				t.Errorf("Server %s RestoreConfig failed: %v", serverID, err)
			}

			// Verify restoration
			if restored != input {
				t.Errorf("Server %s restoration failed", serverID)
			}

			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestRestoreConfig_UnrestorableMarkers(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	// Create content with markers but don't store original values
	// This simulates a session expiry or server restart
	content := `
listen stats
    stats auth admin:<redacted:stats_auth:0>
`

	// Try to restore - should fail because we never called RedactConfig
	_, err := r.RestoreConfig(serverID, content)
	if err == nil {
		t.Error("Expected error when restoring markers without stored values")
	}

	if !strings.Contains(err.Error(), "cannot restore") {
		t.Errorf("Expected 'cannot restore' in error message, got: %v", err)
	}
}

func TestHasUnrestorableMarkers(t *testing.T) {
	r := NewConfigRedactor()
	serverID := "test-server"

	// Content with markers but no stored values
	content := `
listen stats
    stats auth admin:<redacted:stats_auth:0>
    stats auth viewer:<redacted:stats_auth:1>
`

	hasUnrestorable, count := r.HasUnrestorableMarkers(serverID, content)
	if !hasUnrestorable {
		t.Error("Should detect unrestorable markers")
	}
	if count != 2 {
		t.Errorf("Expected 2 unrestorable markers, got %d", count)
	}

	// Now redact some real content
	input := `stats auth admin:realpassword`
	r.RedactConfig(serverID, input)

	// Check content with valid marker
	validContent := `stats auth admin:<redacted:stats_auth:0>`
	hasUnrestorable, count = r.HasUnrestorableMarkers(serverID, validContent)
	if hasUnrestorable {
		t.Error("Should not detect unrestorable markers for valid content")
	}
	if count != 0 {
		t.Errorf("Expected 0 unrestorable markers, got %d", count)
	}
}
