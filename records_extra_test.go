package emf

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"testing"
)

func TestUnknownRecordIsPreserved(t *testing.T) {
	data := bytes.NewBuffer(nil)
	if err := binary.Write(data, binary.LittleEndian, uint32(0xdeadbeef)); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(data, binary.LittleEndian, uint32(8)); err != nil {
		t.Fatal(err)
	}
	record, err := readRecord(bytes.NewReader(data.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := record.(*RawRecord)
	if !ok {
		t.Fatalf("record type = %T, want *RawRecord", record)
	}
	if raw.Type != 0xdeadbeef || raw.Size != 8 {
		t.Fatalf("raw record = %+v", raw.Record)
	}
}

func TestBitmapRecordDecodes24BitDIB(t *testing.T) {
	record := &bitmapRecord{
		Record:    Record{Type: EMR_BITBLT, Size: 8},
		Bounds:    RectL{Right: 2, Bottom: 1},
		offBmiSrc: 1,
		BmiSrc: BitmapInfoHeader{
			Width:       2,
			Height:      1,
			Planes:      1,
			BitCount:    BI_BITCOUNT_5, // 24 bpp
			Compression: BI_RGB,
		},
		BitsSrc: []byte{
			0, 0, 255,
			0, 255, 0,
			0, 0,
		},
	}
	img := record.readDIBImage()
	if img == nil {
		t.Fatal("readDIBImage returned nil")
	}
	if got := color.RGBAModel.Convert(img.At(0, 0)).(color.RGBA); got.R != 255 || got.G != 0 || got.B != 0 {
		t.Fatalf("pixel 0 = %+v, want red", got)
	}
	if got := color.RGBAModel.Convert(img.At(1, 0)).(color.RGBA); got.R != 0 || got.G != 255 || got.B != 0 {
		t.Fatalf("pixel 1 = %+v, want green", got)
	}
}
