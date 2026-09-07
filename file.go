package emf

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"strings"

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
	if len(data) < 8 {
		return nil, fmt.Errorf("EMF data is too short")
	}
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
	if file.Header == nil {
		return nil, fmt.Errorf("EMF header not found")
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
	fillColor    color.Color
	palette      *PaletteRecord
	textAlign    uint32
	breakExtra   int32
	breakCount   int32
	brushOrigin  PointL
	rop2         uint32
	arcDirection uint32
	miterLimit   float64

	worldTransform   [6]float64
	baseTransform    [6]float64
	gdiTransform     [6]float64
	currentX         float64
	currentY         float64
	hasCurrentPoint  bool
	pathActive       bool
	pathOpen         bool
	pathTransform    [6]float64
	pathTransformSet bool
	savedStates      []contextState
	clipMask         *image.Alpha
}

type contextState struct {
	matrix           [6]float64
	mm               uint32
	textColor        color.Color
	bkColor          color.Color
	fillColor        color.Color
	palette          *PaletteRecord
	textAlign        uint32
	breakExtra       int32
	breakCount       int32
	brushOrigin      PointL
	rop2             uint32
	arcDirection     uint32
	miterLimit       float64
	worldTransform   [6]float64
	baseTransform    [6]float64
	gdiTransform     [6]float64
	currentX         float64
	currentY         float64
	hasCurrentPoint  bool
	pathActive       bool
	pathOpen         bool
	pathTransform    [6]float64
	pathTransformSet bool
	we, ve           *SizeL
	wo, vo           *PointL
	clipMask         *image.Alpha
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
		fillColor:      color.White,
		textAlign:      0,
		rop2:           13, // R2_COPYPEN
		arcDirection:   1,  // AD_COUNTERCLOCKWISE
		miterLimit:     10,
		worldTransform: [6]float64{1, 0, 0, 1, 0, 0},
		baseTransform:  [6]float64{1, 0, 0, 1, 0, 0},
		gdiTransform:   [6]float64{1, 0, 0, 1, 0, 0},
	}
}

func (ctx *context) saveState() {
	state := contextState{
		matrix:           ctx.GetMatrixTransform(),
		mm:               ctx.mm,
		textColor:        ctx.textColor,
		bkColor:          ctx.bkColor,
		fillColor:        ctx.fillColor,
		palette:          ctx.palette,
		textAlign:        ctx.textAlign,
		breakExtra:       ctx.breakExtra,
		breakCount:       ctx.breakCount,
		brushOrigin:      ctx.brushOrigin,
		rop2:             ctx.rop2,
		arcDirection:     ctx.arcDirection,
		miterLimit:       ctx.miterLimit,
		worldTransform:   ctx.worldTransform,
		baseTransform:    ctx.baseTransform,
		gdiTransform:     ctx.gdiTransform,
		currentX:         ctx.currentX,
		currentY:         ctx.currentY,
		hasCurrentPoint:  ctx.hasCurrentPoint,
		pathActive:       ctx.pathActive,
		pathOpen:         ctx.pathOpen,
		pathTransform:    ctx.pathTransform,
		pathTransformSet: ctx.pathTransformSet,
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
	state.clipMask = cloneAlpha(ctx.clipMask)
	ctx.savedStates = append(ctx.savedStates, state)
}

func (ctx *context) restoreState() {
	if len(ctx.savedStates) == 0 {
		return
	}
	ctx.restoreTo(int32(len(ctx.savedStates)))
}

func (ctx *context) restoreTo(savedDC int32) {
	levelCount := len(ctx.savedStates)
	if levelCount == 0 || savedDC == 0 {
		return
	}
	targetLevel := int(savedDC)
	if savedDC < 0 {
		// Preserve the package's established relative-level behavior. When the
		// stack has only one level, restore that level to reach the baseline.
		targetLevel = levelCount + int(savedDC)
		if targetLevel < 1 {
			targetLevel = 1
		}
	}
	if targetLevel < 1 || targetLevel > levelCount {
		return
	}
	state := ctx.savedStates[targetLevel-1]
	graphicRestoreCount := levelCount - targetLevel + 1
	ctx.savedStates = ctx.savedStates[:targetLevel-1]
	for i := 0; i < graphicRestoreCount; i++ {
		ctx.Restore()
	}
	ctx.mm = state.mm
	ctx.textColor = state.textColor
	ctx.bkColor = state.bkColor
	ctx.fillColor = state.fillColor
	ctx.palette = state.palette
	ctx.textAlign = state.textAlign
	ctx.breakExtra = state.breakExtra
	ctx.breakCount = state.breakCount
	ctx.brushOrigin = state.brushOrigin
	ctx.rop2 = state.rop2
	ctx.arcDirection = state.arcDirection
	ctx.miterLimit = state.miterLimit
	ctx.worldTransform = state.worldTransform
	ctx.baseTransform = state.baseTransform
	ctx.gdiTransform = state.gdiTransform
	ctx.currentX = state.currentX
	ctx.currentY = state.currentY
	ctx.hasCurrentPoint = state.hasCurrentPoint
	ctx.pathActive = state.pathActive
	ctx.pathOpen = state.pathOpen
	ctx.pathTransform = state.pathTransform
	ctx.pathTransformSet = state.pathTransformSet
	ctx.we = state.we
	ctx.ve = state.ve
	ctx.wo = state.wo
	ctx.vo = state.vo
	ctx.clipMask = cloneAlpha(state.clipMask)
	ctx.SetMatrixTransform(state.matrix)
}

func cloneAlpha(src *image.Alpha) *image.Alpha {
	if src == nil {
		return nil
	}
	dst := image.NewAlpha(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func (ctx *context) paintWithClip(paint func()) {
	if ctx.clipMask == nil {
		paint()
		return
	}
	bounds := ctx.img.Bounds()
	before := image.NewRGBA(bounds)
	draw.Draw(before, bounds, ctx.img, bounds.Min, draw.Src)
	paint()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			alpha := ctx.clipMask.AlphaAt(x, y).A
			if alpha == 0 {
				ctx.img.Set(x, y, before.At(x, y))
				continue
			}
			if alpha == 0xff {
				continue
			}
			old := color.RGBAModel.Convert(before.At(x, y)).(color.RGBA)
			newColor := color.RGBAModel.Convert(ctx.img.At(x, y)).(color.RGBA)
			ctx.img.Set(x, y, color.RGBA{
				R: uint8((uint16(old.R)*(255-uint16(alpha)) + uint16(newColor.R)*uint16(alpha)) / 255),
				G: uint8((uint16(old.G)*(255-uint16(alpha)) + uint16(newColor.G)*uint16(alpha)) / 255),
				B: uint8((uint16(old.B)*(255-uint16(alpha)) + uint16(newColor.B)*uint16(alpha)) / 255),
				A: uint8((uint16(old.A)*(255-uint16(alpha)) + uint16(newColor.A)*uint16(alpha)) / 255),
			})
		}
	}
}

func (ctx *context) Fill(paths ...*draw2d.Path) {
	if ctx.pathOpen {
		return
	}
	ctx.withPathTransform(func() {
		ctx.paintWithClip(func() { ctx.GraphicContext.Fill(paths...) })
	})
	ctx.pathActive = false
	ctx.pathTransformSet = false
}

func (ctx *context) SetFillColor(c color.Color) {
	ctx.fillColor = c
	ctx.GraphicContext.SetFillColor(c)
}

func (ctx *context) emfMoveTo(x, y float64) {
	ctx.MoveTo(x, y)
}

func (ctx *context) emfLineTo(x, y float64) {
	if !ctx.pathActive {
		if ctx.hasCurrentPoint {
			ctx.MoveTo(ctx.currentX, ctx.currentY)
		} else {
			ctx.MoveTo(x, y)
			ctx.currentX, ctx.currentY = x, y
			ctx.hasCurrentPoint = true
			ctx.pathActive = true
			return
		}
	}
	ctx.LineTo(x, y)
}

func (ctx *context) emfCubicCurveTo(cx1, cy1, cx2, cy2, x, y float64) {
	if !ctx.pathActive {
		if ctx.hasCurrentPoint {
			ctx.MoveTo(ctx.currentX, ctx.currentY)
		} else {
			ctx.MoveTo(x, y)
			ctx.currentX, ctx.currentY = x, y
			ctx.hasCurrentPoint = true
			ctx.pathActive = true
			return
		}
	}
	ctx.CubicCurveTo(cx1, cy1, cx2, cy2, x, y)
}

func (ctx *context) BeginPath() {
	ctx.GraphicContext.BeginPath()
	ctx.pathActive = false
	ctx.pathOpen = true
	ctx.pathTransformSet = false
}

func (ctx *context) EndPath() {
	ctx.pathOpen = false
}

func (ctx *context) MoveTo(x, y float64) {
	ctx.capturePathTransform()
	ctx.GraphicContext.MoveTo(x, y)
	ctx.currentX, ctx.currentY = x, y
	ctx.hasCurrentPoint = true
	ctx.pathActive = true
}

func (ctx *context) LineTo(x, y float64) {
	ctx.capturePathTransform()
	ctx.GraphicContext.LineTo(x, y)
	ctx.currentX, ctx.currentY = x, y
	ctx.hasCurrentPoint = true
	ctx.pathActive = true
}

func (ctx *context) CubicCurveTo(cx1, cy1, cx2, cy2, x, y float64) {
	ctx.capturePathTransform()
	ctx.GraphicContext.CubicCurveTo(cx1, cy1, cx2, cy2, x, y)
	ctx.currentX, ctx.currentY = x, y
	ctx.hasCurrentPoint = true
	ctx.pathActive = true
}

func (ctx *context) ArcTo(cx, cy, rx, ry, startAngle, angle float64) {
	ctx.capturePathTransform()
	ctx.GraphicContext.ArcTo(cx, cy, rx, ry, startAngle, angle)
	endAngle := startAngle + angle
	ctx.currentX = cx + math.Cos(endAngle)*rx
	ctx.currentY = cy + math.Sin(endAngle)*ry
	ctx.hasCurrentPoint = true
	ctx.pathActive = true
}

func (ctx *context) Close() {
	if !ctx.pathActive {
		return
	}
	ctx.GraphicContext.Close()
}

func (ctx *context) Stroke(paths ...*draw2d.Path) {
	if ctx.pathOpen {
		return
	}
	ctx.withPathTransform(func() {
		ctx.paintWithClip(func() { ctx.GraphicContext.Stroke(paths...) })
	})
	ctx.pathActive = false
	ctx.pathTransformSet = false
}

func (ctx *context) FillStroke(paths ...*draw2d.Path) {
	if ctx.pathOpen {
		return
	}
	ctx.withPathTransform(func() {
		ctx.paintWithClip(func() { ctx.GraphicContext.FillStroke(paths...) })
	})
	ctx.pathActive = false
	ctx.pathTransformSet = false
}

func (ctx *context) capturePathTransform() {
	if ctx.pathOpen && !ctx.pathTransformSet {
		ctx.pathTransform = ctx.GetMatrixTransform()
		ctx.pathTransformSet = true
	}
}

func (ctx *context) withPathTransform(paint func()) {
	if !ctx.pathTransformSet {
		paint()
		return
	}
	current := ctx.GetMatrixTransform()
	ctx.SetMatrixTransform(ctx.pathTransform)
	paint()
	ctx.SetMatrixTransform(current)
}

func (ctx *context) FillStringAt(text string, x, y float64) (cursor float64) {
	ctx.paintWithClip(func() { cursor = ctx.GraphicContext.FillStringAt(text, x, y) })
	return cursor
}

func (ctx *context) drawImage(dst image.Rectangle, src image.Image) {
	if ctx.clipMask == nil {
		draw.Draw(ctx.img, dst, src, src.Bounds().Min, draw.Over)
		return
	}
	draw.DrawMask(ctx.img, dst, src, src.Bounds().Min, ctx.clipMask, dst.Min, draw.Over)
}

func (ctx *context) clipAllows(x, y int) bool {
	return ctx.clipMask == nil || ctx.clipMask.AlphaAt(x, y).A != 0
}

func (ctx *context) applyClipMask(mask *image.Alpha, mode uint32) {
	if mask == nil {
		return
	}
	if mode == RGN_COPY || ctx.clipMask == nil && mode == RGN_AND {
		ctx.clipMask = cloneAlpha(mask)
		return
	}
	if ctx.clipMask == nil {
		ctx.clipMask = image.NewAlpha(ctx.img.Bounds())
		for i := range ctx.clipMask.Pix {
			ctx.clipMask.Pix[i] = 0xff
		}
	}
	for i := range ctx.clipMask.Pix {
		left := ctx.clipMask.Pix[i]
		right := mask.Pix[i]
		switch mode {
		case RGN_OR:
			if right > left {
				ctx.clipMask.Pix[i] = right
			}
		case RGN_XOR:
			if left > right {
				ctx.clipMask.Pix[i] = left - right
			} else {
				ctx.clipMask.Pix[i] = right - left
			}
		case RGN_DIFF:
			if right >= left {
				ctx.clipMask.Pix[i] = 0
			} else {
				ctx.clipMask.Pix[i] = left - right
			}
		default:
			ctx.clipMask.Pix[i] = uint8(uint16(left) * uint16(right) / 255)
		}
	}
}

func (ctx *context) clipRect(rect RectL) *image.Alpha {
	mask := image.NewAlpha(ctx.img.Bounds())
	x1, y1 := transformPoint(ctx, float64(rect.Left), float64(rect.Top))
	x2, y2 := transformPoint(ctx, float64(rect.Right), float64(rect.Bottom))
	return fillClipRect(mask, x1, y1, x2, y2)
}

// IntersectClipRect and ExcludeClipRect are recorded in device coordinates by
// the EMF producers handled here. Applying the current world transform again
// collapses these rectangles when the record is inside a transformed DC.
func (ctx *context) clipRectDevice(rect RectL) *image.Alpha {
	mask := image.NewAlpha(ctx.img.Bounds())
	return fillClipRect(mask, int(rect.Left), int(rect.Top), int(rect.Right), int(rect.Bottom))
}

func fillClipRect(mask *image.Alpha, x1, y1, x2, y2 int) *image.Alpha {
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	for y := y1; y < y2; y++ {
		for x := x1; x < x2; x++ {
			if (image.Point{X: x, Y: y}).In(mask.Bounds()) {
				mask.SetAlpha(x, y, color.Alpha{A: 0xff})
			}
		}
	}
	return mask
}

func (ctx *context) clipPath() *image.Alpha {
	path := ctx.GetPath()
	mask := image.NewAlpha(ctx.img.Bounds())
	rasterImage := image.NewRGBA(ctx.img.Bounds())
	gc := draw2dimg.NewGraphicContext(rasterImage)
	transform := ctx.GetMatrixTransform()
	if ctx.pathTransformSet {
		transform = ctx.pathTransform
	}
	gc.SetMatrixTransform(transform)
	gc.SetFillColor(color.White)
	gc.Fill(&path)
	for y := mask.Bounds().Min.Y; y < mask.Bounds().Max.Y; y++ {
		for x := mask.Bounds().Min.X; x < mask.Bounds().Max.X; x++ {
			_, _, _, alpha := rasterImage.At(x, y).RGBA()
			if alpha != 0 {
				mask.SetAlpha(x, y, color.Alpha{A: uint8(alpha >> 8)})
			}
		}
	}
	return mask
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

func multiplyTransform(a, b [6]float64) [6]float64 {
	return [6]float64{
		a[0]*b[0] + a[1]*b[2],
		a[0]*b[1] + a[1]*b[3],
		a[2]*b[0] + a[3]*b[2],
		a[2]*b[1] + a[3]*b[3],
		a[4]*b[0] + a[5]*b[2] + b[4],
		a[4]*b[1] + a[5]*b[3] + b[5],
	}
}

func (ctx *context) updateMapping() {
	mapping := [6]float64{1, 0, 0, 1, 0, 0}
	if ctx.we != nil && ctx.ve != nil && ctx.we.Cx != 0 && ctx.we.Cy != 0 {
		sx := float64(ctx.ve.Cx) / float64(ctx.we.Cx)
		sy := float64(ctx.ve.Cy) / float64(ctx.we.Cy)
		wo, woY := int32(0), int32(0)
		vo, voY := int32(0), int32(0)
		if ctx.wo != nil {
			wo, woY = ctx.wo.X, ctx.wo.Y
		}
		if ctx.vo != nil {
			vo, voY = ctx.vo.X, ctx.vo.Y
		}
		mapping = [6]float64{sx, 0, 0, sy, float64(vo) - float64(wo)*sx, float64(voY) - float64(woY)*sy}
	}
	if ctx.mm == MM_LOMETRIC || ctx.mm == MM_HIMETRIC || ctx.mm == MM_LOENGLISH || ctx.mm == MM_HIENGLISH || ctx.mm == MM_TWIPS {
		mapping[3] = -mapping[3]
		mapping[5] = float64(ctx.h) - mapping[5]
	}
	ctx.gdiTransform = multiplyTransform(ctx.baseTransform, mapping)
	ctx.updateCTM()
}

func (f *EmfFile) Draw() image.Image {
	if f == nil || f.Header == nil {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}

	bounds := f.Header.Bounds

	// inclusive-inclusive bounds
	width := int(bounds.Width()) + 1
	height := int(bounds.Height()) + 1
	if width <= 0 || height <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}

	ctx := f.initContext(width, height)

	ctx.baseTransform = ctx.GetMatrixTransform()
	ctx.gdiTransform = ctx.baseTransform

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
	named       map[string]fontFaces
}

type fontFaces struct {
	regular *truetype.Font
	bold    *truetype.Font
	italic  *truetype.Font
	bi      *truetype.Font
}

func (c *FallbackFontCache) Load(fd draw2d.FontData) (*truetype.Font, error) {
	isBold := fd.Style&draw2d.FontStyleBold != 0
	isItalic := fd.Style&draw2d.FontStyleItalic != 0
	faces, ok := c.named[strings.ToLower(strings.TrimSpace(fd.Name))]
	if ok {
		if font := selectFontFace(faces, isBold, isItalic); font != nil {
			return font, nil
		}
	}

	font := selectFontFace(fontFaces{
		regular: c.defaultFont,
		bold:    c.boldFont,
		italic:  c.italicFont,
		bi:      c.biFont,
	}, isBold, isItalic)
	if font != nil {
		return font, nil
	}
	return c.defaultFont, nil
}

func selectFontFace(faces fontFaces, isBold, isItalic bool) *truetype.Font {
	if isBold && isItalic && faces.bi != nil {
		return faces.bi
	}
	if isBold && faces.bold != nil {
		return faces.bold
	}
	if isItalic && faces.italic != nil {
		return faces.italic
	}
	return faces.regular
}

func (c *FallbackFontCache) Store(fd draw2d.FontData, font *truetype.Font) {}

func loadFont(paths ...string) *truetype.Font {
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		font, err := truetype.Parse(b)
		if err == nil {
			return font
		}
	}
	return nil
}

func init() {
	defaultFont := loadFont(
		// Keep the bundled font first so Chinese text remains available when
		// the converter is run outside Windows.
		"NotoSansSC-VF.ttf",
		"C:\\Windows\\Fonts\\NotoSansSC-VF.ttf",
		"C:\\Windows\\Fonts\\arial.ttf",
		"/Library/Fonts/Arial.ttf",
		"/Library/Fonts/Microsoft/Arial.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
	)
	if defaultFont == nil {
		return
	}

	// Do not alias every style to the regular face. EMF records carry the
	// requested weight, and draw2d uses this cache to select the matching face.
	boldFont := loadFont(
		"C:\\Windows\\Fonts\\Noto Sans SC Bold (TrueType).otf",
		"C:\\Windows\\Fonts\\NotoSansSC-Bold.ttf",
		"C:\\Windows\\Fonts\\arialbd.ttf",
	)
	italicFont := loadFont(
		"C:\\Windows\\Fonts\\NotoSansSC-Italic.ttf",
		"C:\\Windows\\Fonts\\ariali.ttf",
	)
	biFont := loadFont(
		"C:\\Windows\\Fonts\\NotoSansSC-BoldItalic.ttf",
		"C:\\Windows\\Fonts\\arialbi.ttf",
	)
	if boldFont == nil {
		boldFont = defaultFont
	}
	if italicFont == nil {
		italicFont = defaultFont
	}
	if biFont == nil {
		biFont = boldFont
	}

	named := make(map[string]fontFaces)
	calibri := fontFaces{
		regular: loadFont("C:\\Windows\\Fonts\\calibri.ttf"),
		bold:    loadFont("C:\\Windows\\Fonts\\calibrib.ttf"),
		italic:  loadFont("C:\\Windows\\Fonts\\calibrii.ttf"),
		bi:      loadFont("C:\\Windows\\Fonts\\calibriz.ttf"),
	}
	if calibri.regular != nil {
		named["calibri"] = calibri
	}
	arial := fontFaces{
		regular: loadFont("C:\\Windows\\Fonts\\arial.ttf"),
		bold:    loadFont("C:\\Windows\\Fonts\\arialbd.ttf"),
		italic:  loadFont("C:\\Windows\\Fonts\\ariali.ttf"),
		bi:      loadFont("C:\\Windows\\Fonts\\arialbi.ttf"),
	}
	if arial.regular != nil {
		named["arial"] = arial
	}

	draw2d.SetFontCache(&FallbackFontCache{
		defaultFont: defaultFont,
		boldFont:    boldFont,
		italicFont:  italicFont,
		biFont:      biFont,
		named:       named,
	})
}
