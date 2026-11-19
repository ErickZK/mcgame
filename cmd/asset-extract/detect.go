// Package main provides asset-extract, a tool to decrypt, decompress, classify,
// and extract browser-ready images, sprites, sounds, configs, and source code
// from the MCGame Flash client and resource directories.
//
// This file implements container detection, BUS XOR decoding, zlib unwrapping,
// magic-byte sniffing, and logical category routing.
package main

import (
	"bytes"
	"compress/zlib"
	"io"
	"path"
	"strings"
)

type DetectedAsset struct {
	Kind       string
	Ext        string
	Layers     []string
	Data       []byte
	InnerError string
}

func decodeBUS(data []byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ byte((i+1)%3)
	}
	return out
}

func tryZlibDecompress(data []byte) ([]byte, bool) {
	if len(data) < 6 {
		return nil, false
	}
	cmf := data[0]
	flg := data[1]
	if (cmf&0x0f) != 8 || (cmf>>4) > 7 {
		return nil, false
	}
	if (int(cmf)*256+int(flg))%31 != 0 {
		return nil, false
	}
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil || len(out) == 0 {
		return nil, false
	}
	return out, true
}

func detectAndUnwrap(raw []byte) DetectedAsset {
	cur := raw
	var layers []string

	for depth := 0; depth < 4; depth++ {
		if len(cur) >= 3 && cur[0] == 'B' && cur[1] == 'U' && cur[2] == 'S' {
			decoded := decodeBUS(cur)
			if len(decoded) >= 3 && (decoded[0] == 'C' || decoded[0] == 'F' || decoded[0] == 'Z') && decoded[1] == 'W' && decoded[2] == 'S' {
				layers = append(layers, "bus-xor")
				cur = decoded
				continue
			}
		}

		if len(cur) >= 8 && bytes.Equal(cur[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
			return DetectedAsset{Kind: "image", Ext: ".png", Layers: layers, Data: cur}
		}
		if len(cur) >= 3 && cur[0] == 0xff && cur[1] == 0xd8 && cur[2] == 0xff {
			return DetectedAsset{Kind: "image", Ext: ".jpg", Layers: layers, Data: cur}
		}
		if len(cur) >= 6 && (bytes.HasPrefix(cur, []byte("GIF87a")) || bytes.HasPrefix(cur, []byte("GIF89a"))) {
			return DetectedAsset{Kind: "image", Ext: ".gif", Layers: layers, Data: cur}
		}
		if len(cur) >= 3 && (bytes.HasPrefix(cur, []byte("CWS")) || bytes.HasPrefix(cur, []byte("FWS")) || bytes.HasPrefix(cur, []byte("ZWS"))) {
			return DetectedAsset{Kind: "swf", Ext: ".swf", Layers: layers, Data: cur}
		}
		if len(cur) >= 12 && bytes.HasPrefix(cur, []byte("RIFF")) && bytes.Equal(cur[8:12], []byte("WAVE")) {
			return DetectedAsset{Kind: "sound", Ext: ".wav", Layers: layers, Data: cur}
		}
		if len(cur) >= 4 && bytes.HasPrefix(cur, []byte("OggS")) {
			return DetectedAsset{Kind: "sound", Ext: ".ogg", Layers: layers, Data: cur}
		}
		if isLikelyMP3(cur) {
			return DetectedAsset{Kind: "sound", Ext: ".mp3", Layers: layers, Data: cur}
		}
		if len(cur) >= 4 && bytes.HasPrefix(cur, []byte("PK\x03\x04")) {
			return DetectedAsset{Kind: "other", Ext: ".zip", Layers: layers, Data: cur}
		}

		if unpacked, ok := tryZlibDecompress(cur); ok {
			layers = append(layers, "zlib")
			cur = unpacked
			continue
		}

		if isLikelyXML(cur) {
			return DetectedAsset{Kind: "config", Ext: ".xml", Layers: layers, Data: cur}
		}
		if isLikelyText(cur) {
			return DetectedAsset{Kind: "other", Ext: ".txt", Layers: layers, Data: cur}
		}
		break
	}

	return DetectedAsset{Kind: "other", Ext: ".bin", Layers: layers, Data: cur}
}

func isLikelyMP3(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	if bytes.HasPrefix(data, []byte("ID3")) {
		return true
	}
	if data[0] == 0xff && (data[1]&0xe0) == 0xe0 {
		layer := (data[1] >> 1) & 0x03
		if layer == 1 {
			return true
		}
	}
	return false
}

func isLikelyXML(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) < 5 {
		return false
	}
	trimmed = bytes.TrimPrefix(trimmed, []byte{0xef, 0xbb, 0xbf})
	return bytes.HasPrefix(trimmed, []byte("<?xml")) || (trimmed[0] == '<' && bytes.Contains(trimmed[:min(len(trimmed), 256)], []byte(">")))
}

func isLikelyText(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	n := min(len(data), 512)
	printable := 0
	for i := 0; i < n; i++ {
		b := data[i]
		if b == 0 {
			return false
		}
		if b == '\n' || b == '\r' || b == '\t' || (b >= 0x20 && b <= 0x7e) || b >= 0x80 {
			printable++
		}
	}
	return printable*100/n >= 95
}

func classifyCategory(logicalPath string, fallback string) string {
	if logicalPath == "" {
		return fallback
	}
	clean := strings.TrimPrefix(strings.ToLower(path.Clean(strings.ReplaceAll(logicalPath, "\\", "/"))), "/")
	parts := strings.Split(clean, "/")
	if len(parts) >= 2 && (parts[0] == "resource" || parts[0] == "icon") {
		if parts[0] == "icon" && len(parts) >= 2 {
			if len(parts) >= 3 {
				return "icon_" + sanitizeSegment(parts[1])
			}
			return "icon"
		}
		return sanitizeSegment(parts[1])
	}
	if len(parts) >= 1 && parts[0] != "" {
		return sanitizeSegment(parts[0])
	}
	return fallback
}

func sanitizeSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	res := strings.Trim(b.String(), "._")
	if res == "" {
		return "unnamed"
	}
	return res
}
