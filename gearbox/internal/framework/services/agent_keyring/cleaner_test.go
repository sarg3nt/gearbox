package agent_keyring

import (
	"context"
	"testing"
	"time"
)

func TestCleaner_RemovesRetiredKeyOnTick(t *testing.T) {
	rotator, mock, db, box := setupRotator(t)
	if _, err := rotator.RotateBox(box.ID, 24*time.Hour); err != nil {
		t.Fatalf("RotateBox: %v", err)
	}
	if mock.entryCount() != 2 {
		t.Fatalf("post-rotation agent entries = %d, want 2", mock.entryCount())
	}

	// Tiny overlap + tiny interval so the cleaner sweeps quickly and
	// considers the just-retired legacy entry eligible.
	cleaner := NewCleaner(rotator, db, 1*time.Millisecond, 20*time.Millisecond, rotator.logger)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		cleaner.Run(ctx)
		close(done)
	}()

	// Give it time for the immediate-on-start sweep to fire.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if got := mock.entryCount(); got != 1 {
		t.Errorf("agent entries after cleaner sweep = %d, want 1", got)
	}
	keys, _ := db.GetBoxAgentKeys(box.ID)
	if len(keys) != 1 {
		t.Errorf("db keys after cleaner sweep = %d, want 1", len(keys))
	}
}

func TestCleaner_NoopWhenNothingRetired(t *testing.T) {
	rotator, mock, db, box := setupRotator(t)
	// No rotation = no retired keys. Cleaner sweep should leave the
	// single legacy primary alone.

	cleaner := NewCleaner(rotator, db, 1*time.Millisecond, 1*time.Hour, rotator.logger)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cleaner.Run(ctx)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	if got := mock.entryCount(); got != 1 {
		t.Errorf("mock entries = %d, want 1", got)
	}
	keys, _ := db.GetBoxAgentKeys(box.ID)
	if len(keys) != 1 {
		t.Errorf("db keys = %d, want 1", len(keys))
	}
}
