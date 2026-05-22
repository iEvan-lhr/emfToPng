package emf

import (
	"io"
	"log"
	"os"
	"testing"
)

func TestExecEMF(t *testing.T) {
	file, err := os.ReadFile("image1.emf")
	if err != nil {
		log.Fatal(err)
	}
	ExecEMF(file, "image1.emf")
}

func TestConvert(t *testing.T) {
	// 1. Test []byte input and []byte output
	data, err := os.ReadFile("image1.emf")
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	outBytesVal, err := Convert(data)
	if err != nil {
		t.Fatalf("Convert with []byte failed: %v", err)
	}

	outBytes, ok := outBytesVal.([]byte)
	if !ok {
		t.Fatalf("expected []byte output, got: %T", outBytesVal)
	}
	if len(outBytes) == 0 {
		t.Fatal("expected non-empty output bytes")
	}

	// 2. Test io.Reader (opened file stream) input and io.Reader output
	file, err := os.Open("image1.emf")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer file.Close()

	outReaderVal, err := Convert(file)
	if err != nil {
		t.Fatalf("Convert with io.Reader failed: %v", err)
	}

	outReader, ok := outReaderVal.(io.Reader)
	if !ok {
		t.Fatalf("expected io.Reader output, got: %T", outReaderVal)
	}

	// Verify we can read from the returned stream
	pngBytes, err := io.ReadAll(outReader)
	if err != nil {
		t.Fatalf("failed to read from returned stream: %v", err)
	}
	if len(pngBytes) == 0 {
		t.Fatal("expected non-empty bytes read from output stream")
	}

	// Verify that the contents match
	if len(outBytes) != len(pngBytes) {
		t.Errorf("byte slice output length (%d) does not match stream output length (%d)", len(outBytes), len(pngBytes))
	}
}
