// Ephemeral single-use WebSocket authentication tickets backed by Redis
// (atomic GETDEL with 30s TTL) with an in-memory fallback for tests and
// standalone runs without Redis.
package security

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultTicketTTL = 30 * time.Second
	ticketKeyPrefix  = "mcgame:ws:ticket:"
)

var (
	ErrTicketNotFound = errors.New("ticket not found or expired")
	ErrTicketMismatch = errors.New("ticket client mismatch")
)

type WSTicket struct {
	SessionID string    `json:"sessionId"`
	AccountID string    `json:"accountId"`
	Username  string    `json:"username"`
	IPAddress string    `json:"ipAddress"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type TicketStore struct {
	rdb   redis.Cmdable
	ttl   time.Duration
	mu    sync.Mutex
	local map[string]WSTicket
}

func NewTicketStore(rdb redis.Cmdable, ttl time.Duration) *TicketStore {
	if ttl <= 0 {
		ttl = defaultTicketTTL
	}
	return &TicketStore{
		rdb:   rdb,
		ttl:   ttl,
		local: make(map[string]WSTicket),
	}
}

func (s *TicketStore) Issue(ctx context.Context, ticket WSTicket) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generate ws ticket: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now().UTC()
	ticket.IssuedAt = now
	ticket.ExpiresAt = now.Add(s.ttl)

	if s.rdb != nil {
		payload, err := json.Marshal(ticket)
		if err != nil {
			return "", time.Time{}, err
		}
		if err := s.rdb.Set(ctx, ticketKeyPrefix+token, payload, s.ttl).Err(); err != nil {
			return "", time.Time{}, fmt.Errorf("store ws ticket in redis: %w", err)
		}
		return token, ticket.ExpiresAt, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	s.local[token] = ticket
	return token, ticket.ExpiresAt, nil
}

func (s *TicketStore) Consume(ctx context.Context, token string, clientIP string) (*WSTicket, error) {
	if token == "" {
		return nil, ErrTicketNotFound
	}
	var ticket WSTicket
	if s.rdb != nil {
		val, err := s.rdb.GetDel(ctx, ticketKeyPrefix+token).Bytes()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return nil, ErrTicketNotFound
			}
			return nil, err
		}
		if err := json.Unmarshal(val, &ticket); err != nil {
			return nil, ErrTicketNotFound
		}
	} else {
		s.mu.Lock()
		now := time.Now().UTC()
		s.pruneExpiredLocked(now)
		found, ok := s.local[token]
		if ok {
			delete(s.local, token)
		}
		s.mu.Unlock()
		if !ok {
			return nil, ErrTicketNotFound
		}
		ticket = found
	}

	if time.Now().UTC().After(ticket.ExpiresAt) {
		return nil, ErrTicketNotFound
	}
	if ticket.IPAddress != "" && clientIP != "" && ticket.IPAddress != clientIP {
		return nil, ErrTicketMismatch
	}
	return &ticket, nil
}

func (s *TicketStore) pruneExpiredLocked(now time.Time) {
	for k, v := range s.local {
		if now.After(v.ExpiresAt) {
			delete(s.local, k)
		}
	}
}
