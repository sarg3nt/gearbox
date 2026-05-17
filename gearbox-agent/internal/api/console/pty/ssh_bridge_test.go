package pty

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSSHBridgeConfigFromEnv_RejectsMissingFields(t *testing.T) {
	// All four env vars are required. Confirm each missing field
	// produces a clear "missing" error rather than silently using
	// a zero value (which would surface as a confusing dial failure
	// at first session).
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_HOST", "")
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_USER", "")
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_KEY", "")
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_HOSTKEY", "")
	_, err := LoadSSHBridgeConfigFromEnv()
	if err == nil {
		t.Fatal("LoadSSHBridgeConfigFromEnv with all empty = nil err; want error")
	}
	for _, want := range []string{"HOST", "USER", "KEY", "HOSTKEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing mention of %s", err.Error(), want)
		}
	}
}

func TestLoadSSHBridgeConfigFromEnv_RejectsWorldReadableKey(t *testing.T) {
	// Loose perms on an SSH key in the data dir are a foot-gun.
	// Catch them at startup, not at first session.
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent.key")
	if err := os.WriteFile(keyPath, []byte("not really a key"), 0o644); err != nil {
		t.Fatal(err)
	}
	hostKeyPath := filepath.Join(dir, "host.pub")
	if err := os.WriteFile(hostKeyPath, []byte("ssh-ed25519 AAAA fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_HOST", "127.0.0.1:22")
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_USER", "agent")
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_KEY", keyPath)
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_HOSTKEY", hostKeyPath)
	_, err := LoadSSHBridgeConfigFromEnv()
	if err == nil {
		t.Fatal("LoadSSHBridgeConfigFromEnv with 0644 key = nil err; want refusal")
	}
	if !strings.Contains(err.Error(), "permissions") {
		t.Errorf("error %q missing 'permissions'", err.Error())
	}
}

func TestLoadSSHBridgeConfigFromEnv_AcceptsTightKey(t *testing.T) {
	// Sanity: a properly-protected key (mode 0600) passes the
	// validation step. We don't try to actually parse it here —
	// dialAndStart does that at session time.
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent.key")
	if err := os.WriteFile(keyPath, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	hostKeyPath := filepath.Join(dir, "host.pub")
	if err := os.WriteFile(hostKeyPath, []byte("ssh-ed25519 AAAA fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_HOST", "127.0.0.1:22")
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_USER", "agent")
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_KEY", keyPath)
	t.Setenv("HAPROXY_AGENT_CONSOLE_SSH_HOSTKEY", hostKeyPath)
	cfg, err := LoadSSHBridgeConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadSSHBridgeConfigFromEnv = %v, want nil err", err)
	}
	if cfg.Host != "127.0.0.1:22" || cfg.User != "agent" {
		t.Errorf("cfg = %+v, want host=127.0.0.1:22 user=agent", cfg)
	}
}

func TestSSHBridgeSpawner_RejectsRunAs(t *testing.T) {
	cfg := &SSHBridgeConfig{Host: "x", User: "y", PrivateKey: "z", HostKey: "w"}
	sp := SSHBridgeSpawner(cfg)
	_, err := sp(context.Background(), []string{"/bin/true"}, "1000", 80, 24)
	if err == nil {
		t.Fatal("SSHBridgeSpawner with run-as = nil err; want explicit refusal")
	}
	if !strings.Contains(err.Error(), "HAPROXY_AGENT_CONSOLE_SSH_USER") {
		t.Errorf("error %q should suggest the env var", err.Error())
	}
}

func TestSSHBridgeSpawner_RejectsEmptyCommand(t *testing.T) {
	cfg := &SSHBridgeConfig{Host: "x", User: "y", PrivateKey: "z", HostKey: "w"}
	sp := SSHBridgeSpawner(cfg)
	_, err := sp(context.Background(), nil, "", 80, 24)
	if err == nil {
		t.Fatal("SSHBridgeSpawner with empty cmd = nil err; want refusal")
	}
}
