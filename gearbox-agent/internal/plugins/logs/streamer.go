// Package logs provides log collection and streaming functionality.
package logs

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

// LogLine represents a single log line with metadata.
type LogLine struct {
	Source    string    `json:"source"`
	Line      string    `json:"line"`
	Timestamp time.Time `json:"timestamp"`
}

// Streamer streams logs from configured sources in real-time.
type Streamer struct {
	sources  []LogSource
	eventBus *events.Bus
	logger   *slog.Logger

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	mu        sync.RWMutex
	active    map[string]context.CancelFunc // source name -> cancel func
	listeners map[string]struct{}           // active source listeners
}

// NewStreamer creates a new log streamer.
func NewStreamer(sources []LogSource, eventBus *events.Bus, logger *slog.Logger) *Streamer {
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Streamer{
		sources:   sources,
		eventBus:  eventBus,
		logger:    logger,
		ctx:       ctx,
		cancel:    cancel,
		active:    make(map[string]context.CancelFunc),
		listeners: make(map[string]struct{}),
	}
}

// StartSource starts streaming a specific log source.
func (s *Streamer) StartSource(sourceName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Already streaming this source
	if _, exists := s.active[sourceName]; exists {
		return nil
	}

	// Find the source
	var source *LogSource
	for i := range s.sources {
		if s.sources[i].Name == sourceName {
			source = &s.sources[i]
			break
		}
	}
	if source == nil {
		s.logger.Warn("unknown log source for streaming", "source", sourceName)
		return nil
	}

	// Create cancelable context for this source
	sourceCtx, sourceCancel := context.WithCancel(s.ctx)
	s.active[sourceName] = sourceCancel

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.active, sourceName)
			s.mu.Unlock()
		}()

		s.streamSource(sourceCtx, *source)
	}()

	s.logger.Info("started log streaming", "source", sourceName)
	return nil
}

// StopSource stops streaming a specific log source.
func (s *Streamer) StopSource(sourceName string) {
	s.mu.Lock()
	cancel, exists := s.active[sourceName]
	s.mu.Unlock()

	if exists {
		cancel()
		s.logger.Info("stopped log streaming", "source", sourceName)
	}
}

// StartAll starts streaming all configured sources.
func (s *Streamer) StartAll() {
	for _, source := range s.sources {
		if err := s.StartSource(source.Name); err != nil {
			s.logger.Error("failed to start source streaming",
				"source", source.Name,
				"error", err)
		}
	}
}

// Stop stops all log streaming.
func (s *Streamer) Stop() {
	s.cancel()
	s.wg.Wait()
	s.logger.Info("log streamer stopped")
}

// GetActiveSources returns the list of currently streaming sources.
func (s *Streamer) GetActiveSources() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sources := make([]string, 0, len(s.active))
	for name := range s.active {
		sources = append(sources, name)
	}
	return sources
}

// streamSource handles streaming for a single log source.
func (s *Streamer) streamSource(ctx context.Context, source LogSource) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := s.runTail(ctx, source); err != nil {
				if ctx.Err() != nil {
					return // Context cancelled, exit cleanly
				}
				s.logger.Error("log tail failed, restarting",
					"source", source.Name,
					"error", err)
				// Brief pause before retry
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
	}
}

// runTail executes the appropriate tail command for a source.
func (s *Streamer) runTail(ctx context.Context, source LogSource) error {
	var cmd *exec.Cmd

	// Build the appropriate command based on source type
	switch {
	case source.Unit != "":
		// Systemd unit: journalctl -f -u <unit>
		cmd = exec.CommandContext(ctx, "journalctl", "-f", "-u", source.Unit, "--no-pager", "-n", "0")

	case source.Type == SourceTypeFile && source.FilePath != "":
		// File: tail -F <file>
		cmd = exec.CommandContext(ctx, "tail", "-F", "-n", "0", source.FilePath)

	case source.Type == SourceTypeJournalKernel:
		// Kernel logs: journalctl -k -f
		cmd = exec.CommandContext(ctx, "journalctl", "-k", "-f", "--no-pager", "-n", "0")

	case source.Type == SourceTypeJournalPriority:
		// Priority filtered: journalctl -f -p <priority>
		args := []string{"-f", "--no-pager", "-n", "0"}
		if source.Priority != "" {
			args = append(args, "-p", source.Priority)
		}
		cmd = exec.CommandContext(ctx, "journalctl", args...)

	case source.Type == SourceTypeJournalBoot:
		// Boot logs: journalctl -b -f
		args := []string{"-b", "-f", "--no-pager", "-n", "0"}
		if source.Priority != "" {
			args = append(args, "-p", source.Priority)
		}
		cmd = exec.CommandContext(ctx, "journalctl", args...)

	case source.Type == SourceTypeJournalSSH:
		// SSH logs: journalctl _COMM=sshd -f
		cmd = exec.CommandContext(ctx, "journalctl", "-f", "--no-pager", "-n", "0", "_COMM=sshd")

	default:
		s.logger.Warn("unsupported source type for streaming", "source", source.Name, "type", source.Type)
		return nil
	}

	// Get stdout pipe
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return err
	}

	// Read lines and publish events
	s.readAndPublish(ctx, source, stdout)

	// Wait for command to finish
	return cmd.Wait()
}

// readAndPublish reads lines from a reader and publishes log events.
func (s *Streamer) readAndPublish(ctx context.Context, source LogSource, reader io.Reader) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			line := scanner.Text()

			// Apply filter if configured
			if source.Filter != "" && !matchesPattern(line, source.Filter) {
				continue
			}

			// Publish event
			s.eventBus.Publish(events.Event{
				Type:      events.EventLogsUpdated,
				Timestamp: time.Now(),
				Data: map[string]any{
					"source":    source.Name,
					"line":      line,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				},
			})
		}
	}
}

// AddListener marks a source as having an active listener.
// This is used to track which sources should be streaming.
func (s *Streamer) AddListener(sourceName string) {
	s.mu.Lock()
	s.listeners[sourceName] = struct{}{}
	s.mu.Unlock()

	// Start streaming if not already active
	if err := s.StartSource(sourceName); err != nil {
		s.logger.Error("failed to start source from listener", "source", sourceName, "error", err)
	}
}

// RemoveListener removes a listener for a source.
// If no listeners remain, streaming for that source is stopped.
func (s *Streamer) RemoveListener(sourceName string) {
	s.mu.Lock()
	delete(s.listeners, sourceName)
	hasListeners := len(s.listeners) > 0
	s.mu.Unlock()

	// Only stop if no listeners remain for any source
	// For now, we keep sources running once started to avoid frequent restarts
	_ = hasListeners
}
