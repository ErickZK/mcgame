// Command bus-extract extracts the zlib payload from a BUS resource.
//
// It intentionally never modifies the input file. The default format is:
//
//	8-byte BUS header followed by a zlib stream
package main

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	input := flag.String("input", "", "BUS resource to extract")
	output := flag.String("output", "", "output file; defaults to <input>.decompressed")
	offset := flag.Int("offset", 8, "byte offset where the zlib stream starts")
	flag.Parse()

	if *input == "" {
		fail("-input is required")
	}
	if *offset < 0 {
		fail("-offset cannot be negative")
	}

	data, err := os.ReadFile(*input)
	if err != nil {
		fail("read input: %v", err)
	}
	if len(data) < *offset {
		fail("input has %d bytes, but offset is %d", len(data), *offset)
	}

	if len(data) >= 3 && string(data[:3]) == "BUS" {
		fmt.Printf("BUS header: %q\n", data[:3])
		if len(data) >= 8 {
			fmt.Printf("BUS header bytes: % X\n", data[:8])
			fmt.Printf("little-endian header value: %d\n", binary.LittleEndian.Uint32(data[4:8]))
		}
	} else {
		fmt.Println("warning: BUS magic not found; trying zlib anyway")
	}

	if len(data) < *offset+2 || data[*offset] != 0x78 {
		fmt.Printf("warning: expected zlib CMF at offset %d, found % X\n", *offset, prefix(data[*offset:]))
	}

	decoded, codec, err := decompress(data[*offset:])
	if err != nil {
		fail("decompress payload at offset %d: %v", *offset, err)
	}

	if *output == "" {
		*output = *input + ".decompressed"
	}
	if err := os.WriteFile(*output, decoded, 0644); err != nil {
		fail("write output: %v", err)
	}

	fmt.Printf("decompressed: %d -> %d bytes\n", len(data)-*offset, len(decoded))
	fmt.Printf("codec: %s\n", codec)
	fmt.Printf("output: %s\n", *output)
	fmt.Printf("detected format: %s\n", format(decoded))
}

func decompress(payload []byte) ([]byte, string, error) {
	if reader, err := zlib.NewReader(bytes.NewReader(payload)); err == nil {
		decoded, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr == nil {
			readErr = closeErr
		}
		if readErr == nil {
			return decoded, "zlib", nil
		}
	}

	// Some game containers store a raw DEFLATE stream while retaining bytes
	// that resemble a zlib header. Try raw DEFLATE as a diagnostic fallback.
	reader := flate.NewReader(bytes.NewReader(payload))
	decoded, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr == nil {
		readErr = closeErr
	}
	if readErr == nil {
		return decoded, "raw-deflate", nil
	}

	return nil, "", fmt.Errorf("zlib and raw-deflate failed (zlib header may be custom or encrypted): %v", readErr)
}

func prefix(data []byte) []byte {
	if len(data) > 8 {
		return data[:8]
	}
	return data
}

func format(data []byte) string {
	switch {
	case len(data) >= 3 && string(data[:3]) == "FWS":
		return "SWF (uncompressed)"
	case len(data) >= 3 && string(data[:3]) == "CWS":
		return "SWF (zlib-compressed)"
	case len(data) >= 3 && string(data[:3]) == "ZWS":
		return "SWF (LZMA-compressed)"
	case len(data) >= 4 && bytes.Equal(data[:4], []byte("PK\x03\x04")):
		return "ZIP"
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "PNG"
	case len(data) >= 4 && bytes.Equal(data[:4], []byte("OggS")):
		return "Ogg audio"
	case len(data) >= 4 && bytes.Equal(data[:4], []byte("RIFF")):
		return "RIFF/WAV or related format"
	default:
		return "unknown/custom"
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bus-extract: "+format+"\n", args...)
	os.Exit(1)
}
