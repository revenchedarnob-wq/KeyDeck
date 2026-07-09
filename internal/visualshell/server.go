package visualshell

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"keydeck.local/feasibilitylab/internal/presentation"
)

const (
	DefaultListenAddress = "127.0.0.1:0"
	DefaultMaxBodyBytes  = int64(64 << 10)
	DefaultSnapshotLimit = 100
	MaxSnapshotLimit     = 200
)

var (
	ErrInvalidConfig  = errors.New("invalid visual shell configuration")
	ErrAlreadyStarted = errors.New("visual shell already started")
)

//go:embed assets/*
var embeddedAssets embed.FS

type PresentationShell interface {
	Connect(context.Context) error
	Disconnect()
	Refresh(context.Context, uint64, int) (presentation.Snapshot, error)
	CreateTask(context.Context, string, presentation.TaskCreateRequest) (presentation.TaskCreateResult, error)
}

type Config struct {
	ListenAddress string
	Shell         PresentationShell
	Random        io.Reader
	MaxBodyBytes  int64
}

type Server struct {
	mu       sync.Mutex
	cfg      Config
	listener net.Listener
	http     *http.Server
	token    string
	host     string
	origin   string
	basePath string
	fatal    chan error
	closed   bool
}

type ViewStatus struct {
	Product        string `json:"product"`
	BuildID        string `json:"build_id"`
	APIVersion     string `json:"api_version"`
	TaskCount      int    `json:"task_count"`
	TimelineEvents int    `json:"timeline_events"`
	RequestRecords int    `json:"request_records"`
}

type TaskCard struct {
	TaskID          string  `json:"task_id"`
	SessionID       string  `json:"session_id"`
	Status          string  `json:"status"`
	LastSequence    uint64  `json:"last_sequence"`
	PassedChecks    int     `json:"passed_checks"`
	TotalChecks     int     `json:"total_checks"`
	ProgressPercent float64 `json:"progress_percent"`
	Complete        bool    `json:"complete"`
}

type TimelineItem struct {
	Sequence  uint64 `json:"sequence"`
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	Domain    string `json:"domain"`
	Kind      string `json:"kind"`
	Summary   string `json:"summary,omitempty"`
}

type ViewSnapshot struct {
	Connected  bool           `json:"connected"`
	BuildID    string         `json:"build_id"`
	APIVersion string         `json:"api_version"`
	Status     ViewStatus     `json:"status"`
	Tasks      []TaskCard     `json:"tasks"`
	Timeline   []TimelineItem `json:"timeline"`
	After      uint64         `json:"after"`
	NextAfter  uint64         `json:"next_after"`
}

type createTaskEnvelope struct {
	IdempotencyKey string                         `json:"idempotency_key"`
	Task           presentation.TaskCreateRequest `json:"task"`
}

func Open(cfg Config) (*Server, error) {
	if cfg.Shell == nil {
		return nil, ErrInvalidConfig
	}
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = DefaultListenAddress
	}
	if err := validateLoopbackListen(cfg.ListenAddress); err != nil {
		return nil, err
	}
	if cfg.Random == nil {
		cfg.Random = rand.Reader
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if cfg.MaxBodyBytes > 1<<20 {
		return nil, ErrInvalidConfig
	}
	return &Server{cfg: cfg, fatal: make(chan error, 1)}, nil
}

func (s *Server) Start(ctx context.Context) (string, error) {
	if s == nil {
		return "", ErrInvalidConfig
	}
	s.mu.Lock()
	if s.listener != nil || s.closed {
		s.mu.Unlock()
		return "", ErrAlreadyStarted
	}
	s.mu.Unlock()

	if err := s.cfg.Shell.Connect(ctx); err != nil {
		return "", err
	}
	token, err := randomToken(s.cfg.Random)
	if err != nil {
		s.cfg.Shell.Disconnect()
		return "", err
	}
	ln, err := net.Listen("tcp", s.cfg.ListenAddress)
	if err != nil {
		s.cfg.Shell.Disconnect()
		return "", err
	}
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok || tcp.IP == nil || !tcp.IP.IsLoopback() {
		_ = ln.Close()
		s.cfg.Shell.Disconnect()
		return "", ErrInvalidConfig
	}
	host := ln.Addr().String()
	origin := "http://" + host
	basePath := "/app/" + token + "/"
	httpServer := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	s.mu.Lock()
	s.listener = ln
	s.http = httpServer
	s.token = token
	s.host = host
	s.origin = origin
	s.basePath = basePath
	s.mu.Unlock()

	go func() {
		err := httpServer.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case s.fatal <- err:
			default:
			}
		}
	}()
	return origin + basePath, nil
}

func (s *Server) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.origin == "" || s.basePath == "" {
		return ""
	}
	return s.origin + s.basePath
}

func (s *Server) Fatal() <-chan error { return s.fatal }

func (s *Server) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	h := s.http
	s.mu.Unlock()
	var err error
	if h != nil {
		err = h.Shutdown(ctx)
	}
	s.cfg.Shell.Disconnect()
	return err
}

func (s *Server) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		host, origin, base := s.host, s.origin, s.basePath
		s.mu.Unlock()
		if host == "" || base == "" || r.Host != host || !strings.HasPrefix(r.URL.Path, base) {
			http.NotFound(w, r)
			return
		}
		setSecurityHeaders(w)
		rel := strings.TrimPrefix(r.URL.Path, base)
		switch rel {
		case "":
			if r.Method != http.MethodGet {
				methodNotAllowed(w)
				return
			}
			s.serveAsset(w, "assets/index.html", "text/html; charset=utf-8")
		case "styles.css":
			if r.Method != http.MethodGet {
				methodNotAllowed(w)
				return
			}
			s.serveAsset(w, "assets/styles.css", "text/css; charset=utf-8")
		case "app.js":
			if r.Method != http.MethodGet {
				methodNotAllowed(w)
				return
			}
			s.serveAsset(w, "assets/app.js", "text/javascript; charset=utf-8")
		case "api/snapshot":
			if r.Method != http.MethodGet {
				methodNotAllowed(w)
				return
			}
			if !validOptionalOrigin(r, origin) {
				writeError(w, http.StatusForbidden, "Cross-origin request rejected.")
				return
			}
			s.handleSnapshot(w, r)
		case "api/tasks":
			if r.Method != http.MethodPost {
				methodNotAllowed(w)
				return
			}
			if !validMutationOrigin(r, origin) {
				writeError(w, http.StatusForbidden, "Cross-origin request rejected.")
				return
			}
			s.handleCreateTask(w, r)
		case "api/reconnect":
			if r.Method != http.MethodPost {
				methodNotAllowed(w)
				return
			}
			if !validMutationOrigin(r, origin) {
				writeError(w, http.StatusForbidden, "Cross-origin request rejected.")
				return
			}
			s.handleReconnect(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func (s *Server) serveAsset(w http.ResponseWriter, name, contentType string) {
	b, err := embeddedAssets.ReadFile(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Renderer asset unavailable.")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	after, err := parseUintQuery(r.URL.Query().Get("after"), 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid snapshot cursor.")
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid snapshot limit.")
		return
	}
	snap, err := s.cfg.Shell.Refresh(r.Context(), after, limit)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Core connection unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, projectSnapshot(snap))
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "JSON request required.")
		return
	}
	var in createTaskEnvelope
	if err := decodeStrictBounded(r.Body, s.cfg.MaxBodyBytes, &in); err != nil {
		if errors.Is(err, errTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "Request too large.")
			return
		}
		writeError(w, http.StatusBadRequest, "Invalid task request.")
		return
	}
	if err := validateCreateEnvelope(in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid task request.")
		return
	}
	result, err := s.cfg.Shell.CreateTask(r.Context(), in.IdempotencyKey, in.Task)
	if err != nil {
		writeError(w, http.StatusConflict, "Task command rejected.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": result.State.TaskID, "status": string(result.State.Status), "reused": result.Reused, "reconciled": result.Reconciled})
}

func (s *Server) handleReconnect(w http.ResponseWriter, r *http.Request) {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "JSON request required.")
		return
	}
	var empty struct{}
	if err := decodeStrictBounded(r.Body, 1024, &empty); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid reconnect request.")
		return
	}
	if err := s.cfg.Shell.Connect(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "Core reconnection failed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"connected": true})
}

func projectSnapshot(in presentation.Snapshot) ViewSnapshot {
	out := ViewSnapshot{
		Connected:  in.Connected,
		BuildID:    in.Identity.BuildID,
		APIVersion: in.Identity.APIVersion,
		Status:     ViewStatus{Product: in.Status.Product, BuildID: in.Status.BuildID, APIVersion: in.Status.APIVersion, TaskCount: in.Status.TaskCount, TimelineEvents: in.Status.TimelineEvents, RequestRecords: in.Status.RequestRecords},
		After:      in.After,
		NextAfter:  in.NextAfter,
		Tasks:      make([]TaskCard, 0, len(in.Tasks)),
		Timeline:   make([]TimelineItem, 0, len(in.Timeline)),
	}
	for _, t := range in.Tasks {
		out.Tasks = append(out.Tasks, TaskCard{TaskID: t.TaskID, SessionID: t.SessionID, Status: string(t.Status), LastSequence: t.LastSequence, PassedChecks: t.Progress.PassedChecks, TotalChecks: t.Progress.TotalChecks, ProgressPercent: t.Progress.Percent, Complete: t.Progress.Complete})
	}
	for _, e := range in.Timeline {
		out.Timeline = append(out.Timeline, TimelineItem{Sequence: e.Sequence, TaskID: e.TaskID, SessionID: e.SessionID, Domain: string(e.Domain), Kind: e.Kind, Summary: e.Summary})
	}
	return out
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; object-src 'none'")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}

func validOptionalOrigin(r *http.Request, origin string) bool {
	o := r.Header.Get("Origin")
	return o == "" || o == origin
}

func validMutationOrigin(r *http.Request, origin string) bool {
	return r.Header.Get("Origin") == origin
}

func validateLoopbackListen(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return ErrInvalidConfig
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return ErrInvalidConfig
	}
	return nil
}

func randomToken(r io.Reader) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func parseUintQuery(raw string, fallback uint64) (uint64, error) {
	if raw == "" {
		return fallback, nil
	}
	return strconv.ParseUint(raw, 10, 64)
}

func parseLimit(raw string) (int, error) {
	if raw == "" {
		return DefaultSnapshotLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > MaxSnapshotLimit {
		return 0, ErrInvalidConfig
	}
	return n, nil
}

func isJSONContentType(raw string) bool {
	media, _, err := mime.ParseMediaType(raw)
	return err == nil && media == "application/json"
}

var errTooLarge = errors.New("request too large")

func decodeStrictBounded(body io.ReadCloser, max int64, dst any) error {
	defer body.Close()
	limited := io.LimitReader(body, max+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if int64(len(b)) > max {
		return errTooLarge
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func validateCreateEnvelope(in createTaskEnvelope) error {
	if strings.TrimSpace(in.IdempotencyKey) == "" || len(in.IdempotencyKey) > 256 {
		return ErrInvalidConfig
	}
	if strings.TrimSpace(in.Task.TaskID) == "" || len(in.Task.TaskID) > 128 {
		return ErrInvalidConfig
	}
	if strings.TrimSpace(in.Task.SessionID) == "" || len(in.Task.SessionID) > 128 {
		return ErrInvalidConfig
	}
	if strings.TrimSpace(in.Task.Contract.Goal) == "" || len(in.Task.Contract.Goal) > 4096 {
		return ErrInvalidConfig
	}
	if len(in.Task.Contract.Checks) == 0 || len(in.Task.Contract.Checks) > 64 {
		return ErrInvalidConfig
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", http.MethodGet)
	writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
}

// ResolveURL validates that a renderer URL points to this exact in-memory session.
func (s *Server) ResolveURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	s.mu.Lock()
	expected := s.origin + s.basePath
	s.mu.Unlock()
	if u.String() != expected {
		return fmt.Errorf("renderer URL mismatch")
	}
	return nil
}
