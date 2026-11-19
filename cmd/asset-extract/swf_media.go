// This file extracts browser-ready media assets from parsed SWF tags:
// lossless and JPEG bitmaps (including RGBA alpha compositing for JPEG3/4
// and un-premultiplied alpha for Lossless2), MP3/WAV audio from DefineSound
// and streaming MP3 blocks, and nested DefineBinaryData payloads.
package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
)

type SWFExtractStats struct {
	Images     int
	Sounds     int
	Binaries   int
	NestedSWFs int
	Sprites    int
}

func extractSWFBundle(rawSWF []byte, outDir string, soundsOutDir string, baseLabel string, depth int) (SWFMetadata, SWFExtractStats, []byte, error) {
	var stats SWFExtractStats
	fws, hdr, err := decompressSWF(rawSWF)
	if err != nil {
		return SWFMetadata{}, stats, nil, err
	}
	tags, err := parseSWFTags(fws)
	if err != nil {
		return SWFMetadata{}, stats, fws, err
	}

	meta := SWFMetadata{
		Header:    hdr,
		Symbols:   make(map[uint16]string),
		TagCounts: make(map[string]int),
	}

	var jpegTables []byte
	for _, t := range tags {
		meta.TagCounts[tagName(t.Code)]++
		switch t.Code {
		case 56, 76:
			parseSymbolMap(t.Body, meta.Symbols)
		case 8:
			jpegTables = t.Body
		case 2, 22, 32, 83:
			meta.HasVectorShape = true
		case 12, 59, 82:
			meta.HasScripts = true
		case 10, 48, 75:
			meta.HasFonts = true
		}
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return meta, stats, fws, err
	}

	mainFrame := 1
	var streamMP3 bytes.Buffer
	streamIsMP3 := false

	for _, t := range tags {
		switch t.Code {
		case 1:
			mainFrame++
		case 43:
			end := bytes.IndexByte(t.Body, 0)
			if end > 0 {
				if meta.MainLabels == nil {
					meta.MainLabels = make(map[int]string)
				}
				meta.MainLabels[mainFrame] = string(t.Body[:end])
			}
		case 39:
			if sm, ok := parseSpriteTag(t.Body, meta.Symbols); ok {
				meta.Sprites = append(meta.Sprites, sm)
				stats.Sprites++
			}
		case 6, 21, 35, 90:
			if len(t.Body) < 2 {
				continue
			}
			charID := binary.LittleEndian.Uint16(t.Body[0:2])
			rel, ok := writeSWFImageJPEG(t.Code, t.Body, jpegTables, charID, meta.Symbols[charID], outDir)
			if ok {
				meta.ExtractedFiles = append(meta.ExtractedFiles, rel)
				stats.Images++
			}
		case 20, 36:
			if len(t.Body) < 7 {
				continue
			}
			charID := binary.LittleEndian.Uint16(t.Body[0:2])
			rel, ok := writeSWFImageLossless(t.Code == 36, t.Body, charID, meta.Symbols[charID], outDir)
			if ok {
				meta.ExtractedFiles = append(meta.ExtractedFiles, rel)
				stats.Images++
			}
		case 14:
			if len(t.Body) < 7 {
				continue
			}
			charID := binary.LittleEndian.Uint16(t.Body[0:2])
			rel, ok := writeSWFSound(t.Body, charID, meta.Symbols[charID], outDir, soundsOutDir, baseLabel)
			if ok {
				meta.ExtractedFiles = append(meta.ExtractedFiles, rel)
				stats.Sounds++
			}
		case 18, 45:
			if len(t.Body) >= 2 {
				streamFormat := (t.Body[1] >> 4) & 0x0f
				streamIsMP3 = streamFormat == 2
			}
		case 19:
			if streamIsMP3 && len(t.Body) > 4 {
				streamMP3.Write(t.Body[4:])
			}
		case 87:
			if len(t.Body) < 6 {
				continue
			}
			charID := binary.LittleEndian.Uint16(t.Body[0:2])
			payload := t.Body[6:]
			rel, nested := writeSWFBinaryData(payload, charID, meta.Symbols[charID], outDir, soundsOutDir, baseLabel, depth)
			if rel != "" {
				meta.ExtractedFiles = append(meta.ExtractedFiles, rel)
				stats.Binaries++
				stats.Images += nested.Images
				stats.Sounds += nested.Sounds
				stats.Sprites += nested.Sprites
				stats.NestedSWFs += nested.NestedSWFs
			}
		}
	}

	if streamMP3.Len() > 0 {
		name := "stream_audio.mp3"
		_ = os.WriteFile(filepath.Join(outDir, name), streamMP3.Bytes(), 0o644)
		if soundsOutDir != "" {
			_ = os.MkdirAll(soundsOutDir, 0o755)
			_ = os.WriteFile(filepath.Join(soundsOutDir, sanitizeSegment(baseLabel)+"_stream.mp3"), streamMP3.Bytes(), 0o644)
		}
		meta.ExtractedFiles = append(meta.ExtractedFiles, name)
		stats.Sounds++
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err == nil {
		_ = os.WriteFile(filepath.Join(outDir, "meta.json"), metaBytes, 0o644)
	}
	return meta, stats, fws, nil
}

func assetFilename(prefix string, charID uint16, symbol string, ext string) string {
	if symbol != "" {
		clean := sanitizeSegment(symbol)
		if len(clean) > 80 {
			clean = clean[:80]
		}
		return fmt.Sprintf("%s_%04d_%s%s", prefix, charID, clean, ext)
	}
	return fmt.Sprintf("%s_%04d%s", prefix, charID, ext)
}

func stripLegacyJPEGHeader(data []byte) []byte {
	if len(data) >= 4 && data[0] == 0xff && data[1] == 0xd9 && data[2] == 0xff && data[3] == 0xd8 {
		return data[4:]
	}
	return data
}

func mergeJPEGTables(tables []byte, img []byte) []byte {
	t := stripLegacyJPEGHeader(tables)
	d := stripLegacyJPEGHeader(img)
	if len(t) < 4 || len(d) < 2 {
		return d
	}
	if len(t) >= 2 && t[len(t)-2] == 0xff && t[len(t)-1] == 0xd9 {
		t = t[:len(t)-2]
	}
	if len(d) >= 2 && d[0] == 0xff && d[1] == 0xd8 {
		d = d[2:]
	}
	out := make([]byte, 0, len(t)+len(d))
	out = append(out, t...)
	out = append(out, d...)
	return out
}

func writeSWFImageJPEG(code uint16, body []byte, jpegTables []byte, charID uint16, symbol string, outDir string) (string, bool) {
	var imgBytes []byte
	var alphaCompressed []byte

	switch code {
	case 6:
		imgBytes = mergeJPEGTables(jpegTables, body[2:])
	case 21:
		imgBytes = stripLegacyJPEGHeader(body[2:])
	case 35:
		if len(body) < 6 {
			return "", false
		}
		alphaOff := int(binary.LittleEndian.Uint32(body[2:6]))
		if 6+alphaOff > len(body) {
			return "", false
		}
		imgBytes = stripLegacyJPEGHeader(body[6 : 6+alphaOff])
		alphaCompressed = body[6+alphaOff:]
	case 90:
		if len(body) < 8 {
			return "", false
		}
		alphaOff := int(binary.LittleEndian.Uint32(body[2:6]))
		if 8+alphaOff > len(body) {
			return "", false
		}
		imgBytes = stripLegacyJPEGHeader(body[8 : 8+alphaOff])
		alphaCompressed = body[8+alphaOff:]
	default:
		return "", false
	}

	if len(imgBytes) >= 8 && bytes.Equal(imgBytes[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
		name := assetFilename("img", charID, symbol, ".png")
		if err := os.WriteFile(filepath.Join(outDir, name), imgBytes, 0o644); err == nil {
			return name, true
		}
		return "", false
	}
	if len(imgBytes) >= 6 && (bytes.HasPrefix(imgBytes, []byte("GIF87a")) || bytes.HasPrefix(imgBytes, []byte("GIF89a"))) {
		name := assetFilename("img", charID, symbol, ".gif")
		if err := os.WriteFile(filepath.Join(outDir, name), imgBytes, 0o644); err == nil {
			return name, true
		}
		return "", false
	}

	if len(alphaCompressed) > 0 {
		zr, err := zlib.NewReader(bytes.NewReader(alphaCompressed))
		if err == nil {
			alphaRaw, aErr := io.ReadAll(zr)
			zr.Close()
			if aErr == nil {
				decoded, jErr := jpeg.Decode(bytes.NewReader(imgBytes))
				if jErr == nil {
					bounds := decoded.Bounds()
					w, h := bounds.Dx(), bounds.Dy()
					if len(alphaRaw) >= w*h {
						nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
						for y := 0; y < h; y++ {
							for x := 0; x < w; x++ {
								r, g, b, _ := decoded.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
								a := alphaRaw[y*w+x]
								nrgba.SetNRGBA(x, y, color.NRGBA{
									R: uint8(r >> 8),
									G: uint8(g >> 8),
									B: uint8(b >> 8),
									A: a,
								})
							}
						}
						name := assetFilename("img", charID, symbol, ".png")
						f, fErr := os.Create(filepath.Join(outDir, name))
						if fErr == nil {
							encErr := png.Encode(f, nrgba)
							f.Close()
							if encErr == nil {
								return name, true
							}
						}
					}
				}
			}
		}
	}

	name := assetFilename("img", charID, symbol, ".jpg")
	if err := os.WriteFile(filepath.Join(outDir, name), imgBytes, 0o644); err == nil {
		return name, true
	}
	return "", false
}

func writeSWFImageLossless(hasAlpha bool, body []byte, charID uint16, symbol string, outDir string) (string, bool) {
	if len(body) < 8 {
		return "", false
	}
	format := body[2]
	w := int(binary.LittleEndian.Uint16(body[3:5]))
	h := int(binary.LittleEndian.Uint16(body[5:7]))
	if w == 0 || h == 0 || w > 16384 || h > 16384 {
		return "", false
	}

	off := 7
	colorTableSize := 0
	if format == 3 {
		if len(body) < 9 {
			return "", false
		}
		colorTableSize = int(body[7]) + 1
		off = 8
	}

	zr, err := zlib.NewReader(bytes.NewReader(body[off:]))
	if err != nil {
		return "", false
	}
	raw, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		return "", false
	}

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	switch format {
	case 3:
		entryBytes := 3
		if hasAlpha {
			entryBytes = 4
		}
		paletteLen := colorTableSize * entryBytes
		if len(raw) < paletteLen {
			return "", false
		}
		palette := make([]color.NRGBA, colorTableSize)
		for i := 0; i < colorTableSize; i++ {
			base := i * entryBytes
			r, g, b := raw[base], raw[base+1], raw[base+2]
			a := uint8(255)
			if hasAlpha {
				a = raw[base+3]
				r, g, b = unpremultiply(r, g, b, a)
			}
			palette[i] = color.NRGBA{R: r, G: g, B: b, A: a}
		}
		stride := (w + 3) &^ 3
		pix := raw[paletteLen:]
		if len(pix) < stride*h {
			return "", false
		}
		for y := 0; y < h; y++ {
			row := pix[y*stride : y*stride+w]
			for x := 0; x < w; x++ {
				idx := int(row[x])
				if idx < len(palette) {
					img.SetNRGBA(x, y, palette[idx])
				}
			}
		}
	case 4:
		stride := (w*2 + 3) &^ 3
		if len(raw) < stride*h {
			return "", false
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				v := binary.BigEndian.Uint16(raw[y*stride+x*2 : y*stride+x*2+2])
				r := uint8(((v >> 10) & 0x1f) * 255 / 31)
				g := uint8(((v >> 5) & 0x1f) * 255 / 31)
				b := uint8((v & 0x1f) * 255 / 31)
				img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
			}
		}
	case 5:
		if len(raw) < w*h*4 {
			return "", false
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				base := (y*w + x) * 4
				if hasAlpha {
					a, r, g, b := raw[base], raw[base+1], raw[base+2], raw[base+3]
					r, g, b = unpremultiply(r, g, b, a)
					img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
				} else {
					img.SetNRGBA(x, y, color.NRGBA{R: raw[base+1], G: raw[base+2], B: raw[base+3], A: 255})
				}
			}
		}
	default:
		return "", false
	}

	name := assetFilename("img", charID, symbol, ".png")
	f, err := os.Create(filepath.Join(outDir, name))
	if err != nil {
		return "", false
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", false
	}
	return name, true
}

func unpremultiply(r, g, b, a uint8) (uint8, uint8, uint8) {
	if a == 0 {
		return 0, 0, 0
	}
	if a == 255 {
		return r, g, b
	}
	ur := min(255, int(r)*255/int(a))
	ug := min(255, int(g)*255/int(a))
	ub := min(255, int(b)*255/int(a))
	return uint8(ur), uint8(ug), uint8(ub)
}

func writeSWFSound(body []byte, charID uint16, symbol string, outDir string, soundsOutDir string, baseLabel string) (string, bool) {
	flags := body[2]
	format := (flags >> 4) & 0x0f
	rateCode := (flags >> 2) & 0x03
	is16Bit := (flags & 0x02) != 0
	isStereo := (flags & 0x01) != 0
	payload := body[7:]

	rates := [4]uint32{5512, 11025, 22050, 44100}
	sampleRate := rates[rateCode]

	var ext string
	var outBytes []byte

	switch format {
	case 2:
		if len(payload) < 2 {
			return "", false
		}
		ext = ".mp3"
		outBytes = payload[2:]
	case 0, 3:
		ext = ".wav"
		bits := uint16(8)
		if is16Bit {
			bits = 16
		}
		channels := uint16(1)
		if isStereo {
			channels = 2
		}
		outBytes = buildWAV(payload, sampleRate, channels, bits)
	default:
		ext = fmt.Sprintf(".snd%d", format)
		outBytes = payload
	}

	name := assetFilename("snd", charID, symbol, ext)
	if err := os.WriteFile(filepath.Join(outDir, name), outBytes, 0o644); err != nil {
		return "", false
	}
	if soundsOutDir != "" {
		_ = os.MkdirAll(soundsOutDir, 0o755)
		flatName := sanitizeSegment(baseLabel) + "_" + name
		_ = os.WriteFile(filepath.Join(soundsOutDir, flatName), outBytes, 0o644)
	}
	return name, true
}

func buildWAV(pcm []byte, sampleRate uint32, channels uint16, bitsPerSample uint16) []byte {
	byteRate := sampleRate * uint32(channels) * uint32(bitsPerSample) / 8
	blockAlign := channels * bitsPerSample / 8
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+len(pcm)))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, channels)
	_ = binary.Write(&buf, binary.LittleEndian, sampleRate)
	_ = binary.Write(&buf, binary.LittleEndian, byteRate)
	_ = binary.Write(&buf, binary.LittleEndian, blockAlign)
	_ = binary.Write(&buf, binary.LittleEndian, bitsPerSample)
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(pcm)))
	buf.Write(pcm)
	return buf.Bytes()
}

func writeSWFBinaryData(payload []byte, charID uint16, symbol string, outDir string, soundsOutDir string, baseLabel string, depth int) (string, SWFExtractStats) {
	var nested SWFExtractStats
	det := detectAndUnwrap(payload)
	if det.Kind == "swf" && depth < 2 {
		subName := assetFilename("embedded_swf", charID, symbol, "")
		subDir := filepath.Join(outDir, subName)
		_, st, fws, err := extractSWFBundle(det.Data, subDir, soundsOutDir, baseLabel+"_"+subName, depth+1)
		if err == nil {
			if len(fws) > 0 {
				_ = os.WriteFile(filepath.Join(subDir, subName+".swf"), fws, 0o644)
			}
			nested = st
			nested.NestedSWFs++
			return subName + "/", nested
		}
	}
	name := assetFilename("bin", charID, symbol, det.Ext)
	if err := os.WriteFile(filepath.Join(outDir, name), det.Data, 0o644); err != nil {
		return "", nested
	}
	return name, nested
}
