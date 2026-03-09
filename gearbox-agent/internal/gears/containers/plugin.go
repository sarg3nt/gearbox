// Package containers provides Docker/container runtime monitoring as a gear.
package containers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	dockerclient "github.com/moby/moby/client"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// Gear implements container runtime monitoring.
type Gear struct {
	gear.BaseGear
	mu          sync.RWMutex
	runtimeInfo RuntimeInfo
	dockerCli   *dockerclient.Client
	runtime     *dockerRuntime
	eventBus    *events.Bus
}

// Info returns gear metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "containers",
		DisplayName: "Containers",
		Description: "Monitor and manage Docker containers and Compose stacks",
		Version:     "1.0.0",
		Category:    "infrastructure",
		Core:        false,
	}
}

// Initialize sets up the gear and detects the container runtime.
func (p *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := p.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}

	p.eventBus = deps.EventBus

	// Detect runtime (non-fatal: gear still registers even if Docker is absent)
	info, cli := detectRuntime(ctx)
	p.runtimeInfo = info

	if cli != nil {
		p.dockerCli = cli
		p.runtime = &dockerRuntime{cli: cli}
		p.Logger().Info("containers gear: Docker detected", "version", info.Version, "api", info.APIVersion)
	} else {
		p.Logger().Info("containers gear: no container runtime detected, endpoints will return empty results")
	}

	return nil
}

// Stop cleans up the Docker client.
func (p *Gear) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dockerCli != nil {
		_ = p.dockerCli.Close()
		p.dockerCli = nil
		p.runtime = nil
	}
	return nil
}

// Health returns the current health status.
func (p *Gear) Health() gear.HealthStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.runtimeInfo.Available {
		return gear.NewDegradedStatus("no container runtime detected")
	}
	return gear.NewHealthyStatus("Docker runtime available")
}

// EventTypes returns the events this gear publishes.
func (p *Gear) EventTypes() []gear.EventType {
	return []gear.EventType{
		{
			Name:        "containers.updated",
			Description: "Published when the container list is refreshed",
			Payload:     "ContainerList",
		},
	}
}

// RegisterRoutes registers all HTTP API endpoints.
func (p *Gear) RegisterRoutes(r chi.Router) {
	// Runtime info
	r.Get("/api/v1/containers/runtime", p.handleRuntime)

	// Container listing and management
	r.Get("/api/v1/containers/list", p.handleList)
	r.Get("/api/v1/containers/{id}", p.handleInspect)
	r.Get("/api/v1/containers/{id}/stats", p.handleStats)
	r.Get("/api/v1/containers/{id}/logs", p.handleLogs)
	r.Post("/api/v1/containers/{id}/start", p.handleStart)
	r.Post("/api/v1/containers/{id}/stop", p.handleStop)
	r.Post("/api/v1/containers/{id}/restart", p.handleRestart)
	r.Delete("/api/v1/containers/{id}", p.handleRemove)

	// Stack operations
	r.Get("/api/v1/containers/stacks", p.handleListStacks)
	r.Get("/api/v1/containers/stacks/{name}", p.handleStackDetail)

	// Images
	r.Get("/api/v1/containers/images", p.handleListImages)
}

// jsonError writes a JSON error response. All error responses from this gear
// MUST use this helper instead of http.Error() to ensure:
//  1. Consistent JSON format that the dashboard client can parse
//  2. No "Failed to" prefixes (the JS client adds its own user-friendly prefix)
func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// jsonOK writes a JSON success response.
func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// handleRuntime returns container runtime information.
func (p *Gear) handleRuntime(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	info := p.runtimeInfo
	p.mu.RUnlock()
	jsonOK(w, info)
}

// handleList returns all containers.
func (p *Gear) handleList(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	runtime := p.runtime
	p.mu.RUnlock()

	if runtime == nil {
		jsonOK(w, ContainerList{Containers: []Container{}, CollectedAt: time.Now().UTC()})
		return
	}

	containers, err := runtime.listContainers(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	running := 0
	for _, c := range containers {
		if c.State == "running" {
			running++
		}
	}

	jsonOK(w, ContainerList{
		Containers:  containers,
		Total:       len(containers),
		Running:     running,
		Stopped:     len(containers) - running,
		CollectedAt: time.Now().UTC(),
	})
}

// handleInspect returns detailed information about a container.
func (p *Gear) handleInspect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		jsonError(w, "container id is required", http.StatusBadRequest)
		return
	}

	p.mu.RLock()
	cli := p.dockerCli
	p.mu.RUnlock()

	if cli == nil {
		jsonError(w, "no container runtime available", http.StatusServiceUnavailable)
		return
	}

	result, err := cli.ContainerInspect(r.Context(), id, dockerclient.ContainerInspectOptions{})
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, result.Container)
}

// handleStats returns resource usage statistics for a container.
func (p *Gear) handleStats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		jsonError(w, "container id is required", http.StatusBadRequest)
		return
	}

	p.mu.RLock()
	runtime := p.runtime
	p.mu.RUnlock()

	if runtime == nil {
		jsonError(w, "no container runtime available", http.StatusServiceUnavailable)
		return
	}

	stats, err := runtime.containerStats(r.Context(), id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, stats)
}

// handleLogs returns logs for a container.
func (p *Gear) handleLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		jsonError(w, "container id is required", http.StatusBadRequest)
		return
	}

	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "100"
	}
	// Validate tail is a number or "all"
	if tail != "all" {
		if _, err := strconv.Atoi(tail); err != nil {
			jsonError(w, "tail must be a number or 'all'", http.StatusBadRequest)
			return
		}
	}

	p.mu.RLock()
	cli := p.dockerCli
	p.mu.RUnlock()

	if cli == nil {
		jsonError(w, "no container runtime available", http.StatusServiceUnavailable)
		return
	}

	timestamps := r.URL.Query().Get("timestamps") == "true"
	since := r.URL.Query().Get("since")

	result, err := cli.ContainerLogs(r.Context(), id, dockerclient.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: timestamps,
		Tail:       tail,
		Since:      since,
	})
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = result.Close() }()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.Copy(w, result)
}

// handleStart starts a container.
func (p *Gear) handleStart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		jsonError(w, "container id is required", http.StatusBadRequest)
		return
	}

	p.mu.RLock()
	cli := p.dockerCli
	p.mu.RUnlock()

	if cli == nil {
		jsonError(w, "no container runtime available", http.StatusServiceUnavailable)
		return
	}

	if _, err := cli.ContainerStart(r.Context(), id, dockerclient.ContainerStartOptions{}); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "started"})
}

// handleStop stops a container.
func (p *Gear) handleStop(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		jsonError(w, "container id is required", http.StatusBadRequest)
		return
	}

	p.mu.RLock()
	cli := p.dockerCli
	p.mu.RUnlock()

	if cli == nil {
		jsonError(w, "no container runtime available", http.StatusServiceUnavailable)
		return
	}

	if _, err := cli.ContainerStop(r.Context(), id, dockerclient.ContainerStopOptions{}); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "stopped"})
}

// handleRestart restarts a container.
func (p *Gear) handleRestart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		jsonError(w, "container id is required", http.StatusBadRequest)
		return
	}

	p.mu.RLock()
	cli := p.dockerCli
	p.mu.RUnlock()

	if cli == nil {
		jsonError(w, "no container runtime available", http.StatusServiceUnavailable)
		return
	}

	if _, err := cli.ContainerRestart(r.Context(), id, dockerclient.ContainerRestartOptions{}); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "restarted"})
}

// handleRemove removes a container.
func (p *Gear) handleRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		jsonError(w, "container id is required", http.StatusBadRequest)
		return
	}

	force := r.URL.Query().Get("force") == "true"

	p.mu.RLock()
	cli := p.dockerCli
	p.mu.RUnlock()

	if cli == nil {
		jsonError(w, "no container runtime available", http.StatusServiceUnavailable)
		return
	}

	if _, err := cli.ContainerRemove(r.Context(), id, dockerclient.ContainerRemoveOptions{Force: force}); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "removed"})
}

// handleListStacks returns all Compose stacks.
func (p *Gear) handleListStacks(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	runtime := p.runtime
	p.mu.RUnlock()

	if runtime == nil {
		jsonOK(w, StackList{Stacks: []Stack{}, CollectedAt: time.Now().UTC()})
		return
	}

	containers, err := runtime.listContainers(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stacks := listStacks(containers)

	jsonOK(w, StackList{
		Stacks:      stacks,
		Total:       len(stacks),
		CollectedAt: time.Now().UTC(),
	})
}

// handleStackDetail returns a specific stack with its containers.
func (p *Gear) handleStackDetail(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		jsonError(w, "stack name is required", http.StatusBadRequest)
		return
	}

	p.mu.RLock()
	runtime := p.runtime
	p.mu.RUnlock()

	if runtime == nil {
		jsonError(w, "no container runtime available", http.StatusServiceUnavailable)
		return
	}

	containers, err := runtime.listContainers(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stacks := listStacks(containers)
	for _, s := range stacks {
		if s.Name == name {
			jsonOK(w, s)
			return
		}
	}

	jsonError(w, "stack not found", http.StatusNotFound)
}

// handleListImages returns all local Docker images.
func (p *Gear) handleListImages(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	cli := p.dockerCli
	p.mu.RUnlock()

	if cli == nil {
		jsonOK(w, []Image{})
		return
	}

	result, err := cli.ImageList(r.Context(), dockerclient.ImageListOptions{All: false})
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	images := make([]Image, 0, len(result.Items))
	for _, img := range result.Items {
		tags := img.RepoTags
		if len(tags) == 0 {
			tags = []string{"<none>:<none>"}
		}
		images = append(images, Image{
			ID:      img.ID,
			ShortID: shortID(img.ID),
			Tags:    tags,
			Size:    img.Size,
		})
	}

	jsonOK(w, images)
}

