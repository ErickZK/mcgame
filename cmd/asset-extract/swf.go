// This file implements a pure-Go SWF container parser (FWS/CWS decompression,
// header decoding, tag iteration, SymbolClass/ExportAssets mapping, and
// DefineSprite/FrameLabel/PlaceObject timeline metadata extraction).
package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

type SWFHeader struct {
	Signature  string  `json:"signature"`
	Version    uint8   `json:"version"`
	FileLength uint32  `json:"fileLength"`
	WidthPx    float64 `json:"widthPx"`
	HeightPx   float64 `json:"heightPx"`
	FrameRate  float64 `json:"frameRate"`
	FrameCount uint16  `json:"frameCount"`
}

type SWFTag struct {
	Code uint16
	Body []byte
}

type SWFSpriteMeta struct {
	CharacterID uint16            `json:"characterId"`
	SymbolName  string            `json:"symbolName,omitempty"`
	FrameCount  uint16            `json:"frameCount"`
	FrameLabels map[int]string    `json:"frameLabels,omitempty"`
	Referenced  []uint16          `json:"referencedIds,omitempty"`
	Placements  []SWFPlacementRec `json:"placements,omitempty"`
}

type SWFPlacementRec struct {
	Frame       int     `json:"frame"`
	Depth       uint16  `json:"depth"`
	CharacterID uint16  `json:"characterId,omitempty"`
	Name        string  `json:"name,omitempty"`
	Tx          float64 `json:"tx,omitempty"`
	Ty          float64 `json:"ty,omitempty"`
	ScaleX      float64 `json:"scaleX,omitempty"`
	ScaleY      float64 `json:"scaleY,omitempty"`
}

type SWFMetadata struct {
	Header         SWFHeader         `json:"header"`
	Symbols        map[uint16]string `json:"symbols,omitempty"`
	MainLabels     map[int]string    `json:"mainFrameLabels,omitempty"`
	Sprites        []SWFSpriteMeta   `json:"sprites,omitempty"`
	TagCounts      map[string]int    `json:"tagCounts"`
	ExtractedFiles []string          `json:"extractedFiles,omitempty"`
	HasVectorShape bool              `json:"hasVectorShapes"`
	HasScripts     bool              `json:"hasScripts"`
	HasFonts       bool              `json:"hasFonts"`
}

type bitReader struct {
	data   []byte
	bitPos int
}

func (br *bitReader) readUB(n int) uint32 {
	var val uint32
	for i := 0; i < n; i++ {
		byteIdx := br.bitPos >> 3
		if byteIdx >= len(br.data) {
			return val
		}
		bitIdx := 7 - (br.bitPos & 7)
		val = (val << 1) | uint32((br.data[byteIdx]>>bitIdx)&1)
		br.bitPos++
	}
	return val
}

func (br *bitReader) readSB(n int) int32 {
	if n == 0 {
		return 0
	}
	u := br.readUB(n)
	signBit := uint32(1) << (n - 1)
	if (u & signBit) != 0 {
		u |= ^((uint32(1) << n) - 1)
	}
	return int32(u)
}

func (br *bitReader) alignByte() {
	rem := br.bitPos & 7
	if rem != 0 {
		br.bitPos += 8 - rem
	}
}

func (br *bitReader) byteOffset() int {
	br.alignByte()
	return br.bitPos >> 3
}

func decompressSWF(raw []byte) ([]byte, SWFHeader, error) {
	if len(raw) < 8 {
		return nil, SWFHeader{}, fmt.Errorf("swf too short: %d bytes", len(raw))
	}
	sig := string(raw[:3])
	hdr := SWFHeader{
		Signature:  sig,
		Version:    raw[3],
		FileLength: binary.LittleEndian.Uint32(raw[4:8]),
	}

	var body []byte
	switch sig {
	case "FWS":
		body = raw[8:]
	case "CWS":
		zr, err := zlib.NewReader(bytes.NewReader(raw[8:]))
		if err != nil {
			return nil, hdr, fmt.Errorf("cws zlib open: %w", err)
		}
		defer zr.Close()
		var decErr error
		body, decErr = io.ReadAll(zr)
		if decErr != nil {
			return nil, hdr, fmt.Errorf("cws zlib read: %w", decErr)
		}
	default:
		return nil, hdr, fmt.Errorf("unsupported swf signature: %s", sig)
	}

	fws := make([]byte, 8+len(body))
	copy(fws[:3], "FWS")
	fws[3] = hdr.Version
	binary.LittleEndian.PutUint32(fws[4:8], uint32(len(fws)))
	copy(fws[8:], body)

	br := &bitReader{data: body}
	nbits := int(br.readUB(5))
	xmin := br.readSB(nbits)
	xmax := br.readSB(nbits)
	ymin := br.readSB(nbits)
	ymax := br.readSB(nbits)
	off := br.byteOffset()
	if off+4 <= len(body) {
		hdr.FrameRate = float64(binary.LittleEndian.Uint16(body[off:off+2])) / 256.0
		hdr.FrameCount = binary.LittleEndian.Uint16(body[off+2 : off+4])
	}
	hdr.WidthPx = float64(xmax-xmin) / 20.0
	hdr.HeightPx = float64(ymax-ymin) / 20.0
	return fws, hdr, nil
}

func parseSWFTags(fws []byte) ([]SWFTag, error) {
	if len(fws) < 12 {
		return nil, fmt.Errorf("fws too short")
	}
	body := fws[8:]
	br := &bitReader{data: body}
	nbits := int(br.readUB(5))
	br.bitPos += nbits * 4
	off := br.byteOffset() + 4
	if off > len(body) {
		return nil, fmt.Errorf("invalid swf header offset")
	}
	return parseTagStream(body[off:]), nil
}

func parseTagStream(stream []byte) []SWFTag {
	var tags []SWFTag
	off := 0
	for off+2 <= len(stream) {
		tagCodeAndLen := binary.LittleEndian.Uint16(stream[off : off+2])
		off += 2
		code := tagCodeAndLen >> 6
		length := int(tagCodeAndLen & 0x3f)
		if length == 0x3f {
			if off+4 > len(stream) {
				break
			}
			length = int(binary.LittleEndian.Uint32(stream[off : off+4]))
			off += 4
		}
		if length < 0 || off+length > len(stream) {
			break
		}
		tags = append(tags, SWFTag{
			Code: code,
			Body: stream[off : off+length],
		})
		off += length
		if code == 0 {
			break
		}
	}
	return tags
}

func parseSymbolMap(tagBody []byte, symbols map[uint16]string) {
	if len(tagBody) < 2 {
		return
	}
	count := int(binary.LittleEndian.Uint16(tagBody[0:2]))
	off := 2
	for i := 0; i < count && off+2 <= len(tagBody); i++ {
		id := binary.LittleEndian.Uint16(tagBody[off : off+2])
		off += 2
		end := bytes.IndexByte(tagBody[off:], 0)
		if end < 0 {
			break
		}
		name := string(tagBody[off : off+end])
		off += end + 1
		if name != "" {
			symbols[id] = name
		}
	}
}

func parseSpriteTag(tagBody []byte, symbols map[uint16]string) (SWFSpriteMeta, bool) {
	if len(tagBody) < 4 {
		return SWFSpriteMeta{}, false
	}
	charID := binary.LittleEndian.Uint16(tagBody[0:2])
	frameCount := binary.LittleEndian.Uint16(tagBody[2:4])
	subTags := parseTagStream(tagBody[4:])

	meta := SWFSpriteMeta{
		CharacterID: charID,
		SymbolName:  symbols[charID],
		FrameCount:  frameCount,
	}
	refSet := make(map[uint16]struct{})
	frame := 1

	for _, st := range subTags {
		switch st.Code {
		case 1:
			frame++
		case 43:
			end := bytes.IndexByte(st.Body, 0)
			if end > 0 {
				if meta.FrameLabels == nil {
					meta.FrameLabels = make(map[int]string)
				}
				meta.FrameLabels[frame] = string(st.Body[:end])
			}
		case 26, 70:
			rec, ok := parsePlaceObject(st.Code, st.Body, frame)
			if ok {
				if rec.CharacterID > 0 {
					refSet[rec.CharacterID] = struct{}{}
				}
				if len(meta.Placements) < 256 {
					meta.Placements = append(meta.Placements, rec)
				}
			}
		}
	}
	for id := range refSet {
		meta.Referenced = append(meta.Referenced, id)
	}
	return meta, true
}

func parsePlaceObject(code uint16, body []byte, frame int) (SWFPlacementRec, bool) {
	rec := SWFPlacementRec{Frame: frame}
	minLen := 3
	if code == 70 {
		minLen = 4
	}
	if len(body) < minLen {
		return rec, false
	}
	flags := body[0]
	off := 1
	if code == 70 {
		off = 2
	}
	if off+2 > len(body) {
		return rec, false
	}
	rec.Depth = binary.LittleEndian.Uint16(body[off : off+2])
	off += 2

	hasChar := (flags & 0x02) != 0
	hasMatrix := (flags & 0x04) != 0
	hasColor := (flags & 0x08) != 0
	hasRatio := (flags & 0x10) != 0
	hasName := (flags & 0x20) != 0

	if code == 70 && len(body) > 1 && (body[1]&0x10) != 0 {
		end := bytes.IndexByte(body[off:], 0)
		if end < 0 {
			return rec, true
		}
		off += end + 1
	}
	if hasChar {
		if off+2 > len(body) {
			return rec, true
		}
		rec.CharacterID = binary.LittleEndian.Uint16(body[off : off+2])
		off += 2
	}
	if hasMatrix && off < len(body) {
		br := &bitReader{data: body[off:]}
		if br.readUB(1) == 1 {
			nScale := int(br.readUB(5))
			sx := float64(br.readSB(nScale)) / 65536.0
			sy := float64(br.readSB(nScale)) / 65536.0
			rec.ScaleX = math.Round(sx*1000) / 1000
			rec.ScaleY = math.Round(sy*1000) / 1000
		}
		if br.readUB(1) == 1 {
			nRot := int(br.readUB(5))
			br.bitPos += nRot * 2
		}
		nTrans := int(br.readUB(5))
		tx := float64(br.readSB(nTrans)) / 20.0
		ty := float64(br.readSB(nTrans)) / 20.0
		rec.Tx = math.Round(tx*100) / 100
		rec.Ty = math.Round(ty*100) / 100
		off += br.byteOffset()
	}
	if hasColor && off < len(body) {
		br := &bitReader{data: body[off:]}
		hasAdd := br.readUB(1)
		hasMult := br.readUB(1)
		nBits := int(br.readUB(4))
		if hasMult == 1 {
			br.bitPos += nBits * 4
		}
		if hasAdd == 1 {
			br.bitPos += nBits * 4
		}
		off += br.byteOffset()
	}
	if hasRatio {
		off += 2
	}
	if hasName && off < len(body) {
		end := bytes.IndexByte(body[off:], 0)
		if end > 0 {
			rec.Name = string(body[off : off+end])
		}
	}
	return rec, true
}

func tagName(code uint16) string {
	switch code {
	case 0:
		return "End"
	case 1:
		return "ShowFrame"
	case 2:
		return "DefineShape"
	case 6:
		return "DefineBits"
	case 8:
		return "JPEGTables"
	case 10, 48, 75:
		return "DefineFont"
	case 11, 33, 37:
		return "DefineText"
	case 12, 59, 82:
		return "DoActionOrABC"
	case 14:
		return "DefineSound"
	case 18, 45:
		return "SoundStreamHead"
	case 19:
		return "SoundStreamBlock"
	case 20:
		return "DefineBitsLossless"
	case 21:
		return "DefineBitsJPEG2"
	case 22:
		return "DefineShape2"
	case 26:
		return "PlaceObject2"
	case 32:
		return "DefineShape3"
	case 35:
		return "DefineBitsJPEG3"
	case 36:
		return "DefineBitsLossless2"
	case 39:
		return "DefineSprite"
	case 43:
		return "FrameLabel"
	case 56:
		return "ExportAssets"
	case 70:
		return "PlaceObject3"
	case 76:
		return "SymbolClass"
	case 83:
		return "DefineShape4"
	case 87:
		return "DefineBinaryData"
	case 90:
		return "DefineBitsJPEG4"
	default:
		return fmt.Sprintf("Tag_%d", code)
	}
}
