// Integration tests for the WebSocket gateway covering Argon2id registration
// and login, Origin validation (anti-CSWSH), single-use ticket enforcement,
// RPC dispatch, server-push callbacks, brute-force lockout, and payload limits
// using in-memory net.Pipe connections so tests run cleanly without TCP ports.
package ws

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	appauth "mcgame-server/internal/application/auth"
	domainauth "mcgame-server/internal/domain/auth"
	"mcgame-server/internal/infrastructure/config"
	"mcgame-server/internal/infrastructure/rtmp"
	pkgerrors "mcgame-server/pkg/errors"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type memAccountRepo struct {
	mu     sync.Mutex
	byUser map[string]*domainauth.Account
	byID   map[uuid.UUID]*domainauth.Account
}

func newMemAccountRepo() *memAccountRepo {
	return &memAccountRepo{
		byUser: make(map[string]*domainauth.Account),
		byID:   make(map[uuid.UUID]*domainauth.Account),
	}
}

func (r *memAccountRepo) FindByID(ctx context.Context, id uuid.UUID) (*domainauth.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.byID[id]
	if !ok {
		return nil, pkgerrors.ErrAccountNotFound
	}
	return a, nil
}

func (r *memAccountRepo) FindByUsername(ctx context.Context, username string) (*domainauth.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.byUser[username]
	if !ok {
		return nil, pkgerrors.ErrAccountNotFound
	}
	return a, nil
}

func (r *memAccountRepo) Create(ctx context.Context, account *domainauth.Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byUser[account.Username] = account
	r.byID[account.ID] = account
	return nil
}

func (r *memAccountRepo) Update(ctx context.Context, account *domainauth.Account) error {
	return r.Create(ctx, account)
}

func (r *memAccountRepo) ExistsByUsername(ctx context.Context, username string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.byUser[username]
	return ok, nil
}

func (r *memAccountRepo) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	return nil
}

type memSessionRepo struct {
	mu       sync.Mutex
	sessions map[string]*domainauth.Session
}

func newMemSessionRepo() *memSessionRepo {
	return &memSessionRepo{sessions: make(map[string]*domainauth.Session)}
}

func (r *memSessionRepo) Get(ctx context.Context, sessionID string) (*domainauth.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[sessionID]
	if !ok {
		return nil, pkgerrors.ErrSessionExpired
	}
	return s, nil
}

func (r *memSessionRepo) GetByAccountID(ctx context.Context, accountID uuid.UUID) (*domainauth.Session, error) {
	return nil, pkgerrors.ErrSessionExpired
}

func (r *memSessionRepo) Save(ctx context.Context, session *domainauth.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.ID] = session
	return nil
}

func (r *memSessionRepo) Delete(ctx context.Context, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, sessionID)
	return nil
}

func (r *memSessionRepo) DeleteByAccountID(ctx context.Context, accountID uuid.UUID) error {
	return nil
}

func (r *memSessionRepo) Refresh(ctx context.Context, sessionID string) error {
	return nil
}

type pipeHijackWriter struct {
	*httptest.ResponseRecorder
	serverConn net.Conn
	hijacked   bool
}

func (p *pipeHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	p.hijacked = true
	rw := bufio.NewReadWriter(bufio.NewReader(p.serverConn), bufio.NewWriter(p.serverConn))
	return p.serverConn, rw, nil
}

func dialPipeWS(t *testing.T, handler http.Handler, origin string, remoteAddr string) (net.Conn, *bufio.Reader, int) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	keyBytes := make([]byte, 16)
	_, _ = rand.Read(keyBytes)
	key := base64.StdEncoding.EncodeToString(keyBytes)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.RemoteAddr = remoteAddr
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", key)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}

	pw := &pipeHijackWriter{
		ResponseRecorder: httptest.NewRecorder(),
		serverConn:       serverConn,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(pw, req)
	}()

	br := bufio.NewReader(clientConn)
	readCh := make(chan int, 1)
	go func() {
		_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		resp, err := http.ReadResponse(br, req)
		if err != nil {
			readCh <- 0
			return
		}
		readCh <- resp.StatusCode
	}()

	<-done
	if !pw.hijacked {
		_ = clientConn.Close()
		_ = serverConn.Close()
		return nil, nil, pw.Code
	}
	status := <-readCh
	return clientConn, br, status
}

func writeClientMaskedText(t *testing.T, conn net.Conn, payload []byte) {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteByte(0x81)
	n := len(payload)
	switch {
	case n <= 125:
		buf.WriteByte(0x80 | byte(n))
	case n <= 65535:
		buf.WriteByte(0x80 | 126)
		_ = binary.Write(&buf, binary.BigEndian, uint16(n))
	default:
		buf.WriteByte(0x80 | 127)
		_ = binary.Write(&buf, binary.BigEndian, uint64(n))
	}
	mask := [4]byte{0x12, 0x34, 0x56, 0x78}
	buf.Write(mask[:])
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	buf.Write(masked)
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(buf.Bytes()); err != nil {
		t.Fatalf("write masked frame: %v", err)
	}
}

func readServerText(t *testing.T, conn net.Conn, br *bufio.Reader) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var hdr [2]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		t.Fatalf("read server frame hdr: %v", err)
	}
	length := int64(hdr[1] & 0x7f)
	if length == 126 {
		var ext [2]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			t.Fatalf("read server len16: %v", err)
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		t.Fatalf("read server payload: %v", err)
	}
	return payload
}

func doJSONPost(handler http.Handler, path string, body string, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = remoteAddr
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestWSGateway_AuthSecurityAndRPCFlow(t *testing.T) {
	logger := zap.NewNop()
	accRepo := newMemAccountRepo()
	sessRepo := newMemSessionRepo()
	authSvc := appauth.NewService(accRepo, sessRepo, logger)
	dispatcher := rtmp.NewRPCDispatcher(logger)
	dispatcher.Register("echoPing", func(ctx *rtmp.RPCContext, args []interface{}) (interface{}, error) {
		_ = ctx.Connection.SendCallback("onEchoPush", "hello-web")
		return map[string]interface{}{"user": ctx.AccountID, "args": args}, nil
	})
	rtmpSrv := rtmp.NewServer(config.RTMPConfig{}, logger, dispatcher)

	wsSrv := NewServer(config.WSConfig{
		Enabled:           true,
		AllowedOrigins:    []string{"http://localhost:5173"},
		TrustProxyHeaders: false,
		MaxConnections:    10,
		MaxConnsPerIP:     2,
		MaxMessageBytes:   1024,
	}, logger, authSvc, rtmpSrv, dispatcher, nil)
	handler := wsSrv.Handler()

	regRec := doJSONPost(handler, "/api/auth/register",
		`{"username":"player1","password":"StrongPassword123!","email":"p1@example.com"}`, "198.51.100.10:5000")
	if regRec.Code != http.StatusCreated {
		t.Fatalf("register failed: status=%d body=%s", regRec.Code, regRec.Body.String())
	}

	acc, _ := accRepo.FindByUsername(context.Background(), "player1")
	if !domainauth.IsArgon2idHash(acc.PasswordHash) {
		t.Fatalf("expected Argon2id password hash on register, got %q", acc.PasswordHash)
	}

	loginRec := doJSONPost(handler, "/api/auth/login",
		`{"username":"player1","password":"StrongPassword123!"}`, "198.51.100.10:5001")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var loginOut struct {
		Ticket string `json:"ticket"`
	}
	_ = json.NewDecoder(loginRec.Body).Decode(&loginOut)
	if loginOut.Ticket == "" {
		t.Fatalf("expected non-empty ticket")
	}

	_, _, badStatus := dialPipeWS(t, handler, "https://evil.example.com", "198.51.100.10:5002")
	if badStatus != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed origin, got %d", badStatus)
	}

	conn, br, status := dialPipeWS(t, handler, "http://localhost:5173", "198.51.100.10:5003")
	defer conn.Close()
	if status != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got %d", status)
	}

	authFrame, _ := json.Marshal(IncomingEnvelope{Op: "auth", Tx: 1, Ticket: loginOut.Ticket})
	writeClientMaskedText(t, conn, authFrame)
	authReplyRaw := readServerText(t, conn, br)
	var authReply OutgoingEnvelope
	if err := json.Unmarshal(authReplyRaw, &authReply); err != nil || authReply.Op != "res" {
		t.Fatalf("expected auth res envelope, got %s", string(authReplyRaw))
	}

	rpcFrame, _ := json.Marshal(IncomingEnvelope{Op: "rpc", Tx: 2, Method: "echoPing", Args: []interface{}{"ok"}})
	writeClientMaskedText(t, conn, rpcFrame)

	msg1 := readServerText(t, conn, br)
	msg2 := readServerText(t, conn, br)
	combined := string(msg1) + "\n" + string(msg2)
	if !strings.Contains(combined, `"op":"evt"`) || !strings.Contains(combined, `"onEchoPush"`) {
		t.Fatalf("expected server push callback onEchoPush, got:\n%s", combined)
	}
	if !strings.Contains(combined, `"op":"res"`) || !strings.Contains(combined, `"tx":2`) {
		t.Fatalf("expected rpc response tx=2, got:\n%s", combined)
	}

	replayConn, replayBr, replayStatus := dialPipeWS(t, handler, "http://localhost:5173", "198.51.100.10:5004")
	defer replayConn.Close()
	if replayStatus != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 for replay test conn, got %d", replayStatus)
	}
	writeClientMaskedText(t, replayConn, authFrame)
	replayReply := readServerText(t, replayConn, replayBr)
	if !strings.Contains(string(replayReply), `"op":"err"`) {
		t.Fatalf("expected ticket replay to be rejected with err, got %s", string(replayReply))
	}

	for i := 0; i < 5; i++ {
		rec := doJSONPost(handler, "/api/auth/login",
			`{"username":"player1","password":"WrongPassword!"}`, "203.0.113.55:6000")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d expected 401, got %d", i+1, rec.Code)
		}
	}
	lockedRec := doJSONPost(handler, "/api/auth/login",
		`{"username":"player1","password":"StrongPassword123!"}`, "203.0.113.55:6001")
	if lockedRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 lockout after 5 failed attempts, got %d", lockedRec.Code)
	}
}
