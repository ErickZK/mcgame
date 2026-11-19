// HTTP/WebSocket gateway server for browser, PC, and mobile clients.
// Exposes Argon2id login/registration issuing single-use Redis WS tickets,
// validates Origin headers against CSWSH, enforces IP/global connection caps,
// per-connection token-bucket rate limits, strike auto-kick, and routes RPCs
// into the shared rtmp.RPCDispatcher and rtmp.Server session registry.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	appauth "mcgame-server/internal/application/auth"
	"mcgame-server/internal/infrastructure/config"
	"mcgame-server/internal/infrastructure/rtmp"
	"mcgame-server/internal/infrastructure/security"
	pkgerrors "mcgame-server/pkg/errors"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	maxAuthBodyBytes  = 4 * 1024
	authFrameTimeout  = 10 * time.Second
	pingInterval      = 25 * time.Second
	readIdleTimeout   = 60 * time.Second
	globalRPCRate     = 25.0
	globalRPCBurst    = 40
	strikeKickLimit   = 10
	minPasswordLength = 8
	maxPasswordLength = 128
)

var blockedWSMethods = map[string]bool{
	"onConnectAuth": true,
}

type Server struct {
	cfg            config.WSConfig
	logger         *zap.Logger
	authService    *appauth.Service
	rtmpServer     *rtmp.Server
	dispatcher     *rtmp.RPCDispatcher
	tickets        *security.TicketStore
	loginLimiter   *security.LoginLimiter
	connTracker    *security.IPConnTracker
	allowedOrigins map[string]bool
	httpServer     *http.Server
}

func NewServer(
	cfg config.WSConfig,
	logger *zap.Logger,
	authService *appauth.Service,
	rtmpServer *rtmp.Server,
	dispatcher *rtmp.RPCDispatcher,
	rdb redis.Cmdable,
) *Server {
	origins := make(map[string]bool)
	for _, raw := range cfg.AllowedOrigins {
		trimmed := strings.TrimRight(strings.ToLower(strings.TrimSpace(raw)), "/")
		if trimmed != "" {
			origins[trimmed] = true
		}
	}

	s := &Server{
		cfg:            cfg,
		logger:         logger,
		authService:    authService,
		rtmpServer:     rtmpServer,
		dispatcher:     dispatcher,
		tickets:        security.NewTicketStore(rdb, 30*time.Second),
		loginLimiter:   security.NewLoginLimiter(),
		connTracker:    security.NewIPConnTracker(cfg.MaxConnections, cfg.MaxConnsPerIP),
		allowedOrigins: origins,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/auth/login", s.handleLogin)
	mux.HandleFunc("/api/auth/register", s.handleRegister)
	mux.HandleFunc("/ws", s.handleWS)

	s.httpServer = &http.Server{
		Addr:              cfg.Address(),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s
}

func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

func (s *Server) Start() error {
	s.logger.Info("WebSocket gateway started",
		zap.String("address", s.cfg.Address()),
		zap.Int("max_connections", s.cfg.MaxConnections),
		zap.Int("max_conns_per_ip", s.cfg.MaxConnsPerIP),
	)
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":            "ok",
		"activeConnections": s.connTracker.Total(),
	})
}

type authPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		s.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	clientIP := s.extractClientIP(r)
	if allowed, retryAfter := s.loginLimiter.Check("ip:" + clientIP); !allowed {
		s.writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error":          "too many login attempts",
			"retryAfterSecs": int(retryAfter.Seconds()) + 1,
		})
		return
	}

	var req authPayload
	if !s.decodeLimitedJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !security.ValidateUsername(req.Username) || req.Password == "" || len(req.Password) > maxPasswordLength {
		s.loginLimiter.RecordFailure("ip:" + clientIP)
		s.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	acctKey := "acct:" + clientIP + ":" + strings.ToLower(req.Username)
	if allowed, retryAfter := s.loginLimiter.Check(acctKey); !allowed {
		s.writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error":          "account temporarily locked due to failed attempts",
			"retryAfterSecs": int(retryAfter.Seconds()) + 1,
		})
		return
	}

	resp, err := s.authService.LoginPlaintext(r.Context(), &appauth.LoginRequest{
		Username:  req.Username,
		Password:  req.Password,
		IPAddress: clientIP,
	})
	if err != nil {
		s.loginLimiter.RecordFailure("ip:" + clientIP)
		s.loginLimiter.RecordFailure(acctKey)
		if pkgerrors.Is(err, pkgerrors.ErrAccountBanned) {
			s.writeJSON(w, http.StatusForbidden, map[string]string{"error": "account banned"})
			return
		}
		s.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	s.loginLimiter.RecordSuccess("ip:" + clientIP)
	s.loginLimiter.RecordSuccess(acctKey)

	token, expiresAt, err := s.tickets.Issue(r.Context(), security.WSTicket{
		SessionID: resp.SessionID,
		AccountID: resp.AccountID,
		Username:  resp.Username,
		IPAddress: clientIP,
	})
	if err != nil {
		s.logger.Error("Failed to issue WS ticket", zap.Error(err))
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session ticket"})
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"ticket":    token,
		"expiresAt": expiresAt.UTC().Format(time.RFC3339),
		"accountId": resp.AccountID,
		"username":  resp.Username,
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if s.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		s.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	clientIP := s.extractClientIP(r)
	regKey := "reg:" + clientIP
	if allowed, retryAfter := s.loginLimiter.Check(regKey); !allowed {
		s.writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error":          "too many registration attempts",
			"retryAfterSecs": int(retryAfter.Seconds()) + 1,
		})
		return
	}

	var req authPayload
	if !s.decodeLimitedJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)

	if !security.ValidateUsername(req.Username) {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username must be 3-32 alphanumeric characters, _ or -"})
		return
	}
	if len(req.Password) < minPasswordLength || len(req.Password) > maxPasswordLength {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be 8-128 characters"})
		return
	}

	err := s.authService.Register(r.Context(), &appauth.RegisterRequest{
		Username: req.Username,
		Password: req.Password,
		Email:    req.Email,
	})
	if err != nil {
		s.loginLimiter.RecordFailure(regKey)
		if pkgerrors.Is(err, pkgerrors.ErrAlreadyExists) {
			s.writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
			return
		}
		s.logger.Error("Account registration failed", zap.Error(err))
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "registration failed"})
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"status":   "created",
		"username": req.Username,
	})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.isOriginAllowed(r.Header.Get("Origin")) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	clientIP := s.extractClientIP(r)
	if ok, reason := s.connTracker.Acquire(clientIP); !ok {
		http.Error(w, reason, http.StatusTooManyRequests)
		return
	}

	fc, err := Upgrade(w, r, int64(s.cfg.MaxMessageBytes))
	if err != nil {
		s.connTracker.Release(clientIP)
		return
	}

	go s.serveWSConn(fc, clientIP)
}

func (s *Server) serveWSConn(fc *FrameConn, clientIP string) {
	defer func() {
		_ = fc.Close()
		s.connTracker.Release(clientIP)
	}()

	_ = fc.SetReadDeadline(time.Now().Add(authFrameTimeout))
	op, raw, err := fc.ReadMessage()
	if err != nil || op != opText {
		return
	}

	var authEnv IncomingEnvelope
	if err := json.Unmarshal(raw, &authEnv); err != nil || authEnv.Op != "auth" || authEnv.Ticket == "" {
		_ = NewWSTransport(fc).WriteError(context.Background(), authEnv.Tx, errors.New("authentication required"))
		return
	}

	ticket, err := s.tickets.Consume(context.Background(), authEnv.Ticket, clientIP)
	if err != nil {
		_ = NewWSTransport(fc).WriteError(context.Background(), authEnv.Tx, errors.New("invalid or expired ticket"))
		return
	}

	if s.rtmpServer != nil {
		s.rtmpServer.ForceDisconnectAccount(ticket.AccountID, 0)
	}

	conn := s.rtmpServer.AllocateConnection()
	transport := NewWSTransport(fc)
	conn.SetTransport(transport)
	conn.SetRemoteIP(clientIP)
	appName := strings.TrimSpace(authEnv.App)
	if appName == "" {
		appName = "master/test"
	}
	conn.SetAppName(appName)
	conn.SetSession(ticket.SessionID, ticket.AccountID, ticket.Username)
	defer func() {
		conn.Close()
		s.rtmpServer.RemoveConnection(conn.ID)
	}()

	_ = transport.WriteResult(context.Background(), authEnv.Tx, map[string]interface{}{
		"authenticated": true,
		"accountId":     ticket.AccountID,
		"username":      ticket.Username,
		"connId":        conn.ID,
	})

	stopPing := make(chan struct{})
	defer close(stopPing)
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := fc.WritePing(); err != nil {
					_ = fc.Close()
					return
				}
			case <-stopPing:
				return
			}
		}
	}()

	bucket := security.NewTokenBucket(globalRPCRate, globalRPCBurst)
	strikes := security.NewStrikeTracker(strikeKickLimit, 2*time.Minute)

	for {
		_ = fc.SetReadDeadline(time.Now().Add(readIdleTimeout))
		op, payload, err := fc.ReadMessage()
		if err != nil {
			return
		}
		if op != opText {
			continue
		}
		conn.UpdateLastActive()

		if !bucket.Allow() {
			if _, kick := strikes.Add(2); kick {
				s.logger.Warn("Kicking WS client for RPC flooding",
					zap.Uint32("conn_id", conn.ID),
					zap.String("ip", clientIP))
				_ = transport.WriteError(context.Background(), 0, pkgerrors.ErrRateLimited)
				return
			}
			continue
		}

		var env IncomingEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			if _, kick := strikes.Add(3); kick {
				return
			}
			_ = transport.WriteError(context.Background(), 0, pkgerrors.ErrInvalidInput)
			continue
		}

		if env.Op == "ping" {
			_ = transport.WriteResult(context.Background(), env.Tx, map[string]int64{"ts": time.Now().UnixMilli()})
			continue
		}

		if env.Op != "rpc" || env.Method == "" {
			if _, kick := strikes.Add(2); kick {
				return
			}
			_ = transport.WriteError(context.Background(), env.Tx, pkgerrors.ErrInvalidInput)
			continue
		}

		if blockedWSMethods[env.Method] || !s.dispatcher.HasHandler(env.Method) {
			if _, kick := strikes.Add(2); kick {
				return
			}
			_ = transport.WriteError(context.Background(), env.Tx, pkgerrors.ErrMethodNotFound)
			continue
		}

		res, dispErr := s.dispatcher.Dispatch(conn, env.Method, env.Args)
		if dispErr != nil {
			if pkgerrors.Is(dispErr, pkgerrors.ErrRateLimited) {
				if _, kick := strikes.Add(1); kick {
					return
				}
			}
			_ = transport.WriteError(context.Background(), env.Tx, dispErr)
			continue
		}
		if env.Tx > 0 || res != nil {
			_ = transport.WriteResult(context.Background(), env.Tx, res)
		}
	}
}

func (s *Server) isOriginAllowed(origin string) bool {
	if len(s.allowedOrigins) == 0 {
		return true
	}
	if origin == "" {
		return false
	}
	normalized := strings.TrimRight(strings.ToLower(strings.TrimSpace(origin)), "/")
	return s.allowedOrigins[normalized] || s.allowedOrigins["*"]
}

func (s *Server) applyCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin != "" && s.isOriginAllowed(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodOptions {
		if origin != "" && !s.isOriginAllowed(origin) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return true
		}
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func (s *Server) decodeLimitedJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	defer r.Body.Close()
	limited := io.LimitReader(r.Body, maxAuthBodyBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil || len(raw) > maxAuthBodyBytes {
		s.writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	return true
}

func (s *Server) extractClientIP(r *http.Request) string {
	if s.cfg.TrustProxyHeaders {
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			return xri
		}
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
				return strings.TrimSpace(parts[0])
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
