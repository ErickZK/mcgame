// Package main provides asset-pack, a Go tool that builds browser-optimized
// maps, real portal graphs, A* collision bitmasks (from MAP_HIT_NEW.swf),
// directional PixiJS v8 spritesheets (stand/walk across 5 base + 3 mirrored
// directions), and UI assets in web-client/public/assets/.
package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type PortalEntry struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	DestMapID int    `json:"destMapId"`
	DestX     int    `json:"destX"`
	DestY     int    `json:"destY"`
}

type MapCatalogEntry struct {
	ID           int           `json:"id"`
	Name         string        `json:"name"`
	Width        int           `json:"width"`
	Height       int           `json:"height"`
	SafeX        int           `json:"safeX"`
	SafeY        int           `json:"safeY"`
	ResCode      int64         `json:"resCode"`
	Image        string        `json:"image"`
	CollisionBin string        `json:"collisionBin,omitempty"`
	GridStep     int           `json:"gridStep,omitempty"`
	Portals      []PortalEntry `json:"portals,omitempty"`
}

type SpriteCatalogEntry struct {
	Key         string   `json:"key"`
	ResCode     int64    `json:"resCode,omitempty"`
	Category    string   `json:"category"`
	SheetJSON   string   `json:"sheetJson"`
	SheetImage  string   `json:"sheetImage"`
	FrameCount  int      `json:"frameCount"`
	FrameWidth  int      `json:"frameWidth"`
	FrameHeight int      `json:"frameHeight"`
	Animations  []string `json:"animations,omitempty"`
}

var (
	reMapRow   = regexp.MustCompile(`\(([0-9]+),\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*'[^']*',\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*([0-9]+),\s*'[^']*',\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*'([^']*)',\s*[^,]+,\s*'[^']*',\s*'[^']*',\s*([0-9]+),\s*'[^']*',\s*[^,]+,\s*[^,]+,\s*([0-9]+),\s*([0-9]+),\s*[^,]+,\s*[^,]+,\s*([0-9]+)\)`)
	reGateRow  = regexp.MustCompile(`\(([0-9]+),\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*'([^']*)',\s*[^,]+,\s*[^,]+,\s*[^,]+,\s*([0-9]+),\s*([0-9]+),\s*([0-9]+),\s*([0-9]+),\s*([0-9]+)\)`)
	reNPCRes   = regexp.MustCompile(`\b(2060\d{9}|2050\d{9})\b`)
)

func main() {
	outDir := flag.String("out", filepath.Join("web-client", "public", "assets"), "output assets root")
	resDir := flag.String("res", filepath.Join("frontend", "s", "res"), "raw hashed resource folder")
	extractedDir := flag.String("extracted", "extracted", "extracted assets folder")
	seedsDir := flag.String("seeds", filepath.Join("supabase", "seeds"), "database seeds folder")
	maxNPCs := flag.Int("max-npcs", 160, "maximum NPC spritesheets to pack")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir out: %v\n", err)
		os.Exit(1)
	}

	portalsByMap := parsePortals(filepath.Join(*seedsDir, "data_tbl_sceneitem_instance.sql"))
	maps, collisionCount := packMaps(
		filepath.Join(*seedsDir, "data_tbl_map.sql"),
		*resDir,
		filepath.Join(*outDir, "maps"),
		portalsByMap,
	)

	priorityKeys := collectSeedCreatureKeys(filepath.Join(*seedsDir, "data_tbl_npc.sql"), filepath.Join(*seedsDir, "data_tbl_class.sql"))
	sprites := packSprites(*extractedDir, filepath.Join(*outDir, "sprites"), priorityKeys, *maxNPCs)
	uiCount := packUI(*extractedDir, filepath.Join(*outDir, "ui"))

	fmt.Printf("Asset pack v2 complete -> %s\n", *outDir)
	fmt.Printf("  Maps exported        : %d\n", len(maps))
	fmt.Printf("  Collision grids built: %d\n", collisionCount)
	fmt.Printf("  Portal maps linked   : %d\n", len(portalsByMap))
	fmt.Printf("  Spritesheets packed  : %d\n", len(sprites))
	fmt.Printf("  UI assets copied     : %d\n", uiCount)
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

type rawGate struct {
	id    int
	name  string
	mapID int
	from  int
	x     int
	y     int
	tid   int
}

func parsePortals(seedPath string) map[int][]PortalEntry {
	raw, err := os.ReadFile(seedPath)
	if err != nil {
		return map[int][]PortalEntry{}
	}
	byID := make(map[int]rawGate)
	for _, m := range reGateRow.FindAllStringSubmatch(string(raw), -1) {
		if len(m) < 8 {
			continue
		}
		tid, _ := strconv.Atoi(m[7])
		if tid != 38 {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		mapID, _ := strconv.Atoi(m[3])
		fromID, _ := strconv.Atoi(m[4])
		x, _ := strconv.Atoi(m[5])
		y, _ := strconv.Atoi(m[6])
		byID[id] = rawGate{
			id:    id,
			name:  m[2],
			mapID: mapID,
			from:  fromID,
			x:     x,
			y:     y,
			tid:   tid,
		}
	}

	out := make(map[int][]PortalEntry)
	for _, g := range byID {
		if g.from <= 0 {
			continue
		}
		dest, ok := byID[g.from]
		if !ok || dest.mapID <= 0 {
			continue
		}
		out[g.mapID] = append(out[g.mapID], PortalEntry{
			ID:        g.id,
			Name:      g.name,
			X:         g.x,
			Y:         g.y,
			DestMapID: dest.mapID,
			DestX:     dest.x,
			DestY:     dest.y,
		})
	}
	for mid := range out {
		sort.Slice(out[mid], func(i, j int) bool { return out[mid][i].ID < out[mid][j].ID })
	}
	return out
}

func packMaps(seedPath string, resDir string, mapsOut string, portalsByMap map[int][]PortalEntry) ([]MapCatalogEntry, int) {
	_ = os.MkdirAll(mapsOut, 0o755)
	raw, err := os.ReadFile(seedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not read %s: %v\n", seedPath, err)
		return nil, 0
	}

	matches := reMapRow.FindAllStringSubmatch(string(raw), -1)
	var catalog []MapCatalogEntry
	collisionCount := 0

	for _, m := range matches {
		if len(m) < 8 {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		height, _ := strconv.Atoi(m[2])
		name := m[3]
		resCode, _ := strconv.ParseInt(m[4], 10, 64)
		safeX, _ := strconv.Atoi(m[5])
		safeY, _ := strconv.Atoi(m[6])
		width, _ := strconv.Atoi(m[7])

		folderNum := resCode % 1000000
		logical := fmt.Sprintf("resource/scene/SCENE_FOLDER_%06d/MAP_0_0.jpg", folderNum)
		src := filepath.Join(resDir, md5Hex(logical))
		data, readErr := os.ReadFile(src)
		if readErr != nil || len(data) < 4 {
			continue
		}

		fileName := fmt.Sprintf("map_%d.jpg", id)
		if err := os.WriteFile(filepath.Join(mapsOut, fileName), data, 0o644); err != nil {
			continue
		}

		entry := MapCatalogEntry{
			ID:      id,
			Name:    name,
			Width:   width,
			Height:  height,
			SafeX:   safeX,
			SafeY:   safeY,
			ResCode: resCode,
			Image:   "/assets/maps/" + fileName,
			Portals: portalsByMap[id],
		}

		hitLogical := fmt.Sprintf("resource/scene/SCENE_FOLDER_%06d/MAP_HIT_NEW.swf", folderNum)
		hitSrc := filepath.Join(resDir, md5Hex(hitLogical))
		if gridBytes, ok := buildCollisionGridFromSWF(hitSrc, width, height, collisionStepPx); ok {
			hitFile := fmt.Sprintf("hit_%d.bin", id)
			if os.WriteFile(filepath.Join(mapsOut, hitFile), gridBytes, 0o644) == nil {
				entry.CollisionBin = "/assets/maps/" + hitFile
				entry.GridStep = collisionStepPx
				collisionCount++
			}
		}

		catalog = append(catalog, entry)
	}

	sort.Slice(catalog, func(i, j int) bool { return catalog[i].ID < catalog[j].ID })
	if out, err := json.MarshalIndent(catalog, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(mapsOut, "index.json"), out, 0o644)
	}
	return catalog, collisionCount
}

func collectSeedCreatureKeys(paths ...string) map[string]int64 {
	result := make(map[string]int64)
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, codeStr := range reNPCRes.FindAllString(string(raw), -1) {
			code, err := strconv.ParseInt(codeStr, 10, 64)
			if err != nil {
				continue
			}
			if key, ok := creatureKeyFromResCode(code); ok {
				result[key] = code
			}
		}
	}
	return result
}

func packUI(extractedDir string, uiOut string) int {
	_ = os.MkdirAll(uiOut, 0o755)
	candidates := []string{
		filepath.Join(extractedDir, "images", "ui"),
		filepath.Join(extractedDir, "images", "icon_ui"),
	}
	copied := 0
	for _, dir := range candidates {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			lower := strings.ToLower(e.Name())
			if strings.Contains(lower, "starrysky") ||
				strings.Contains(lower, "worldmap") ||
				strings.Contains(lower, "soulbg") ||
				strings.Contains(lower, "magicfarm") {
				data, rErr := os.ReadFile(filepath.Join(dir, e.Name()))
				if rErr == nil {
					cleanName := normalizeUIName(e.Name())
					if os.WriteFile(filepath.Join(uiOut, cleanName), data, 0o644) == nil {
						copied++
					}
				}
			}
		}
	}
	return copied
}

func normalizeUIName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if idx := strings.LastIndex(base, "_"); idx > 0 && len(base)-idx-1 == 8 {
		base = base[:idx]
	}
	return strings.ToLower(base) + ext
}
