package emf

import (
	"bytes"
	"fmt"
	"image/png"
	"io"
	"log"
	"os"
	"strings"
)

func ExecEMF(fdata []byte, fname string) {
	if err := ExecEMFWithError(fdata, fname); err != nil {
		log.Printf("emf: %v", err)
	}
}

// ExecEMFWithError converts an EMF byte slice to PNG. When fname is empty,
// the PNG is written to stdout; otherwise the .png output is created next to
// the supplied EMF filename.
func ExecEMFWithError(fdata []byte, fname string) error {
	file, err := ReadFile(fdata)
	if err != nil {
		return err
	}

	img := file.Draw()

	var f io.Writer

	if fname != "" {
		f, err = os.Create(strings.TrimSuffix(fname, ".emf") + ".png")
		if err != nil {
			return err
		}
		defer func() {
			_ = f.(*os.File).Close()
		}()

	} else {
		f = os.Stdout
	}

	err = png.Encode(f, img)
	if err != nil {
		return err
	}
	return nil
}

// Convert converts an EMF input (either io.Reader or []byte) and returns the corresponding PNG output (either io.Reader or []byte).
func Convert(input any) (any, error) {
	var fdata []byte
	var err error

	switch v := input.(type) {
	case []byte:
		fdata = v
	case io.Reader:
		fdata, err = io.ReadAll(v)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported input type: %T", input)
	}

	file, err := ReadFile(fdata)
	if err != nil {
		return nil, err
	}

	img := file.Draw()

	var buf bytes.Buffer
	err = png.Encode(&buf, img)
	if err != nil {
		return nil, err
	}

	switch input.(type) {
	case []byte:
		return buf.Bytes(), nil
	case io.Reader:
		return bytes.NewReader(buf.Bytes()), nil
	default:
		return buf.Bytes(), nil
	}
}

// ConvertEMFToPNGBytes converts a byte slice of EMF data to a byte slice of PNG data.
func ConvertEMFToPNGBytes(fdata []byte) ([]byte, error) {
	file, err := ReadFile(fdata)
	if err != nil {
		return nil, err
	}

	img := file.Draw()

	var buf bytes.Buffer
	err = png.Encode(&buf, img)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ConvertEMFToPNGStream converts an EMF stream from an io.Reader to a PNG stream in an io.Writer.
func ConvertEMFToPNGStream(r io.Reader, w io.Writer) error {
	fdata, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	file, err := ReadFile(fdata)
	if err != nil {
		return err
	}

	img := file.Draw()

	return png.Encode(w, img)
}

// ConvertEmfToPng converts a byte slice of EMF data to a byte slice of PNG data.
// This is a convenient wrapper matching the user's preferred naming.
func ConvertEmfToPng(fdata []byte) ([]byte, error) {
	return ConvertEMFToPNGBytes(fdata)
}
