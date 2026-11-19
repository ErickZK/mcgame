// This file is the CLI entry point for asset-extract. It resolves hashed
// resource filenames, decrypts and unpacks frontend/s/res/, the main client
// BUS SWF, GameLoaders.swf, profile configs, and decompiled ActionScript
// code into a structured browser-ready directory tree with a JSON manifest.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

type ManifestEntry struct {
	SourceFile   string   `json:"sourceFile"`
	Hash         string   `json:"hash,omitempty"`
	LogicalPath  string   `json:"logicalPath,omitempty"`
	Category     string   `json:"category"`
	Kind         string   `json:"kind"`
	Ext          string   `json:"ext"`
	Layers       []string `json:"layers,omitempty"`
	SizeBytes    int      `json:"sizeBytes"`
	OutputPath   string   `json:"outputPath"`
	DecodedSWF   string   `json:"decodedSwf,omitempty"`
	ImageCount   int      `json:"imageCount,omitempty"`
	SoundCount   int      `json:"soundCount,omitempty"`
	SpriteCount  int      `json:"spriteCount,omitempty"`
	BinaryCount  int      `json:"binaryCount,omitempty"`
	HasVectors   bool     `json:"hasVectors,omitempty"`
	HasScripts   bool     `json:"hasScripts,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type ExtractReport struct {
	TotalResources      int            `json:"totalResources"`
	ResolvedNames       int            `json:"resolvedNames"`
	UnresolvedNames     int            `json:"unresolvedNames"`
	ByKind              map[string]int `json:"byKind"`
	ByCategory          map[string]int `json:"byCategory"`
	TotalSWFImages      int            `json:"totalSwfImages"`
	TotalSWFSounds      int            `json:"totalSwfSounds"`
	TotalSWFSprites     int            `json:"totalSwfSprites"`
	TotalSWFBinaries    int            `json:"totalSwfBinaries"`
	TotalNestedSWFs     int            `json:"totalNestedSwfs"`
	SWFsWithVectorsOnly int            `json:"swfsWithVectorsOnly"`
	CopiedAS3Files      int            `json:"copiedAs3Files"`
	CopiedConfigFiles   int            `json:"copiedConfigFiles"`
	Errors              int            `json:"errors"`
}

func main() {
	outRoot := flag.String("out", "extracted", "output directory for extracted assets")
	resDir := flag.String("res", filepath.Join("frontend", "s", "res"), "hashed resource directory")
	mainBUS := flag.String("main-bus", filepath.Join("frontend", "s", "6a41a5ba5c0006ab4e0c1321a2b05027"), "main encrypted BUS client")
	loaderSWF := flag.String("loader-swf", filepath.Join("frontend", "s", "sv", "GameLoaders.swf"), "loader SWF path")
	workers := flag.Int("workers", runtime.NumCPU(), "number of concurrent extraction workers")
	flag.Parse()

	if err := os.MkdirAll(*outRoot, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output root: %v\n", err)
		os.Exit(1)
	}

	entries, err := os.ReadDir(*resDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read %s: %v\n", *resDir, err)
		os.Exit(1)
	}

	var hashes []string
	for _, e := range entries {
		if !e.IsDir() {
			hashes = append(hashes, e.Name())
		}
	}
	fmt.Printf("Found %d files in %s. Resolving MD5 hashes...\n", len(hashes), *resDir)

	resolver := newPathResolver(hashes)
	resolver.scanDirectory(filepath.Join("supabase", "seeds"), map[string]bool{".sql": true})
	resolver.scanDirectory(filepath.Join("docs", "client"), map[string]bool{".as": true})
	resolver.scanDirectory(filepath.Join("work", "client-as-20261116"), map[string]bool{".as": true})
	resolver.scanDirectory(filepath.Join("work", "gameloaders-as"), map[string]bool{".as": true})
	resolver.scanDirectory(filepath.Join("frontend", "s", "profile"), map[string]bool{".xml": true, ".html": true, ".txt": true})
	resolver.scanDirectory(filepath.Join("frontend", "s", "sv", "profile"), map[string]bool{".xml": true, ".html": true, ".txt": true})
	resolver.probeSequentialPatterns()

	fmt.Printf("Resolved %d / %d hashed resource names.\n", len(resolver.resolved), len(hashes))

	var (
		mu       sync.Mutex
		manifest []ManifestEntry
		report   = ExtractReport{
			TotalResources:  len(hashes),
			ResolvedNames:   len(resolver.resolved),
			UnresolvedNames: len(hashes) - len(resolver.resolved),
			ByKind:          make(map[string]int),
			ByCategory:      make(map[string]int),
		}
		doneCount int64
	)

	jobs := make(chan string, 256)
	var wg sync.WaitGroup
	numWorkers := max(1, *workers)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for hash := range jobs {
				entry, st := processResFile(*resDir, hash, resolver.resolved[strings.ToLower(hash)], *outRoot)
				mu.Lock()
				manifest = append(manifest, entry)
				report.ByKind[entry.Kind]++
				report.ByCategory[entry.Category]++
				report.TotalSWFImages += st.Images
				report.TotalSWFSounds += st.Sounds
				report.TotalSWFSprites += st.Sprites
				report.TotalSWFBinaries += st.Binaries
				report.TotalNestedSWFs += st.NestedSWFs
				if entry.Kind == "swf" && st.Images == 0 && entry.HasVectors {
					report.SWFsWithVectorsOnly++
				}
				if entry.Error != "" {
					report.Errors++
				}
				mu.Unlock()
				n := atomic.AddInt64(&doneCount, 1)
				if n%1000 == 0 || int(n) == len(hashes) {
					fmt.Printf("Processed %d / %d resources...\n", n, len(hashes))
				}
			}
		}()
	}

	for _, h := range hashes {
		jobs <- h
	}
	close(jobs)
	wg.Wait()

	processSpecialSWF(*mainBUS, "main-client", *outRoot, &manifest, &report)
	processSpecialSWF(*loaderSWF, "loader", *outRoot, &manifest, &report)

	report.CopiedConfigFiles += copyTree(filepath.Join("frontend", "s", "profile"), filepath.Join(*outRoot, "config", "profile"))
	report.CopiedConfigFiles += copyTree(filepath.Join("frontend", "s", "sv", "profile"), filepath.Join(*outRoot, "config", "sv-profile"))

	if dirExists(filepath.Join("work", "client-as-20261116", "scripts")) {
		report.CopiedAS3Files += copyTree(filepath.Join("work", "client-as-20261116", "scripts"), filepath.Join(*outRoot, "code", "client-as3"))
	} else {
		report.CopiedAS3Files += copyTree(filepath.Join("docs", "client"), filepath.Join(*outRoot, "code", "client-as3"))
	}
	report.CopiedAS3Files += copyTree(filepath.Join("work", "gameloaders-as"), filepath.Join(*outRoot, "code", "loader-as3"))

	sort.Slice(manifest, func(i, j int) bool {
		if manifest[i].Category != manifest[j].Category {
			return manifest[i].Category < manifest[j].Category
		}
		return manifest[i].OutputPath < manifest[j].OutputPath
	})

	writeManifestAndSummary(*outRoot, manifest, report)
	fmt.Printf("Extraction complete! Output: %s\n", *outRoot)
}

func processResFile(resDir string, hash string, logicalPath string, outRoot string) (ManifestEntry, SWFExtractStats) {
	srcPath := filepath.Join(resDir, hash)
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return ManifestEntry{
			SourceFile:  srcPath,
			Hash:        hash,
			LogicalPath: logicalPath,
			Category:    "error",
			Kind:        "error",
			Error:       err.Error(),
		}, SWFExtractStats{}
	}

	det := detectAndUnwrap(raw)
	category := classifyCategory(logicalPath, "unmapped")
	baseName := hash
	if logicalPath != "" {
		baseName = strings.TrimSuffix(filepath.Base(logicalPath), filepath.Ext(logicalPath))
		baseName = sanitizeSegment(baseName) + "_" + hash[:8]
	}

	entry := ManifestEntry{
		SourceFile:  srcPath,
		Hash:        hash,
		LogicalPath: logicalPath,
		Category:    category,
		Kind:        det.Kind,
		Ext:         det.Ext,
		Layers:      det.Layers,
		SizeBytes:   len(det.Data),
	}

	switch det.Kind {
	case "image":
		relOut := filepath.Join("images", category, baseName+det.Ext)
		fullOut := filepath.Join(outRoot, relOut)
		if err := writeFileEnsuringDir(fullOut, det.Data); err != nil {
			entry.Error = err.Error()
		}
		entry.OutputPath = filepath.ToSlash(relOut)
		return entry, SWFExtractStats{}

	case "sound":
		relOut := filepath.Join("sounds", category, baseName+det.Ext)
		fullOut := filepath.Join(outRoot, relOut)
		if err := writeFileEnsuringDir(fullOut, det.Data); err != nil {
			entry.Error = err.Error()
		}
		entry.OutputPath = filepath.ToSlash(relOut)
		return entry, SWFExtractStats{}

	case "swf":
		relSpriteDir := filepath.Join("sprites", category, baseName)
		fullSpriteDir := filepath.Join(outRoot, relSpriteDir)
		soundsDir := filepath.Join(outRoot, "sounds", "from-swf", category)

		meta, st, _, err := extractSWFBundle(det.Data, fullSpriteDir, soundsDir, baseName, 0)
		if err != nil {
			entry.Error = err.Error()
		}
		relSWF := filepath.Join("swf-decoded", category, baseName+".swf")
		_ = writeFileEnsuringDir(filepath.Join(outRoot, relSWF), det.Data)

		entry.OutputPath = filepath.ToSlash(relSpriteDir)
		entry.DecodedSWF = filepath.ToSlash(relSWF)
		entry.ImageCount = st.Images
		entry.SoundCount = st.Sounds
		entry.SpriteCount = st.Sprites
		entry.BinaryCount = st.Binaries
		entry.HasVectors = meta.HasVectorShape
		entry.HasScripts = meta.HasScripts
		return entry, st

	case "config":
		relOut := filepath.Join("config", category, baseName+det.Ext)
		if err := writeFileEnsuringDir(filepath.Join(outRoot, relOut), det.Data); err != nil {
			entry.Error = err.Error()
		}
		entry.OutputPath = filepath.ToSlash(relOut)
		return entry, SWFExtractStats{}

	default:
		relOut := filepath.Join("other", category, baseName+det.Ext)
		if err := writeFileEnsuringDir(filepath.Join(outRoot, relOut), det.Data); err != nil {
			entry.Error = err.Error()
		}
		entry.OutputPath = filepath.ToSlash(relOut)
		return entry, SWFExtractStats{}
	}
}

func processSpecialSWF(srcPath string, label string, outRoot string, manifest *[]ManifestEntry, report *ExtractReport) {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return
	}
	fmt.Printf("Extracting special SWF: %s (%s)...\n", label, srcPath)
	det := detectAndUnwrap(raw)
	relSpriteDir := filepath.Join("sprites", label)
	fullSpriteDir := filepath.Join(outRoot, relSpriteDir)
	soundsDir := filepath.Join(outRoot, "sounds", "from-swf", label)

	meta, st, fws, err := extractSWFBundle(det.Data, fullSpriteDir, soundsDir, label, 0)
	relSWF := filepath.Join("swf-decoded", label+".swf")
	if len(fws) > 0 {
		_ = writeFileEnsuringDir(filepath.Join(outRoot, relSWF), fws)
	}

	entry := ManifestEntry{
		SourceFile:  srcPath,
		LogicalPath: label,
		Category:    label,
		Kind:        det.Kind,
		Ext:         det.Ext,
		Layers:      det.Layers,
		SizeBytes:   len(det.Data),
		OutputPath:  filepath.ToSlash(relSpriteDir),
		DecodedSWF:  filepath.ToSlash(relSWF),
		ImageCount:  st.Images,
		SoundCount:  st.Sounds,
		SpriteCount: st.Sprites,
		BinaryCount: st.Binaries,
		HasVectors:  meta.HasVectorShape,
		HasScripts:  meta.HasScripts,
	}
	if err != nil {
		entry.Error = err.Error()
		report.Errors++
	}
	*manifest = append(*manifest, entry)
	report.TotalSWFImages += st.Images
	report.TotalSWFSounds += st.Sounds
	report.TotalSWFSprites += st.Sprites
	report.TotalSWFBinaries += st.Binaries
	report.TotalNestedSWFs += st.NestedSWFs
}

func writeFileEnsuringDir(dst string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func copyTree(srcRoot string, dstRoot string) int {
	if !dirExists(srcRoot) {
		return 0
	}
	count := 0
	_ = filepath.WalkDir(srcRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, rErr := filepath.Rel(srcRoot, p)
		if rErr != nil {
			return nil
		}
		dst := filepath.Join(dstRoot, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil
		}
		if err := copyFile(p, dst); err == nil {
			count++
		}
		return nil
	})
	return count
}

func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func writeManifestAndSummary(outRoot string, manifest []ManifestEntry, report ExtractReport) {
	payload := struct {
		Report  ExtractReport   `json:"report"`
		Entries []ManifestEntry `json:"entries"`
	}{
		Report:  report,
		Entries: manifest,
	}
	if data, err := json.MarshalIndent(payload, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(outRoot, "manifest.json"), data, 0o644)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "MCGame Full Asset Extraction Summary\n")
	fmt.Fprintf(&b, "====================================\n")
	fmt.Fprintf(&b, "Total hashed files in res/ : %d\n", report.TotalResources)
	fmt.Fprintf(&b, "Resolved original names    : %d (%.1f%%)\n", report.ResolvedNames, float64(report.ResolvedNames)*100/float64(max(1, report.TotalResources)))
	fmt.Fprintf(&b, "Unresolved hashes          : %d\n\n", report.UnresolvedNames)

	fmt.Fprintf(&b, "By Kind:\n")
	for _, k := range sortedKeys(report.ByKind) {
		fmt.Fprintf(&b, "  %-12s : %d\n", k, report.ByKind[k])
	}
	fmt.Fprintf(&b, "\nBy Category:\n")
	for _, k := range sortedKeys(report.ByCategory) {
		fmt.Fprintf(&b, "  %-18s : %d\n", k, report.ByCategory[k])
	}
	fmt.Fprintf(&b, "\nExtracted From Inside SWFs (including main-client & loader):\n")
	fmt.Fprintf(&b, "  Bitmap images (PNG/JPG/GIF): %d\n", report.TotalSWFImages)
	fmt.Fprintf(&b, "  Sounds (MP3/WAV)           : %d\n", report.TotalSWFSounds)
	fmt.Fprintf(&b, "  Sprites (MovieClips/meta)  : %d\n", report.TotalSWFSprites)
	fmt.Fprintf(&b, "  Embedded binary blobs      : %d\n", report.TotalSWFBinaries)
	fmt.Fprintf(&b, "  Nested SWFs unpacked       : %d\n", report.TotalNestedSWFs)
	fmt.Fprintf(&b, "  Vector-only SWFs (no bmp)  : %d\n\n", report.SWFsWithVectorsOnly)
	fmt.Fprintf(&b, "Copied Decompiled AS3 Files  : %d\n", report.CopiedAS3Files)
	fmt.Fprintf(&b, "Copied Profile Config Files  : %d\n", report.CopiedConfigFiles)
	fmt.Fprintf(&b, "Errors                       : %d\n", report.Errors)

	_ = os.WriteFile(filepath.Join(outRoot, "summary.txt"), []byte(b.String()), 0o644)
	fmt.Println()
	fmt.Print(b.String())
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
