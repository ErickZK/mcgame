// WSTransport implements rtmp.Transport over a JSON-framed RFC 6455 WebSocket,
// allowing browser, Tauri, and Capacitor clients to invoke the exact same
// server RPC handlers and receive server-push callbacks alongside Flash clients.
package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
)

type OutgoingEnvelope struct {
	Op     string        `json:"op"`
	Tx     int64         `json:"tx,omitempty"`
	Method string        `json:"m,omitempty"`
	Args   []interface{} `json:"a,omitempty"`
	Data   interface{}   `json:"d,omitempty"`
	Error  string        `json:"e,omitempty"`
}

type IncomingEnvelope struct {
	Op     string        `json:"op"`
	Tx     int64         `json:"tx,omitempty"`
	Method string        `json:"m,omitempty"`
	Args   []interface{} `json:"a,omitempty"`
	Ticket string        `json:"ticket,omitempty"`
	App    string        `json:"app,omitempty"`
}

type WSTransport struct {
	conn *FrameConn
}

func NewWSTransport(conn *FrameConn) *WSTransport {
	return &WSTransport{conn: conn}
}

func (t *WSTransport) Kind() string {
	return "ws"
}

func (t *WSTransport) Close() error {
	if t.conn == nil {
		return nil
	}
	return t.conn.Close()
}

func (t *WSTransport) WriteCommand(ctx context.Context, method string, args []interface{}) error {
	return t.sendEnvelope(OutgoingEnvelope{
		Op:     "evt",
		Method: method,
		Args:   sanitizeSlice(args),
	})
}

func (t *WSTransport) WriteCommandSync(ctx context.Context, method string, args []interface{}) error {
	return t.WriteCommand(ctx, method, args)
}

func (t *WSTransport) WriteResult(ctx context.Context, txID int64, result interface{}) error {
	return t.sendEnvelope(OutgoingEnvelope{
		Op:   "res",
		Tx:   txID,
		Data: sanitizeValue(result),
	})
}

func (t *WSTransport) WriteError(ctx context.Context, txID int64, err error) error {
	msg := "internal error"
	if err != nil {
		msg = err.Error()
	}
	return t.sendEnvelope(OutgoingEnvelope{
		Op:    "err",
		Tx:    txID,
		Error: msg,
	})
}

func (t *WSTransport) sendEnvelope(env OutgoingEnvelope) error {
	if t.conn == nil {
		return fmt.Errorf("ws transport closed")
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal ws envelope: %w", err)
	}
	return t.conn.WriteText(raw)
}

func sanitizeSlice(items []interface{}) []interface{} {
	if items == nil {
		return nil
	}
	out := make([]interface{}, len(items))
	for i, item := range items {
		out[i] = sanitizeValue(item)
	}
	return out
}

func sanitizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return nil
		}
		return val
	case []interface{}:
		return sanitizeSlice(val)
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, item := range val {
			out[k] = sanitizeValue(item)
		}
		return out
	default:
		return v
	}
}
