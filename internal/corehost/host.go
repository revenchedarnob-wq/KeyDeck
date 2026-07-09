package corehost

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Host struct {
	cfg        Config
	layout     Layout
	credential Credential
	identity   Identity
	backend    Backend
	lease      *Lease
	listener   net.Listener
	server     *http.Server
	runtime    RuntimeInfo

	mu         sync.Mutex
	stateMu    sync.Mutex
	closed     bool
	hbStop     chan struct{}
	hbDone     chan struct{}
	fatal      chan error
	fatalOnce  sync.Once
	hbStopOnce sync.Once
	hbStarted  bool
}

func Open(cfg Config) (*Host, error) {
	normalized, layout, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	instanceID, err := randomHex(normalized.Random, 16)
	if err != nil {
		return nil, err
	}
	lease, err := AcquireLease(layout.LeaseDir, instanceID, os.Getpid(), normalized.Now, normalized.StaleLeaseAfter)
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = lease.Release()
		}
	}()

	credential, _, err := LoadOrCreateCredential(layout.CredentialPath, normalized.Random)
	if err != nil {
		return nil, err
	}
	backend := normalized.Backend
	if backend == nil {
		backend, err = OpenFileBackend(layout, normalized.BuildID, normalized.APIVersion)
		if err != nil {
			return nil, err
		}
	}
	h := &Host{
		cfg: normalized, layout: layout, credential: credential, backend: backend, lease: lease,
		identity: Identity{Product: "KeyDeck", BuildID: normalized.BuildID, APIVersion: normalized.APIVersion, InstallID: credential.InstallID, InstanceID: instanceID},
		hbStop:   make(chan struct{}), hbDone: make(chan struct{}), fatal: make(chan error, 1),
	}
	cleanup = false
	return h, nil
}

func (h *Host) Start() (RuntimeInfo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.listener != nil {
		return RuntimeInfo{}, errors.New("core host is closed or already started")
	}
	ln, err := net.Listen("tcp", h.cfg.ListenAddress)
	if err != nil {
		return RuntimeInfo{}, err
	}
	actual := ln.Addr().String()
	h.runtime = RuntimeInfo{Version: 1, InstanceID: h.identity.InstanceID, InstallID: h.identity.InstallID, Address: actual, BuildID: h.identity.BuildID, APIVersion: h.identity.APIVersion, PID: os.Getpid(), SupervisorInstanceID: h.cfg.SupervisorInstanceID}
	if err := atomicWriteJSON(h.layout.RuntimePath, h.runtime, 0o600); err != nil {
		_ = ln.Close()
		return RuntimeInfo{}, err
	}
	h.listener = ln
	h.server = &http.Server{
		Handler:           h.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	go func() {
		err := h.server.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.reportFatal(fmt.Errorf("core HTTP server failed: %w", err))
			h.stopHeartbeat()
		}
	}()
	h.hbStarted = true
	go h.heartbeatLoop()
	return h.runtime, nil
}

func (h *Host) Close(ctx context.Context) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	server := h.server
	instanceID := h.identity.InstanceID
	hbStarted := h.hbStarted
	if hbStarted {
		h.stopHeartbeat()
	}
	h.mu.Unlock()

	if hbStarted {
		<-h.hbDone
	}
	var errs []error
	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if info, err := ReadRuntime(h.layout.RuntimePath); err == nil && info.InstanceID == instanceID {
		if err := os.Remove(h.layout.RuntimePath); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	if err := h.lease.Release(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (h *Host) Runtime() RuntimeInfo   { return h.runtime }
func (h *Host) Layout() Layout         { return h.layout }
func (h *Host) Credential() Credential { return h.credential }
func (h *Host) Identity() Identity     { return h.identity }
func (h *Host) Fatal() <-chan error    { return h.fatal }

func (h *Host) heartbeatLoop() {
	defer close(h.hbDone)
	ticker := time.NewTicker(h.cfg.HeartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-h.hbStop:
			return
		case <-ticker.C:
			if err := h.lease.Refresh(); err != nil {
				h.reportFatal(fmt.Errorf("core lease refresh failed: %w", err))
				h.stopHeartbeat()
				h.mu.Lock()
				server := h.server
				h.mu.Unlock()
				if server != nil {
					_ = server.Close()
				}
				return
			}
		}
	}
}

func (h *Host) reportFatal(err error) {
	if err == nil {
		return
	}
	h.fatalOnce.Do(func() { h.fatal <- err })
}

func (h *Host) stopHeartbeat() {
	h.hbStopOnce.Do(func() { close(h.hbStop) })
}

func (h *Host) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.Handle("/v1/", h.authenticated(http.HandlerFunc(h.handleV1)))
	return mux
}

func (h *Host) authenticated(next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(h.credential.Token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		got := sha256.Sum256([]byte(strings.TrimSpace(strings.TrimPrefix(auth, prefix))))
		if subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Host) handleV1(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/identity":
		writeJSON(w, http.StatusOK, h.identity)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/snapshot":
		h.handleSnapshot(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/status":
		h.stateMu.Lock()
		status, err := h.backend.Status()
		h.stateMu.Unlock()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state unavailable")
			return
		}
		writeJSON(w, http.StatusOK, status)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks":
		h.handleCreateTask(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks":
		h.stateMu.Lock()
		items, err := h.backend.ListTasks()
		h.stateMu.Unlock()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tasks": items})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
		h.stateMu.Lock()
		state, err := h.backend.GetTask(id)
		h.stateMu.Unlock()
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid task id")
			return
		}
		writeJSON(w, http.StatusOK, state)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/timeline":
		after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		h.stateMu.Lock()
		events, err := h.backend.Timeline(after, limit)
		h.stateMu.Unlock()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Host) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	h.stateMu.Lock()
	status, statusErr := h.backend.Status()
	items, tasksErr := h.backend.ListTasks()
	events, timelineErr := h.backend.Timeline(after, limit)
	h.stateMu.Unlock()
	if statusErr != nil || tasksErr != nil || timelineErr != nil {
		writeError(w, http.StatusInternalServerError, "state unavailable")
		return
	}
	next := after
	if len(events) > 0 {
		next = events[len(events)-1].Sequence
	}
	writeJSON(w, http.StatusOK, ProjectionSnapshot{Identity: h.identity, Status: status, Tasks: items, Timeline: events, After: after, NextAfter: next})
}

func (h *Host) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "application/json required")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !safeID.MatchString(key) {
		writeError(w, http.StatusBadRequest, "valid Idempotency-Key required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req TaskCreateRequest
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := ensureJSONEOF(dec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	h.stateMu.Lock()
	result, err := h.backend.CreateTask(req, key)
	h.stateMu.Unlock()
	switch {
	case errors.Is(err, ErrIdempotencyConflict), errors.Is(err, ErrTaskConflict):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusBadRequest, "invalid task request")
	default:
		writeJSON(w, http.StatusCreated, result)
	}
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func normalizeConfig(cfg Config) (Config, Layout, error) {
	layout, err := BuildLayout(cfg.DataDir)
	if err != nil {
		return Config{}, Layout{}, err
	}
	cfg.BuildID = strings.TrimSpace(cfg.BuildID)
	cfg.SupervisorInstanceID = strings.TrimSpace(cfg.SupervisorInstanceID)
	if cfg.SupervisorInstanceID != "" && !safeID.MatchString(cfg.SupervisorInstanceID) {
		return Config{}, Layout{}, fmt.Errorf("%w: invalid supervisor instance id", ErrInvalidConfig)
	}
	if cfg.BuildID == "" {
		return Config{}, Layout{}, fmt.Errorf("%w: build id required", ErrInvalidConfig)
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = DefaultAPIVersion
	}
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(cfg.ListenAddress)
	if err != nil {
		return Config{}, Layout{}, fmt.Errorf("%w: invalid listen address", ErrInvalidConfig)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return Config{}, Layout{}, fmt.Errorf("%w: listen address must be explicit loopback IP", ErrInvalidConfig)
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if cfg.MaxBodyBytes > 1<<20 {
		return Config{}, Layout{}, fmt.Errorf("%w: max body too large", ErrInvalidConfig)
	}
	if cfg.StaleLeaseAfter <= 0 {
		cfg.StaleLeaseAfter = 10 * time.Second
	}
	if cfg.HeartbeatEvery <= 0 {
		cfg.HeartbeatEvery = 2 * time.Second
	}
	if cfg.HeartbeatEvery >= cfg.StaleLeaseAfter {
		return Config{}, Layout{}, fmt.Errorf("%w: heartbeat must be shorter than stale lease", ErrInvalidConfig)
	}
	if cfg.Random == nil {
		cfg.Random = rand.Reader
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return cfg, layout, nil
}
