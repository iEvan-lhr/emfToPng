package emf

import (
	"bytes"
	"image"
	"image/color"
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

// UnsupportedRecordTypes reports records that were parsed safely but have no
// renderer. Conversion remains best-effort, while callers can decide whether
// the output is acceptable for their input.
func (f *EmfFile) UnsupportedRecordTypes() map[uint32]int {
	counts := make(map[uint32]int)
	for _, record := range f.Records {
		if raw, ok := record.(*RawRecord); ok {
			counts[raw.Type]++
		}
	}
	return counts
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

	textColor    color.Color
	bkColor      color.Color
	textAlign    uint32
	breakExtra   int32
	breakCount   int32
	brushOrigin  PointL
	rop2         uint32
	arcDirection uint32
	miterLimit   float64

	worldTransform [6]float64
	gdiTransform   [6]float64
	savedStates    []contextState
}

type contextState struct {
	matrix         [6]float64
	mm             uint32
	textColor      color.Color
	bkColor        color.Color
	textAlign      uint32
	breakExtra     int32
	breakCount     int32
	brushOrigin    PointL
	rop2           uint32
	arcDirection   uint32
	miterLimit     float64
	worldTransform [6]float64
	gdiTransform   [6]float64
	we, ve         *SizeL
	wo, vo         *PointL
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
		textColor:      color.Black,
		bkColor:        color.White,
		textAlign:      0,
		rop2:           13, // R2_COPYPEN
		arcDirection:   1,  // AD_COUNTERCLOCKWISE
		miterLimit:     10,
		worldTransform: [6]float64{1, 0, 0, 1, 0, 0},
		gdiTransform:   [6]float64{1, 0, 0, 1, 0, 0},
	}
}

func (ctx *context) saveState() {
	state := contextState{
		matrix:         ctx.GetMatrixTransform(),
		mm:             ctx.mm,
		textColor:      ctx.textColor,
		bkColor:        ctx.bkColor,
		textAlign:      ctx.textAlign,
		breakExtra:     ctx.breakExtra,
		breakCount:     ctx.breakCount,
		brushOrigin:    ctx.brushOrigin,
		rop2:           ctx.rop2,
		arcDirection:   ctx.arcDirection,
		miterLimit:     ctx.miterLimit,
		worldTransform: ctx.worldTransform,
		gdiTransform:   ctx.gdiTransform,
	}
	if ctx.we != nil {
		v := *ctx.we
		state.we = &v
	}
	if ctx.ve != nil {
		v := *ctx.ve
		state.ve = &v
	}
	if ctx.wo != nil {
		v := *ctx.wo
		state.wo = &v
	}
	if ctx.vo != nil {
		v := *ctx.vo
		state.vo = &v
	}
	ctx.savedStates = append(ctx.savedStates, state)
}

func (ctx *context) restoreState() {
	if len(ctx.savedStates) == 0 {
		return
	}
	state := ctx.savedStates[len(ctx.savedStates)-1]
	ctx.savedStates = ctx.savedStates[:len(ctx.savedStates)-1]
	ctx.mm = state.mm
	ctx.textColor = state.textColor
	ctx.bkColor = state.bkColor
	ctx.textAlign = state.textAlign
	ctx.breakExtra = state.breakExtra
	ctx.breakCount = state.breakCount
	ctx.brushOrigin = state.brushOrigin
	ctx.rop2 = state.rop2
	ctx.arcDirection = state.arcDirection
	ctx.miterLimit = state.miterLimit
	ctx.worldTransform = state.worldTransform
	ctx.gdiTransform = state.gdiTransform
	ctx.we = state.we
	ctx.ve = state.ve
	ctx.wo = state.wo
	ctx.vo = state.vo
	ctx.SetMatrixTransform(state.matrix)
}

func (ctx *context) updateCTM() {
	w := ctx.worldTransform
	g := ctx.gdiTransform
	res := [6]float64{
		w[0]*g[0] + w[1]*g[2],
		w[0]*g[1] + w[1]*g[3],
		w[2]*g[0] + w[3]*g[2],
		w[2]*g[1] + w[3]*g[3],
		w[4]*g[0] + w[5]*g[2] + g[4],
		w[4]*g[1] + w[5]*g[3] + g[5],
	}
	ctx.SetMatrixTransform(res)
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

	ctx.gdiTransform = ctx.GetMatrixTransform()

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
