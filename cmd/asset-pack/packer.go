// Directional PixiJS v8 spritesheet builder. Parses main-timeline FrameLabel
// and PlaceObject2/3 tags from decoded creature SWFs to preserve authentic
// direction-action animations ("0-0" stand down through "4-1" walk up) and
// indexes every sprite by both logical key and numeric Flash resCode.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type pixiRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type pixiSize struct {
	W int `json:"w"`
	H int `json:"h"`
}

type pixiFrame struct {
	Frame            pixiRect `json:"frame"`
	Rotated          bool     `json:"rotated"`
	Trimmed          bool     `json:"trimmed"`
	SpriteSourceSize pixiRect `json:"spriteSourceSize"`
	SourceSize       pixiSize `json:"sourceSize"`
}

type pixiMeta struct {
	App    string   `json:"app"`
	Image  string   `json:"image"`
	Format string   `json:"format"`
	Size   pixiSize `json:"size"`
	Scale  string   `json:"scale"`
}

type pixiSheet struct {
	Frames     map[string]pixiFrame `json:"frames"`
	Animations map[string][]string  `json:"animations"`
	Meta       pixiMeta             `json:"meta"`
}

var wantedAnimLabels = map[string]bool{
	"0-0": true, "0-1": true, "0-2": true,
	"1-0": true, "1-1": true, "1-2": true,
	"2-0": true, "2-1": true, "2-2": true,
	"3-0": true, "3-1": true, "3-2": true,
	"4-0": true, "4-1": true, "4-2": true,
}

func creatureKeyFromResCode(code int64) (string, bool) {
	if code < 2000000000000 {
		return "", false
	}
	rem := code % 1000000000000
	w1 := int(rem / 1000000000)
	rem = rem % 1000000000
	w2 := int(rem / 1000000)
	num := rem % 1000000

	var p1 string
	switch w1 {
	case 50:
		p1 = "PC_"
	case 60:
		p1 = "NPC_"
	default:
		return "", false
	}

	w2Map := map[int]string{
		10: "", 70: "MALE_", 80: "FEMALE_", 90: "FUNC_", 100: "CRE_",
		160: "NPC_", 200: "NORMAL_", 260: "FC1_", 270: "FC2_", 280: "FC3_",
		290: "FC4_", 300: "FC5_", 310: "FC6_", 320: "MC1_", 330: "MC2_",
		340: "MC3_", 350: "MC4_", 360: "MC5_", 370: "MC6_",
	}
	p2, ok := w2Map[w2]
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s%s%06d", p1, p2, num), true
}

func resCodeFromCreatureKey(key string) int64 {
	var w1 int64
	rest := key
	switch {
	case strings.HasPrefix(key, "PC_"):
		w1 = 50
		rest = strings.TrimPrefix(key, "PC_")
	case strings.HasPrefix(key, "NPC_"):
		w1 = 60
		rest = strings.TrimPrefix(key, "NPC_")
	default:
		return 0
	}

	prefixes := []struct {
		p  string
		id int64
	}{
		{"MALE_", 70}, {"FEMALE_", 80}, {"FUNC_", 90}, {"CRE_", 100},
		{"NPC_", 160}, {"NORMAL_", 200}, {"FC1_", 260}, {"FC2_", 270},
		{"FC3_", 280}, {"FC4_", 290}, {"FC5_", 300}, {"FC6_", 310},
		{"MC1_", 320}, {"MC2_", 330}, {"MC3_", 340}, {"MC4_", 350},
		{"MC5_", 360}, {"MC6_", 370},
	}
	w2 := int64(10)
	for _, pref := range prefixes {
		if strings.HasPrefix(rest, pref.p) {
			w2 = pref.id
			rest = strings.TrimPrefix(rest, pref.p)
			break
		}
	}
	num, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || num < 0 || num >= 1000000 {
		return 0
	}
	return 2*1000000000000 + w1*1000000000 + w2*1000000 + num
}

func packSprites(extractedDir string, spritesOut string, priorityKeys map[string]int64, maxNPCs int) []SpriteCatalogEntry {
	_ = os.MkdirAll(spritesOut, 0o755)
	creatureDir := filepath.Join(extractedDir, "sprites", "creature")
	swfDir := filepath.Join(extractedDir, "swf-decoded", "creature")
	entries, err := os.ReadDir(creatureDir)
	if err != nil {
		return nil
	}

	var pcDirs []string
	var prioNPC []string
	var otherNPC []string

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.Contains(name, ".swf_NEW") {
			continue
		}
		cleanKey := stripHashSuffix(name)
		if strings.HasPrefix(name, "PC_") {
			pcDirs = append(pcDirs, name)
		} else if strings.HasPrefix(name, "NPC_") {
			if _, ok := priorityKeys[cleanKey]; ok {
				prioNPC = append(prioNPC, name)
			} else {
				otherNPC = append(otherNPC, name)
			}
		}
	}
	sort.Strings(pcDirs)
	sort.Strings(prioNPC)
	sort.Strings(otherNPC)

	npcDirs := append(prioNPC, otherNPC...)
	if maxNPCs > 0 && len(npcDirs) > maxNPCs {
		npcDirs = npcDirs[:maxNPCs]
	}

	var catalog []SpriteCatalogEntry
	for _, dirName := range pcDirs {
		if entry, ok := buildDirectionalAtlas(filepath.Join(creatureDir, dirName), filepath.Join(swfDir, dirName+".swf"), dirName, "pc", spritesOut); ok {
			catalog = append(catalog, entry)
		}
	}
	for _, dirName := range npcDirs {
		if entry, ok := buildDirectionalAtlas(filepath.Join(creatureDir, dirName), filepath.Join(swfDir, dirName+".swf"), dirName, "npc", spritesOut); ok {
			catalog = append(catalog, entry)
		}
	}

	if out, err := json.MarshalIndent(catalog, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(spritesOut, "index.json"), out, 0o644)
	}
	return catalog
}

func extractSWFTimelineAnimations(swfPath string, availableBitmaps map[uint16]string) map[string][]string {
	raw, err := os.ReadFile(swfPath)
	if err != nil {
		return nil
	}
	fws, ok := decompressSWFToFWS(raw)
	if !ok {
		return nil
	}
	off := swfTagStreamOffset(fws)
	if off < 0 {
		return nil
	}

	anims := make(map[string][]string)
	currentLabel := ""
	var frameFiles []string
	var lastAdded string

	flushFrame := func() {
		if currentLabel == "" || !wantedAnimLabels[currentLabel] || len(frameFiles) == 0 {
			frameFiles = frameFiles[:0]
			return
		}
		chosen := frameFiles[len(frameFiles)-1]
		if chosen != lastAdded && len(anims[currentLabel]) < 12 {
			anims[currentLabel] = append(anims[currentLabel], chosen)
			lastAdded = chosen
		}
		frameFiles = frameFiles[:0]
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

		switch code {
		case 0:
			flushFrame()
			return anims
		case 1:
			flushFrame()
		case 43:
			end := bytes.IndexByte(body, 0)
			if end > 0 {
				currentLabel = string(body[:end])
				lastAdded = ""
			}
		case 26, 70:
			charID := parsePlaceCharID(code, body)
			if charID > 0 {
				if fn, ok := availableBitmaps[charID]; ok {
					frameFiles = append(frameFiles, fn)
				} else if charID > 1 {
					if fn, ok := availableBitmaps[charID-1]; ok {
						frameFiles = append(frameFiles, fn)
					}
				}
			}
		}
	}
	return anims
}

func parsePlaceCharID(code uint16, body []byte) uint16 {
	minLen := 3
	off := 1
	if code == 70 {
		minLen = 4
		off = 2
	}
	if len(body) < minLen {
		return 0
	}
	flags := body[0]
	if (flags & 0x02) == 0 {
		return 0
	}
	off += 2
	if code == 70 && len(body) > 1 && (body[1]&0x10) != 0 {
		end := bytes.IndexByte(body[off:], 0)
		if end < 0 {
			return 0
		}
		off += end + 1
	}
	if off+2 > len(body) {
		return 0
	}
	return binary.LittleEndian.Uint16(body[off : off+2])
}

func buildDirectionalAtlas(srcDir string, swfPath string, dirName string, category string, spritesOut string) (SpriteCatalogEntry, bool) {
	files, err := os.ReadDir(srcDir)
	if err != nil {
		return SpriteCatalogEntry{}, false
	}

	availableByID := make(map[uint16]string)
	var allPNGs []string
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".png") {
			continue
		}
		allPNGs = append(allPNGs, f.Name())
		if strings.HasPrefix(f.Name(), "img_") {
			parts := strings.Split(strings.TrimSuffix(f.Name(), ".png"), "_")
			if len(parts) >= 2 {
				if id, err := strconv.Atoi(parts[1]); err == nil && id > 0 && id < 65535 {
					availableByID[uint16(id)] = f.Name()
				}
			}
		}
	}
	sort.Strings(allPNGs)
	if len(allPNGs) == 0 {
		return SpriteCatalogEntry{}, false
	}

	timelineAnims := extractSWFTimelineAnimations(swfPath, availableByID)
	selectedSet := make(map[string]struct{})
	var selectedFiles []string
	addFile := func(fn string) {
		if _, exists := selectedSet[fn]; !exists {
			selectedSet[fn] = struct{}{}
			selectedFiles = append(selectedFiles, fn)
		}
	}

	var animNames []string
	for label := range timelineAnims {
		if len(timelineAnims[label]) > 0 {
			animNames = append(animNames, label)
		}
	}
	sort.Strings(animNames)
	for _, label := range animNames {
		for _, fn := range timelineAnims[label] {
			addFile(fn)
		}
	}

	if len(selectedFiles) == 0 {
		limit := min(16, len(allPNGs))
		for i := 0; i < limit; i++ {
			addFile(allPNGs[i])
		}
	}
	if len(selectedFiles) > 64 {
		selectedFiles = selectedFiles[:64]
	}

	decoded := make(map[string]image.Image, len(selectedFiles))
	cropRects := make(map[string]image.Rectangle, len(selectedFiles))
	var validFiles []string
	maxW, maxH := 0, 0
	for _, fn := range selectedFiles {
		f, err := os.Open(filepath.Join(srcDir, fn))
		if err != nil {
			continue
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			continue
		}
		b := opaqueBounds(img)
		if b.Dx() < 8 || b.Dy() < 8 || b.Dx() > 512 || b.Dy() > 512 {
			continue
		}
		if b.Dx() > maxW {
			maxW = b.Dx()
		}
		if b.Dy() > maxH {
			maxH = b.Dy()
		}
		decoded[fn] = img
		cropRects[fn] = b
		validFiles = append(validFiles, fn)
	}
	if len(validFiles) == 0 || maxW == 0 || maxH == 0 {
		return SpriteCatalogEntry{}, false
	}

	cols := int(math.Ceil(math.Sqrt(float64(len(validFiles)))))
	rows := int(math.Ceil(float64(len(validFiles)) / float64(cols)))
	atlasW := cols * maxW
	atlasH := rows * maxH
	if atlasW > 4096 || atlasH > 4096 {
		return SpriteCatalogEntry{}, false
	}

	atlas := image.NewNRGBA(image.Rect(0, 0, atlasW, atlasH))
	cleanKey := stripHashSuffix(dirName)
	imgFile := cleanKey + ".png"
	jsonFile := cleanKey + ".json"

	fileToKey := make(map[string]string, len(validFiles))
	sheet := pixiSheet{
		Frames:     make(map[string]pixiFrame, len(validFiles)),
		Animations: make(map[string][]string),
		Meta: pixiMeta{
			App:    "mcgame-asset-pack-v3",
			Image:  imgFile + "?v=b3",
			Format: "RGBA8888",
			Size:   pixiSize{W: atlasW, H: atlasH},
			Scale:  "1",
		},
	}

	for i, fn := range validFiles {
		src := decoded[fn]
		c := i % cols
		r := i / cols
		b := cropRects[fn]
		dx := c*maxW + (maxW-b.Dx())/2
		dy := r*maxH + (maxH - b.Dy())
		draw.Draw(atlas, image.Rect(dx, dy, dx+b.Dx(), dy+b.Dy()), src, b.Min, draw.Src)

		frameKey := fmt.Sprintf("%s_%02d", cleanKey, i)
		fileToKey[fn] = frameKey
		sheet.Frames[frameKey] = pixiFrame{
			Frame:            pixiRect{X: c * maxW, Y: r * maxH, W: maxW, H: maxH},
			Rotated:          false,
			Trimmed:          false,
			SpriteSourceSize: pixiRect{X: 0, Y: 0, W: maxW, H: maxH},
			SourceSize:       pixiSize{W: maxW, H: maxH},
		}
	}

	var exportedAnims []string
	for _, label := range animNames {
		var keys []string
		for _, fn := range timelineAnims[label] {
			if k, ok := fileToKey[fn]; ok {
				keys = append(keys, k)
			}
		}
		if len(keys) > 0 {
			sheet.Animations[label] = keys
			exportedAnims = append(exportedAnims, label)
		}
	}

	if idleFrames, ok := sheet.Animations["0-0"]; ok && len(idleFrames) > 0 {
		sheet.Animations["idle"] = idleFrames
	} else {
		var fallback []string
		for i := 0; i < min(8, len(validFiles)); i++ {
			fallback = append(fallback, fileToKey[validFiles[i]])
		}
		sheet.Animations["idle"] = fallback
	}

	outPNG, err := os.Create(filepath.Join(spritesOut, imgFile))
	if err != nil {
		return SpriteCatalogEntry{}, false
	}
	if err := png.Encode(outPNG, atlas); err != nil {
		outPNG.Close()
		return SpriteCatalogEntry{}, false
	}
	outPNG.Close()

	jsonBytes, err := json.MarshalIndent(sheet, "", "  ")
	if err != nil {
		return SpriteCatalogEntry{}, false
	}
	if err := os.WriteFile(filepath.Join(spritesOut, jsonFile), jsonBytes, 0o644); err != nil {
		return SpriteCatalogEntry{}, false
	}

	return SpriteCatalogEntry{
		Key:         cleanKey,
		ResCode:     resCodeFromCreatureKey(cleanKey),
		Category:    category,
		SheetJSON:   "/assets/sprites/" + jsonFile,
		SheetImage:  "/assets/sprites/" + imgFile,
		FrameCount:  len(validFiles),
		FrameWidth:  maxW,
		FrameHeight: maxH,
		Animations:  exportedAnims,
	}, true
}

func opaqueBounds(img image.Image) image.Rectangle {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	found := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a > 0x1000 {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x+1 > maxX {
					maxX = x + 1
				}
				if y+1 > maxY {
					maxY = y + 1
				}
				found = true
			}
		}
	}
	if !found {
		return b
	}
	return image.Rect(minX, minY, maxX, maxY)
}

func stripHashSuffix(name string) string {
	if idx := strings.LastIndex(name, "_"); idx > 0 && len(name)-idx-1 == 8 {
		return name[:idx]
	}
	return name
}
