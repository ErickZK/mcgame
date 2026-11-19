// Command bus-cachebust changes the hashed ChFields resource name inside a BUS client.
package main

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
)

const (
	oldResource = "resource/ui/ChFields.swf"
	newResource = "resource/ui/ChFieldsPTBR"
)

func main() {
	input := flag.String("input", "", "encoded BUS input")
	output := flag.String("output", "", "encoded BUS output")
	flag.Parse()
	if *input == "" || *output == "" {
		fail("-input and -output are required")
	}

	encoded, err := os.ReadFile(*input)
	if err != nil {
		fail("read input: %v", err)
	}
	decoded := xorBytes(encoded)
	if len(decoded) < 8 || string(decoded[:3]) != "CWS" {
		fail("decoded BUS does not contain a CWS header")
	}

	reader, err := zlib.NewReader(bytes.NewReader(decoded[8:]))
	if err != nil {
		fail("open SWF payload: %v", err)
	}
	body, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil {
		fail("read SWF payload: %v", err)
	}
	if closeErr != nil {
		fail("close SWF payload: %v", closeErr)
	}

	raw := make([]byte, 0, 8+len(body))
	raw = append(raw, 'F', 'W', 'S')
	raw = append(raw, decoded[3:8]...)
	raw = append(raw, body...)
	old := []byte(oldResource)
	newValue := []byte(newResource)
	if len(old) != len(newValue) {
		fail("resource names must have equal byte length")
	}
	count := bytes.Count(raw, old)
	if count != 1 {
		fail("expected one ChFields resource name, found %d", count)
	}
	raw = bytes.Replace(raw, old, newValue, 1)
	compressed := zlibCompress(raw[8:])
	newDecoded := append([]byte{'C', 'W', 'S'}, raw[3:8]...)
	newDecoded = append(newDecoded, compressed...)
	newEncoded := xorBytes(newDecoded)
	if err := os.WriteFile(*output, newEncoded, 0644); err != nil {
		fail("write output: %v", err)
	}

	hash := md5.Sum([]byte(newResource))
	fmt.Printf("patched=%d resource=%s hash=%s bytes=%d\n", count, newResource, hex.EncodeToString(hash[:]), len(newEncoded))
}

func xorBytes(input []byte) []byte {
	output := make([]byte, len(input))
	for index, value := range input {
		output[index] = value ^ byte((index+1)%3)
	}
	return output
}

func zlibCompress(input []byte) []byte {
	var buffer bytes.Buffer
	writer := zlib.NewWriter(&buffer)
	if _, err := writer.Write(input); err != nil {
		fail("compress SWF payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		fail("close compressed SWF payload: %v", err)
	}
	return buffer.Bytes()
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bus-cachebust: "+format+"\n", args...)
	os.Exit(1)
}
