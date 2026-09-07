package emf

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"

	"github.com/llgcode/draw2d/draw2dimg"
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

func TestBitmapRecordDecodesRLE8(t *testing.T) {
	record := &bitmapRecord{
		offBmiSrc: 1,
		BmiSrc:    BitmapInfoHeader{Width: 3, Height: 1, Planes: 1, BitCount: BI_BITCOUNT_3, Compression: BI_RLE8},
		ColorTable: []byte{
			0, 0, 0, 0,
			0, 0, 255, 0,
		},
		BitsSrc: []byte{3, 1, 0, 1},
	}
	img := record.readDIBImage()
	if img == nil {
		t.Fatal("RLE8 image should decode")
	}
	for x := 0; x < 3; x++ {
		if got := color.RGBAModel.Convert(img.At(x, 0)).(color.RGBA); got.R != 255 || got.A == 0 {
			t.Fatalf("RLE8 pixel %d = %+v, want red", x, got)
		}
	}
}

func TestBitmapRecordDecodesRLE4AbsoluteMode(t *testing.T) {
	record := &bitmapRecord{
		offBmiSrc: 1,
		BmiSrc:    BitmapInfoHeader{Width: 4, Height: 1, Planes: 1, BitCount: BI_BITCOUNT_2, Compression: BI_RLE4},
		ColorTable: []byte{
			0, 0, 0, 0,
			0, 0, 255, 0,
			0, 255, 0, 0,
			255, 0, 0, 0,
		},
		BitsSrc: []byte{0, 4, 0x12, 0x30, 0, 0, 0, 1},
	}
	img := record.readDIBImage()
	if img == nil {
		t.Fatal("RLE4 image should decode")
	}
	want := []color.RGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}, {A: 255}}
	for x, expected := range want {
		if got := color.RGBAModel.Convert(img.At(x, 0)).(color.RGBA); got != expected {
			t.Fatalf("RLE4 pixel %d = %+v, want %+v", x, got, expected)
		}
	}
}

func TestClipRectLimitsPaint(t *testing.T) {
	file := &EmfFile{}
	ctx := file.initContext(10, 10)
	ctx.applyClipMask(ctx.clipRect(RectL{Left: 2, Top: 2, Right: 8, Bottom: 8}), RGN_AND)
	ctx.SetFillColor(color.RGBA{R: 255, A: 255})
	ctx.MoveTo(0, 0)
	ctx.LineTo(10, 0)
	ctx.LineTo(10, 10)
	ctx.LineTo(0, 10)
	ctx.Close()
	ctx.Fill()

	if got := ctx.img.At(1, 1); got != (color.RGBA{}) {
		t.Fatalf("outside clip = %v, want transparent", got)
	}
	if got := color.RGBAModel.Convert(ctx.img.At(4, 4)).(color.RGBA); got.R != 255 || got.A == 0 {
		t.Fatalf("inside clip = %+v, want red", got)
	}
}

func TestRasterOperations(t *testing.T) {
	source := color.RGBA{R: 0x0f, G: 0xf0, B: 0xaa, A: 0xff}
	destination := color.RGBA{R: 0xf0, G: 0x0f, B: 0x55, A: 0xff}
	got, ok := applyRasterOperation(source, destination, 0x00660046)
	if !ok || got != (color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
		t.Fatalf("SRCINVERT = %+v, %v", got, ok)
	}
	got, ok = applyRasterOperation(source, destination, 0x00aa0000)
	if !ok || got != destination {
		t.Fatalf("DST = %+v, %v", got, ok)
	}
}

func TestMaskBltUsesForegroundAndBackgroundRops(t *testing.T) {
	ctx := (&EmfFile{}).initContext(2, 1)
	ctx.img.Set(0, 0, color.RGBA{B: 255, A: 255})
	ctx.img.Set(1, 0, color.RGBA{B: 255, A: 255})
	record := &MaskbltRecord{
		CxDest:          2,
		CyDest:          1,
		RasterOperation: 0xAACC0020,
		Source: bitmapRecord{
			offBmiSrc: 1,
			BmiSrc:    BitmapInfoHeader{Width: 2, Height: 1, Planes: 1, BitCount: BI_BITCOUNT_5, Compression: BI_RGB},
			BitsSrc:   []byte{0, 0, 255, 0, 255, 0, 0, 0},
		},
		Mask: bitmapRecord{
			offBmiSrc: 1,
			BmiSrc:    BitmapInfoHeader{Width: 2, Height: 1, Planes: 1, BitCount: BI_BITCOUNT_1, Compression: BI_RGB},
			BitsSrc:   []byte{0x80, 0, 0, 0},
		},
	}
	record.Draw(ctx)
	if got := color.RGBAModel.Convert(ctx.img.At(0, 0)).(color.RGBA); got.R != 255 || got.G != 0 || got.B != 0 {
		t.Fatalf("masked foreground pixel = %+v, want red", got)
	}
	if got := color.RGBAModel.Convert(ctx.img.At(1, 0)).(color.RGBA); got.R != 0 || got.G != 0 || got.B != 255 {
		t.Fatalf("masked background pixel = %+v, want original blue", got)
	}
}

func TestPlgbltMapsSourceIntoParallelogram(t *testing.T) {
	ctx := (&EmfFile{}).initContext(3, 2)
	record := &PlgbltRecord{
		AptlDest: [3]PointL{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 1, Y: 1}},
		CxSrc:    2,
		CySrc:    1,
		Source: bitmapRecord{
			offBmiSrc: 1,
			BmiSrc:    BitmapInfoHeader{Width: 2, Height: 1, Planes: 1, BitCount: BI_BITCOUNT_5, Compression: BI_RGB},
			BitsSrc:   []byte{0, 0, 255, 0, 255, 0, 0, 0},
		},
	}
	record.Draw(ctx)
	if got := color.RGBAModel.Convert(ctx.img.At(0, 0)).(color.RGBA); got.R != 255 || got.G != 0 || got.B != 0 {
		t.Fatalf("PLGBLT first pixel = %+v, want red", got)
	}
	if got := color.RGBAModel.Convert(ctx.img.At(1, 0)).(color.RGBA); got.R != 0 || got.G != 255 || got.B != 0 {
		t.Fatalf("PLGBLT second pixel = %+v, want green", got)
	}
	if got := ctx.img.At(1, 1); got != (color.RGBA{}) {
		t.Fatalf("PLGBLT outside parallelogram = %v, want transparent", got)
	}
}

func TestPaletteRecordsColorIndexedBitmap(t *testing.T) {
	ctx := (&EmfFile{}).initContext(2, 1)
	create := &PaletteRecord{
		Handle:  9,
		Entries: []color.RGBA{{R: 255, A: 255}, {G: 255, A: 255}},
	}
	create.Draw(ctx)
	(&SelectpaletteRecord{Handle: 9}).Draw(ctx)
	(&SetpaletteentriesRecord{Handle: 9, Start: 1, Entries: []color.RGBA{{B: 255, A: 255}}}).Draw(ctx)
	(&ResizepaletteRecord{Handle: 9, Count: 3}).Draw(ctx)

	record := &BitbltRecord{bitmapRecord: bitmapRecord{
		Record:                Record{Type: EMR_BITBLT},
		offBmiSrc:             1,
		xDest:                 0,
		yDest:                 0,
		cxDest:                2,
		cyDest:                1,
		BitBltRasterOperation: 0x00cc0020,
		BmiSrc: BitmapInfoHeader{
			Width: 2, Height: 1, Planes: 1, BitCount: BI_BITCOUNT_3, Compression: BI_RGB,
		},
		UsageSrc: DIB_PAL_INDICES,
		BitsSrc:  []byte{0, 1, 0, 0, 0, 0, 0, 0},
	}}
	record.Draw(ctx)
	if got := color.RGBAModel.Convert(ctx.img.At(0, 0)).(color.RGBA); got.R != 255 || got.G != 0 || got.B != 0 {
		t.Fatalf("palette pixel 0 = %+v, want red", got)
	}
	if got := color.RGBAModel.Convert(ctx.img.At(1, 0)).(color.RGBA); got.R != 0 || got.G != 0 || got.B != 255 {
		t.Fatalf("palette pixel 1 = %+v, want blue", got)
	}
	if len(ctx.palette.Entries) != 3 || ctx.palette.Entries[2].A != 255 {
		t.Fatalf("resized palette = %+v", ctx.palette.Entries)
	}
}

func TestRestoreDCHonorsAbsoluteAndRelativeLevels(t *testing.T) {
	ctx := (&EmfFile{}).initContext(1, 1)
	red := color.RGBA{R: 255, A: 255}
	green := color.RGBA{G: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	ctx.SetFillColor(red)
	(&SavedcRecord{}).Draw(ctx)
	ctx.SetFillColor(green)
	(&SavedcRecord{}).Draw(ctx)
	ctx.SetFillColor(blue)
	(&SavedcRecord{}).Draw(ctx)
	ctx.SetFillColor(color.White)

	(&RestoredcRecord{SavedDC: -1}).Draw(ctx)
	if ctx.fillColor != green || len(ctx.savedStates) != 1 {
		t.Fatalf("RestoreDC(-1) fill=%v levels=%d, want green and one level", ctx.fillColor, len(ctx.savedStates))
	}
	(&RestoredcRecord{SavedDC: 1}).Draw(ctx)
	if ctx.fillColor != red || len(ctx.savedStates) != 0 {
		t.Fatalf("RestoreDC(1) fill=%v levels=%d, want red and zero levels", ctx.fillColor, len(ctx.savedStates))
	}
}

func testRegionData(rect RectL) []byte {
	data := bytes.NewBuffer(nil)
	if err := binary.Write(data, binary.LittleEndian, uint32(48)); err != nil {
		panic(err)
	}
	if err := binary.Write(data, binary.LittleEndian, uint32(1)); err != nil {
		panic(err)
	}
	if err := binary.Write(data, binary.LittleEndian, uint32(1)); err != nil {
		panic(err)
	}
	if err := binary.Write(data, binary.LittleEndian, uint32(16)); err != nil {
		panic(err)
	}
	if err := binary.Write(data, binary.LittleEndian, rect); err != nil {
		panic(err)
	}
	if err := binary.Write(data, binary.LittleEndian, rect); err != nil {
		panic(err)
	}
	return data.Bytes()
}

func TestRegionRecordsPaintAndInvert(t *testing.T) {
	ctx := (&EmfFile{}).initContext(10, 10)
	ctx.objects[7] = LogBrushEx{Color: ColorRef{Red: 255}}
	region := testRegionData(RectL{Left: 2, Top: 2, Right: 8, Bottom: 8})

	fill := &FillrgnRecord{Brush: 7, RegionData: region}
	fill.Draw(ctx)
	if got := color.RGBAModel.Convert(ctx.img.At(4, 4)).(color.RGBA); got.R != 255 || got.A == 0 {
		t.Fatalf("filled region center = %+v, want opaque red", got)
	}
	if got := ctx.img.At(1, 1); got != (color.RGBA{}) {
		t.Fatalf("outside filled region = %v, want transparent", got)
	}

	frame := &FramergnRecord{Brush: 7, FrameSize: SizeL{Cx: 1, Cy: 1}, RegionData: region}
	ctx.img = image.NewRGBA(image.Rect(0, 0, 10, 10))
	ctx.GraphicContext = *draw2dimg.NewGraphicContext(ctx.img)
	frame.Draw(ctx)
	if got := color.RGBAModel.Convert(ctx.img.At(2, 2)).(color.RGBA); got.R != 255 || got.A == 0 {
		t.Fatalf("region frame edge = %+v, want opaque red", got)
	}
	if got := ctx.img.At(4, 4); got != (color.RGBA{}) {
		t.Fatalf("region frame center = %v, want transparent", got)
	}

	ctx.img.Set(4, 4, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	invert := &InvertrgnRecord{RegionData: region}
	invert.Draw(ctx)
	if got := color.RGBAModel.Convert(ctx.img.At(4, 4)).(color.RGBA); got.R != 245 || got.G != 235 || got.B != 225 {
		t.Fatalf("inverted region center = %+v", got)
	}
}

func TestRecordPointCountCannotExceedPayload(t *testing.T) {
	data := bytes.NewBuffer(nil)
	_ = binary.Write(data, binary.LittleEndian, uint32(EMR_POLYGON))
	_ = binary.Write(data, binary.LittleEndian, uint32(28))
	_ = binary.Write(data, binary.LittleEndian, RectL{})
	_ = binary.Write(data, binary.LittleEndian, uint32(1))
	if _, err := readRecord(bytes.NewReader(data.Bytes())); err == nil {
		t.Fatal("polygon with missing point payload should fail")
	}
}

func TestDrawNilOrInvalidFileIsSafe(t *testing.T) {
	if got := (&EmfFile{}).Draw(); got.Bounds() != image.Rect(0, 0, 1, 1) {
		t.Fatalf("invalid file bounds = %v", got.Bounds())
	}
	if _, err := ReadFile([]byte{1, 2, 3}); err == nil {
		t.Fatal("short EMF should return an error")
	}
}
