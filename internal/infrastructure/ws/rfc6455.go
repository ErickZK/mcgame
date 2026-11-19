// Pure-Go RFC 6455 WebSocket server connection implementation with strict
// payload caps, mandatory client frame masking validation, ping/pong handling,
// fragmented message reassembly, and synchronized writes.
package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA

	defaultMaxFrameBytes = 64 * 1024
	defaultWriteTimeout  = 5 * time.Second
)

var (
	ErrPayloadTooLarge = errors.New("websocket payload exceeds limit")
	ErrUnmaskedFrame   = errors.New("client websocket frame must be masked")
	ErrConnClosed      = errors.New("websocket connection closed")
)

type FrameConn struct {
	netConn    net.Conn
	br         *bufio.Reader
	writeMu    sync.Mutex
	closeOnce  sync.Once
	closed     bool
	maxPayload int64
}

func Upgrade(w http.ResponseWriter, r *http.Request, maxPayload int64) (*FrameConn, error) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil, errors.New("websocket upgrade requires GET")
	}
	if !headerContainsToken(r.Header.Get("Connection"), "upgrade") ||
		!strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") ||
		strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")) != "13" {
		http.Error(w, "invalid websocket upgrade headers", http.StatusBadRequest)
		return nil, errors.New("invalid websocket headers")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		http.Error(w, "missing websocket key", http.StatusBadRequest)
		return nil, errors.New("missing Sec-WebSocket-Key")
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return nil, errors.New("response writer does not support hijacking")
	}

	netConn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	accept := computeAcceptKey(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"

	_ = netConn.SetWriteDeadline(time.Now().Add(defaultWriteTimeout))
	if _, err := rw.WriteString(resp); err != nil {
		_ = netConn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = netConn.Close()
		return nil, err
	}
	_ = netConn.SetWriteDeadline(time.Time{})

	if maxPayload <= 0 {
		maxPayload = defaultMaxFrameBytes
	}
	return &FrameConn{
		netConn:    netConn,
		br:         rw.Reader,
		maxPayload: maxPayload,
	}, nil
}

func computeAcceptKey(key string) string {
	h := sha1.New()
	_, _ = h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func headerContainsToken(headerVal string, token string) bool {
	for _, part := range strings.Split(headerVal, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func (c *FrameConn) SetReadDeadline(t time.Time) error {
	return c.netConn.SetReadDeadline(t)
}

func (c *FrameConn) ReadMessage() (byte, []byte, error) {
	var msgOp byte
	var assembled []byte

	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}

		switch op {
		case opPing:
			_ = c.WriteControl(opPong, payload)
			continue
		case opPong:
			continue
		case opClose:
			_ = c.WriteControl(opClose, nil)
			return opClose, nil, io.EOF
		case opText, opBinary:
			if msgOp != 0 {
				return 0, nil, errors.New("unexpected new data frame before previous finished")
			}
			msgOp = op
			assembled = append(assembled, payload...)
		case opContinuation:
			if msgOp == 0 {
				return 0, nil, errors.New("unexpected continuation frame")
			}
			if int64(len(assembled)+len(payload)) > c.maxPayload {
				return 0, nil, ErrPayloadTooLarge
			}
			assembled = append(assembled, payload...)
		default:
			return 0, nil, fmt.Errorf("unsupported websocket opcode: %d", op)
		}

		if fin {
			return msgOp, assembled, nil
		}
	}
}

func (c *FrameConn) readFrame() (bool, byte, []byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
		return false, 0, nil, err
	}
	fin := (hdr[0] & 0x80) != 0
	op := hdr[0] & 0x0f
	masked := (hdr[1] & 0x80) != 0
	if !masked {
		return false, 0, nil, ErrUnmaskedFrame
	}

	length := int64(hdr[1] & 0x7f)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		u := binary.BigEndian.Uint64(ext[:])
		if u > uint64(c.maxPayload) {
			return false, 0, nil, ErrPayloadTooLarge
		}
		length = int64(u)
	}

	if length > c.maxPayload {
		return false, 0, nil, ErrPayloadTooLarge
	}
	if op >= 0x8 && length > 125 {
		return false, 0, nil, ErrPayloadTooLarge
	}

	var maskKey [4]byte
	if _, err := io.ReadFull(c.br, maskKey[:]); err != nil {
		return false, 0, nil, err
	}

	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return false, 0, nil, err
		}
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}

	return fin, op, payload, nil
}

func (c *FrameConn) WriteText(data []byte) error {
	return c.writeFrame(opText, data)
}

func (c *FrameConn) WritePing() error {
	return c.WriteControl(opPing, []byte("hb"))
}

func (c *FrameConn) WriteControl(op byte, payload []byte) error {
	if len(payload) > 125 {
		payload = payload[:125]
	}
	return c.writeFrame(op, payload)
}

func (c *FrameConn) writeFrame(op byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return ErrConnClosed
	}

	var hdr [10]byte
	hdr[0] = 0x80 | (op & 0x0f)
	hdrLen := 2
	n := len(payload)
	switch {
	case n <= 125:
		hdr[1] = byte(n)
	case n <= 65535:
		hdr[1] = 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(n))
		hdrLen = 4
	default:
		hdr[1] = 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(n))
		hdrLen = 10
	}

	_ = c.netConn.SetWriteDeadline(time.Now().Add(defaultWriteTimeout))
	if _, err := c.netConn.Write(hdr[:hdrLen]); err != nil {
		return err
	}
	if n > 0 {
		if _, err := c.netConn.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func (c *FrameConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.writeMu.Lock()
		c.closed = true
		c.writeMu.Unlock()
		err = c.netConn.Close()
	})
	return err
}
