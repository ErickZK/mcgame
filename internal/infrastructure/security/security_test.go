// Unit tests for security primitives: single-use WS tickets, brute-force
// lockout, IP connection limits, authoritative movement validator, and XSS
// sanitization.
package security

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTicketStore_SingleUseAndIPBinding(t *testing.T) {
	store := NewTicketStore(nil, 2*time.Second)
	token, _, err := store.Issue(context.Background(), WSTicket{
		SessionID: "sess-1",
		AccountID: "acc-1",
		Username:  "alice",
		IPAddress: "203.0.113.10",
	})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}

	if _, err := store.Consume(context.Background(), token, "198.51.100.99"); err != ErrTicketMismatch {
		t.Fatalf("expected ErrTicketMismatch for wrong IP, got %v", err)
	}

	if _, err := store.Consume(context.Background(), token, "203.0.113.10"); err != ErrTicketNotFound {
		t.Fatalf("expected consumed ticket to be invalidated after mismatch, got %v", err)
	}

	token2, _, err := store.Issue(context.Background(), WSTicket{
		SessionID: "sess-2",
		AccountID: "acc-1",
		Username:  "alice",
		IPAddress: "203.0.113.10",
	})
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	got, err := store.Consume(context.Background(), token2, "203.0.113.10")
	if err != nil || got.Username != "alice" {
		t.Fatalf("expected valid ticket consume, got %+v err=%v", got, err)
	}
	if _, err := store.Consume(context.Background(), token2, "203.0.113.10"); err != ErrTicketNotFound {
		t.Fatalf("expected replay consume to fail with ErrTicketNotFound, got %v", err)
	}
}

func TestLoginLimiter_ProgressiveLockout(t *testing.T) {
	lim := NewLoginLimiter()
	for i := 0; i < 4; i++ {
		if d := lim.RecordFailure("ip:1.2.3.4"); d != 0 {
			t.Fatalf("attempt %d should not lock yet, got %v", i+1, d)
		}
	}
	if d := lim.RecordFailure("ip:1.2.3.4"); d == 0 {
		t.Fatalf("5th failure should trigger lockout")
	}
	if ok, _ := lim.Check("ip:1.2.3.4"); ok {
		t.Fatalf("expected key to be locked")
	}
	lim.RecordSuccess("ip:1.2.3.4")
	if ok, _ := lim.Check("ip:1.2.3.4"); !ok {
		t.Fatalf("expected key to unlock after success")
	}
}

func TestIPConnTracker_Limits(t *testing.T) {
	tr := NewIPConnTracker(3, 2)
	if ok, _ := tr.Acquire("10.0.0.1"); !ok {
		t.Fatalf("1st acquire failed")
	}
	if ok, _ := tr.Acquire("10.0.0.1"); !ok {
		t.Fatalf("2nd acquire failed")
	}
	if ok, _ := tr.Acquire("10.0.0.1"); ok {
		t.Fatalf("3rd acquire from same IP should fail")
	}
	if ok, _ := tr.Acquire("10.0.0.2"); !ok {
		t.Fatalf("1st acquire from second IP failed")
	}
	if ok, _ := tr.Acquire("10.0.0.3"); ok {
		t.Fatalf("4th global acquire should fail")
	}
	tr.Release("10.0.0.1")
	if ok, _ := tr.Acquire("10.0.0.1"); !ok {
		t.Fatalf("acquire after release should succeed")
	}
}

func TestMovementValidator_AntiSpeedhackAndBounds(t *testing.T) {
	v := NewMovementValidator(500, 100)
	t0 := time.Now()

	res := v.ValidateAndRecord(101, 1, 200, 200, 2000, 2000, t0)
	if !res.Allowed {
		t.Fatalf("initial valid position should be allowed: %+v", res)
	}

	res = v.ValidateAndRecord(101, 1, 350, 200, 2000, 2000, t0.Add(500*time.Millisecond))
	if !res.Allowed {
		t.Fatalf("normal walk should be allowed: %+v", res)
	}

	res = v.ValidateAndRecord(101, 1, 1800, 1800, 2000, 2000, t0.Add(600*time.Millisecond))
	if res.Allowed {
		t.Fatalf("teleport hack should be rejected")
	}
	if res.LastX != 350 || res.LastY != 200 {
		t.Fatalf("expected rubber-band to (350,200), got (%d,%d)", res.LastX, res.LastY)
	}

	res = v.ValidateAndRecord(101, 1, 2500, 200, 2000, 2000, t0.Add(5*time.Second))
	if res.Allowed {
		t.Fatalf("out-of-bounds x should be rejected")
	}

	v.ResetAnchor(101, 1, 1800, 1800, t0.Add(6*time.Second))
	res = v.ValidateAndRecord(101, 1, 1850, 1800, 2000, 2000, t0.Add(6500*time.Millisecond))
	if !res.Allowed {
		t.Fatalf("movement after authorized teleport reset should be allowed")
	}
}

func TestSanitizeText_NeutralizesXSS(t *testing.T) {
	in := "<img src=x onerror=alert(1)>\x00hello"
	out := SanitizeText(in, 100)
	if strings.Contains(out, "<img") || strings.Contains(out, "\x00") {
		t.Fatalf("expected XSS payload and control chars neutralized, got %q", out)
	}
	if !strings.Contains(out, "&lt;img") {
		t.Fatalf("expected escaped angle bracket, got %q", out)
	}
}
