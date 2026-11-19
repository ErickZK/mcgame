// Collision mask extractor for MAP_HIT_NEW.swf files. Unwraps the zlib
// container and SWF DefineBinaryData tag, decompresses the per-pixel bitset
// used by HitTestContainer.as, and downsamples it into a compact 20px A* grid
// binary (uint16 cols, uint16 rows, packed LSB-first bitmask).
package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"os"
)

const collisionStepPx = 20

func buildCollisionGridFromSWF(swfPath string, mapWidth int, mapHeight int, step int) ([]byte, bool) {
	if mapWidth <= 0 || mapHeight <= 0 || step <= 0 {
		return nil, false
	}
	raw, err := os.ReadFile(swfPath)
	if err != nil || len(raw) < 12 {
		return nil, false
	}

	swfBytes := unwrapZlibIfNeeded(raw)
	fws, ok := decompressSWFToFWS(swfBytes)
	if !ok {
		return nil, false
	}

	binPayload, ok := extractFirstBinaryDataTag(fws)
	if !ok {
		return nil, false
	}
	bitmask := unwrapZlibIfNeeded(binPayload)
	expectedBytes := (mapWidth*mapHeight + 7) / 8
	if len(bitmask) < expectedBytes {
		return nil, false
	}

	cols := (mapWidth + step - 1) / step
	rows := (mapHeight + step - 1) / step
	gridBytes := make([]byte, 4+(cols*rows+7)/8)
	binary.LittleEndian.PutUint16(gridBytes[0:2], uint16(cols))
	binary.LittleEndian.PutUint16(gridBytes[2:4], uint16(rows))

	for gy := 0; gy < rows; gy++ {
		py := gy*step + step/2
		if py >= mapHeight {
			py = mapHeight - 1
		}
		for gx := 0; gx < cols; gx++ {
			px := gx*step + step/2
			if px >= mapWidth {
				px = mapWidth - 1
			}
			bitIdx := py*mapWidth + px
			walkable := (bitmask[bitIdx>>3] & (1 << uint(bitIdx&7))) != 0
			if walkable {
				gIdx := gy*cols + gx
				gridBytes[4+(gIdx>>3)] |= 1 << uint(gIdx&7)
			}
		}
	}

	return gridBytes, true
}

func unwrapZlibIfNeeded(data []byte) []byte {
	if len(data) < 6 {
		return data
	}
	cmf := data[0]
	flg := data[1]
	if (cmf&0x0f) != 8 || (cmf>>4) > 7 || (int(cmf)*256+int(flg))%31 != 0 {
		return data
	}
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return data
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil || len(out) == 0 {
		return data
	}
	return out
}

func decompressSWFToFWS(data []byte) ([]byte, bool) {
	if len(data) < 12 {
		return nil, false
	}
	sig := string(data[:3])
	if sig == "FWS" {
		return data, true
	}
	if sig != "CWS" {
		return nil, false
	}
	r, err := zlib.NewReader(bytes.NewReader(data[8:]))
	if err != nil {
		return nil, false
	}
	defer r.Close()
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, false
	}
	fws := make([]byte, 8+len(body))
	copy(fws[:3], "FWS")
	fws[3] = data[3]
	binary.LittleEndian.PutUint32(fws[4:8], uint32(len(fws)))
	copy(fws[8:], body)
	return fws, true
}

func swfTagStreamOffset(fws []byte) int {
	if len(fws) < 14 {
		return -1
	}
	body := fws[8:]
	nbits := int((body[0] >> 3) & 0x1f)
	totalBits := 5 + nbits*4
	rectBytes := (totalBits + 7) / 8
	off := 8 + rectBytes + 4
	if off >= len(fws) {
		return -1
	}
	return off
}

func extractFirstBinaryDataTag(fws []byte) ([]byte, bool) {
	off := swfTagStreamOffset(fws)
	if off < 0 {
		return nil, false
	}
	for off+2 <= len(fws) {
		codeAndLen := binary.LittleEndian.Uint16(fws[off : off+2])
		off += 2
		code := codeAndLen >> 6
		length := int(codeAndLen & 0x3f)
		if length == 0x3f {
			if off+4 > len(fws) {
				break
			}
			length = int(binary.LittleEndian.Uint32(fws[off : off+4]))
			off += 4
		}
		if length < 0 || off+length > len(fws) {
			break
		}
		body := fws[off : off+length]
		off += length
		if code == 87 && len(body) > 6 {
			return body[6:], true
		}
		if code == 0 {
			break
		}
	}
	return nil, false
}
