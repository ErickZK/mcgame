// Server-authoritative movement validation to prevent teleport hacks,
// out-of-bounds positioning, and speedhacks from browser DevTools or modified
// game clients while tolerating normal latency jitter and authorized teleports.
package security

import (
	"math"
	"sync"
	"time"
)

const (
	DefaultMaxSpeedPxPerSec = 950.0
	DefaultBurstTolerancePx = 320.0
)

type MovementAnchor struct {
	MapID     int
	X         int
	Y         int
	UpdatedAt time.Time
}

type MovementResult struct {
	Allowed   bool
	Reason    string
	LastMapID int
	LastX     int
	LastY     int
}

type MovementValidator struct {
	mu               sync.Mutex
	anchors          map[int64]MovementAnchor
	maxSpeedPxPerSec float64
	burstTolerancePx float64
}

func NewMovementValidator(maxSpeedPxPerSec float64, burstTolerancePx float64) *MovementValidator {
	if maxSpeedPxPerSec <= 0 {
		maxSpeedPxPerSec = DefaultMaxSpeedPxPerSec
	}
	if burstTolerancePx <= 0 {
		burstTolerancePx = DefaultBurstTolerancePx
	}
	return &MovementValidator{
		anchors:          make(map[int64]MovementAnchor),
		maxSpeedPxPerSec: maxSpeedPxPerSec,
		burstTolerancePx: burstTolerancePx,
	}
}

func (v *MovementValidator) ResetAnchor(characterID int64, mapID int, x int, y int, now time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.anchors[characterID] = MovementAnchor{
		MapID:     mapID,
		X:         x,
		Y:         y,
		UpdatedAt: now,
	}
}

func (v *MovementValidator) Remove(characterID int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.anchors, characterID)
}

func (v *MovementValidator) ValidateAndRecord(characterID int64, mapID int, x int, y int, mapWidth int, mapHeight int, now time.Time) MovementResult {
	if x < 0 || y < 0 {
		return v.rejectWithLast(characterID, "negative coordinates")
	}
	if mapWidth > 0 && x > mapWidth {
		return v.rejectWithLast(characterID, "x coordinate exceeds map bounds")
	}
	if mapHeight > 0 && y > mapHeight {
		return v.rejectWithLast(characterID, "y coordinate exceeds map bounds")
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	prev, exists := v.anchors[characterID]
	if !exists || prev.MapID != mapID {
		v.anchors[characterID] = MovementAnchor{
			MapID:     mapID,
			X:         x,
			Y:         y,
			UpdatedAt: now,
		}
		return MovementResult{Allowed: true, LastMapID: mapID, LastX: x, LastY: y}
	}

	dt := now.Sub(prev.UpdatedAt).Seconds()
	if dt < 0 {
		dt = 0
	}
	if dt > 10 {
		dt = 10
	}
	dx := float64(x - prev.X)
	dy := float64(y - prev.Y)
	dist := math.Hypot(dx, dy)
	maxAllowed := v.maxSpeedPxPerSec*dt + v.burstTolerancePx

	if dist > maxAllowed {
		return MovementResult{
			Allowed:   false,
			Reason:    "movement speed exceeded",
			LastMapID: prev.MapID,
			LastX:     prev.X,
			LastY:     prev.Y,
		}
	}

	v.anchors[characterID] = MovementAnchor{
		MapID:     mapID,
		X:         x,
		Y:         y,
		UpdatedAt: now,
	}
	return MovementResult{Allowed: true, LastMapID: mapID, LastX: x, LastY: y}
}

func (v *MovementValidator) rejectWithLast(characterID int64, reason string) MovementResult {
	v.mu.Lock()
	defer v.mu.Unlock()
	prev := v.anchors[characterID]
	return MovementResult{
		Allowed:   false,
		Reason:    reason,
		LastMapID: prev.MapID,
		LastX:     prev.X,
		LastY:     prev.Y,
	}
}
