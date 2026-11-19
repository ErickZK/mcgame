// This file builds a reverse lookup table from MD5 hashes in frontend/s/res/
// back to original logical paths by implementing UrlUtil.getUrl and
// ResManager.hash against database seeds, decompiled ActionScript sources,
// profile XMLs, and sequential game asset patterns.
package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	urlWordExt = int64(1000000000000)
	urlWord1   = int64(1000000000)
	urlWord2   = int64(1000000)
)

var resURLFolder = map[int]string{
	10:  "",
	15:  "scene/",
	20:  "item/",
	30:  "item/",
	40:  "tile/",
	50:  "creature/",
	60:  "creature/",
	70:  "equip/",
	80:  "effect/",
	90:  "sound/",
	100: "bullet/",
	110: "emotion/",
	120: "state/",
	130: "ui/",
	140: "skill/",
}

var resURLWord1 = map[int]string{
	10:  "",
	15:  "SCENE_",
	20:  "SITEM_",
	30:  "ITEM_",
	40:  "TILE_",
	50:  "PC_",
	60:  "NPC_",
	70:  "EQUIP_",
	80:  "EFF_",
	90:  "WAV_",
	100: "BLT_",
	110: "EM_",
	120: "ST_",
	130: "UI_",
	140: "SKILL_",
}

var resURLWord2 = map[int]string{
	10:  "",
	15:  "FOLDER_",
	16:  "FLOWER_",
	20:  "TREE_",
	30:  "HOUSE_",
	40:  "SPAWN_",
	50:  "DESERT_",
	60:  "FOREST_",
	70:  "MALE_",
	80:  "FEMALE_",
	90:  "FUNC_",
	100: "CRE_",
	110: "WEAPON_",
	120: "SCENE_",
	130: "PLAYER_",
	140: "SIDE_",
	150: "SCREEN_",
	160: "NPC_",
	170: "UI_",
	180: "EFF_",
	190: "FIRE_",
	200: "NORMAL_",
	210: "LOCAL_",
	220: "GLOBAL_",
	230: "STAGE_",
	240: "BATTLE_",
	250: "LOADING_",
	260: "FC1_",
	270: "FC2_",
	280: "FC3_",
	290: "FC4_",
	300: "FC5_",
	310: "FC6_",
	320: "MC1_",
	330: "MC2_",
	340: "MC3_",
	350: "MC4_",
	360: "MC5_",
	370: "MC6_",
	380: "FLY_",
	390: "WING_",
}

var resURLExt = map[int]string{
	1: "",
	2: ".swf",
	3: ".jpg",
	4: ".png",
	5: ".wav",
	6: ".mp3",
	7: ".ogg",
}

var (
	reCode13   = regexp.MustCompile(`\b([1-7]\d{12})\b`)
	reQuoted   = regexp.MustCompile(`["']([^"'\r\n]{3,180})["']`)
	rePathLike = regexp.MustCompile(`(?i)\.(swf|png|jpg|jpeg|gif|mp3|wav|ogg|xml|txt|html)$`)
)

func decodeResCode(code int64) (string, bool) {
	if code < urlWordExt {
		return "", false
	}
	extIdx := int(code / urlWordExt)
	rem := code % urlWordExt
	w1Idx := int(rem / urlWord1)
	rem = rem % urlWord1
	w2Idx := int(rem / urlWord2)

	ext, okExt := resURLExt[extIdx]
	folder, okF := resURLFolder[w1Idx]
	w1, okW1 := resURLWord1[w1Idx]
	w2, okW2 := resURLWord2[w2Idx]
	if !okExt || !okF || !okW1 || !okW2 {
		return "", false
	}

	numStr := fmt.Sprintf("%06d", rem%urlWord2)
	return folder + w1 + w2 + numStr + ext, true
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

type PathResolver struct {
	targets  map[string]struct{}
	resolved map[string]string
}

func newPathResolver(targetHashes []string) *PathResolver {
	targets := make(map[string]struct{}, len(targetHashes))
	for _, h := range targetHashes {
		targets[strings.ToLower(h)] = struct{}{}
	}
	return &PathResolver{
		targets:  targets,
		resolved: make(map[string]string, len(targetHashes)),
	}
}

func (r *PathResolver) register(candidate string) {
	if candidate == "" {
		return
	}
	h := md5Hex(candidate)
	if _, needed := r.targets[h]; needed {
		if _, already := r.resolved[h]; !already {
			r.resolved[h] = candidate
		}
	}
}

func (r *PathResolver) registerVariants(rel string) {
	r.register(rel)
	resPath := "resource/" + rel
	iconPath := "icon/" + rel
	r.register(resPath)
	r.register(iconPath)
	r.register(resPath + "_NEW.swf")
	r.register(iconPath + "_NEW.swf")
	if strings.HasSuffix(strings.ToLower(rel), ".swf") {
		base := rel[:len(rel)-4]
		r.register("resource/" + base + "_NEW.swf")
		r.register("icon/" + base + "_NEW.swf")
	}
}

func (r *PathResolver) scanDirectory(root string, exts map[string]bool) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !exts[ext] {
			return nil
		}
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		r.scanText(string(data))
		return nil
	})
}

func (r *PathResolver) scanText(text string) {
	for _, m := range reCode13.FindAllString(text, -1) {
		code, err := strconv.ParseInt(m, 10, 64)
		if err != nil {
			continue
		}
		if rel, ok := decodeResCode(code); ok {
			r.registerVariants(rel)
		}
	}

	for _, match := range reQuoted.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		s := strings.TrimSpace(match[1])
		if strings.HasPrefix(s, "resource/") || strings.HasPrefix(s, "icon/") || rePathLike.MatchString(s) {
			r.register(s)
			if !strings.HasPrefix(s, "resource/") && !strings.HasPrefix(s, "icon/") {
				r.register("resource/" + s)
				r.register("icon/" + s)
				r.register("resource/ui/" + s)
				r.register("resource/sound/" + s)
				r.register("resource/scene/" + s)
				r.register("resource/creature/" + s)
				r.register("resource/effect/" + s)
			}
		}
	}
}

func (r *PathResolver) probeSequentialPatterns() {
	for mapID := 1; mapID <= 500; mapID++ {
		r.register(fmt.Sprintf("resource/scene/SCENE_%06d.jpg", mapID))
		r.register(fmt.Sprintf("resource/scene/SCENE_%06d.png", mapID))
		r.register(fmt.Sprintf("resource/scene/SCENE_%06d.swf", mapID))
		r.register(fmt.Sprintf("resource/scene/SCENE_%d.jpg", mapID))
		r.register(fmt.Sprintf("resource/scene/SCENE_%d.swf", mapID))
		r.register(fmt.Sprintf("icon/scene/SCENE_%06d.jpg", mapID))
		r.register(fmt.Sprintf("icon/scene/SCENE_%06d.png", mapID))
		for tx := 0; tx < 25; tx++ {
			for ty := 0; ty < 25; ty++ {
				r.register(fmt.Sprintf("resource/scene/%d/%d_%d.jpg", mapID, tx, ty))
				r.register(fmt.Sprintf("resource/scene/SCENE_%06d/%d_%d.jpg", mapID, tx, ty))
				r.register(fmt.Sprintf("resource/scene/SCENE_%06d_%d_%d.jpg", mapID, tx, ty))
			}
		}
	}

	r.probeWordRanges(15, []int{10, 120}, []int{2, 3, 4}, 0, 2000)
	r.probeWordRanges(40, []int{10, 15, 16, 20, 30, 40, 50, 60}, []int{2, 3, 4}, 0, 2000)
	r.probeWordRanges(90, []int{10, 120, 170, 180, 200, 210, 220, 230, 240}, []int{2, 5, 6, 7}, 0, 1500)
	r.probeWordRanges(110, []int{10}, []int{2, 3, 4}, 0, 1000)
	r.probeWordRanges(120, []int{10}, []int{2, 3, 4}, 0, 1000)
	r.probeWordRanges(130, []int{10, 90, 170, 240, 250}, []int{2, 3, 4}, 0, 1500)
	r.probeWordRanges(80, []int{10, 120, 130, 140, 150, 160, 170, 180, 190, 200, 240}, []int{2, 3, 4}, 0, 1500)
}

func (r *PathResolver) probeWordRanges(w1 int, w2s []int, exts []int, minNum int, maxNum int) {
	folder := resURLFolder[w1]
	p1 := resURLWord1[w1]
	for _, w2 := range w2s {
		p2 := resURLWord2[w2]
		for _, extIdx := range exts {
			ext := resURLExt[extIdx]
			for n := minNum; n <= maxNum; n++ {
				rel := fmt.Sprintf("%s%s%s%06d%s", folder, p1, p2, n, ext)
				r.register("resource/" + rel)
				r.register("icon/" + rel)
			}
		}
	}
}
