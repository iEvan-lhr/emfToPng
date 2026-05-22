package emf

import (
	"bytes"
	"image"
	"image/draw"
	"os"

	"github.com/golang/freetype/truetype"
	"github.com/llgcode/draw2d"
	"github.com/llgcode/draw2d/draw2dimg"
)

type EmfFile struct {
	Header  *HeaderRecord
	Records []Recorder
	EOF     *EOFRecord
}

func ReadFile(data []byte) (*EmfFile, error) {
	// Skip any leading garbage or padding before EMR_HEADER (Type 1: 01 00 00 00)
	if len(data) >= 4 && !(data[0] == 1 && data[1] == 0 && data[2] == 0 && data[3] == 0) {
		if idx := bytes.Index(data, []byte{1, 0, 0, 0}); idx > 0 {
			data = data[idx:]
		}
	}

	reader := bytes.NewReader(data)
	file := &EmfFile{}

	for reader.Len() > 0 {
		rec, err := readRecord(reader)

		if err != nil {
			return nil, err
		}

		switch rec := rec.(type) {
		case *HeaderRecord:
			file.Header = rec
		case *EOFRecord:
			file.EOF = rec
			return file, nil
		default:
			file.Records = append(file.Records, rec)
		}
	}

	return file, nil
}

type context struct {
	draw2dimg.GraphicContext
	img     draw.Image
	objects map[uint32]interface{}

	w, h int

	wo, vo *PointL
	we, ve *SizeL
	mm     uint32
}

func (f *EmfFile) initContext(w, h int) *context {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	gc := draw2dimg.NewGraphicContext(img)
	gc.SetDPI(72)

	return &context{
		GraphicContext: *gc,
		img:            img,
		w:              w,
		h:              h,
		mm:             MM_TEXT,
		objects:        make(map[uint32]interface{}),
	}
}

func (ctx context) applyTransformation() {
	if ctx.we == nil || ctx.ve == nil {
		return
	}

	switch ctx.mm {
	case MM_ISOTROPIC, MM_ANISOTROPIC:
		sx := float64(ctx.ve.Cx) / float64(ctx.we.Cx)
		sy := float64(ctx.ve.Cy) / float64(ctx.we.Cy)
		ctx.Scale(sx, sy)
	default:
		sx := float64(ctx.w) / float64(ctx.we.Cx)
		sy := float64(ctx.h) / float64(ctx.we.Cy)
		ctx.Scale(sx, sy)

	}
}

func (f *EmfFile) Draw() image.Image {

	bounds := f.Header.Bounds

	// inclusive-inclusive bounds
	width := int(bounds.Width()) + 1
	height := int(bounds.Height()) + 1

	ctx := f.initContext(width, height)

	if bounds.Left != 0 || bounds.Top != 0 {
		ctx.Translate(-float64(bounds.Left), -float64(bounds.Top))
	}

	for _, rec := range f.Records {
		rec.Draw(ctx)
	}

	return ctx.img
}

type FallbackFontCache struct {
	defaultFont *truetype.Font
	boldFont    *truetype.Font
	italicFont  *truetype.Font
	biFont      *truetype.Font
}

func (c *FallbackFontCache) Load(fd draw2d.FontData) (*truetype.Font, error) {
	isBold := fd.Style&draw2d.FontStyleBold != 0
	isItalic := fd.Style&draw2d.FontStyleItalic != 0

	if isBold && isItalic && c.biFont != nil {
		return c.biFont, nil
	}
	if isBold && c.boldFont != nil {
		return c.boldFont, nil
	}
	if isItalic && c.italicFont != nil {
		return c.italicFont, nil
	}
	return c.defaultFont, nil
}

func (c *FallbackFontCache) Store(fd draw2d.FontData, font *truetype.Font) {}

func init() {
	// Try loading the user's NotoSansSC-VF.ttf first from the current directory
	if b, err := os.ReadFile("NotoSansSC-VF.ttf"); err == nil {
		if f, err := truetype.Parse(b); err == nil {
			draw2d.SetFontCache(&FallbackFontCache{
				defaultFont: f,
				boldFont:    f,
				italicFont:  f,
				biFont:      f,
			})
			return
		}
	}

	paths := []string{
		"C:\\Windows\\Fonts\\arial.ttf",
		"/Library/Fonts/Arial.ttf",
		"/Library/Fonts/Microsoft/Arial.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
	}

	var defaultFont *truetype.Font
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err == nil {
			f, err := truetype.Parse(b)
			if err == nil {
				defaultFont = f
				break
			}
		}
	}

	if defaultFont == nil {
		return
	}

	var boldFont, italicFont, biFont *truetype.Font
	if b, err := os.ReadFile("C:\\Windows\\Fonts\\arialbd.ttf"); err == nil {
		boldFont, _ = truetype.Parse(b)
	}
	if b, err := os.ReadFile("C:\\Windows\\Fonts\\ariali.ttf"); err == nil {
		italicFont, _ = truetype.Parse(b)
	}
	if b, err := os.ReadFile("C:\\Windows\\Fonts\\arialbi.ttf"); err == nil {
		biFont, _ = truetype.Parse(b)
	}

	draw2d.SetFontCache(&FallbackFontCache{
		defaultFont: defaultFont,
		boldFont:    boldFont,
		italicFont:  italicFont,
		biFont:      biFont,
	})
}
