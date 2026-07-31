package emf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"unicode/utf16"

	"github.com/llgcode/draw2d"
)

// RawRecord keeps an unimplemented record in the parsed stream. This makes
// conversion best-effort for optional or vendor-specific EMF records.
type RawRecord struct {
	Record
}

func (r *RawRecord) Draw(ctx *context) {}

func applyPenStyle(ctx *context, style uint32, width float64) {
	switch style & 0x0f {
	case PS_NULL:
		ctx.SetStrokeColor(color.Transparent)
	case PS_DASH:
		ctx.SetLineDash([]float64{6 * width, 3 * width}, 0)
	case PS_DOT:
		ctx.SetLineDash([]float64{width, 2 * width}, 0)
	case PS_DASHDOT:
		ctx.SetLineDash([]float64{6 * width, 3 * width, width, 3 * width}, 0)
	case PS_DASHDOTDOT:
		ctx.SetLineDash([]float64{6 * width, 3 * width, width, 3 * width, width, 3 * width}, 0)
	default:
		ctx.SetLineDash(nil, 0)
	}
	switch style & 0x0f00 {
	case PS_ENDCAP_SQUARE:
		ctx.SetLineCap(draw2d.SquareCap)
	case PS_ENDCAP_FLAT:
		ctx.SetLineCap(draw2d.ButtCap)
	default:
		ctx.SetLineCap(draw2d.RoundCap)
	}
	switch style & 0xf000 {
	case PS_JOIN_BEVEL:
		ctx.SetLineJoin(draw2d.BevelJoin)
	case PS_JOIN_MITER:
		ctx.SetLineJoin(draw2d.MiterJoin)
	default:
		ctx.SetLineJoin(draw2d.RoundJoin)
	}
}

const (
	gradientFillRectH    = 0
	gradientFillRectV    = 1
	gradientFillTriangle = 2
)

type TriVertex struct {
	X, Y             int32
	Red, Green, Blue uint16
	Alpha            uint16
}

type GradientFillRecord struct {
	Record
	Bounds   RectL
	Vertices []TriVertex
	Indices  []uint32
	Mode     uint32
}

func readGradientFillRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &GradientFillRecord{Record: Record{Type: EMR_GRADIENTFILL, Size: size}}
	var vertexCount, meshCount uint32
	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &vertexCount); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &meshCount); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Mode); err != nil {
		return nil, err
	}
	r.Vertices = make([]TriVertex, vertexCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.Vertices); err != nil {
		return nil, err
	}
	indicesPerMesh := uint32(3)
	if r.Mode == gradientFillRectH || r.Mode == gradientFillRectV {
		indicesPerMesh = 2
	}
	r.Indices = make([]uint32, meshCount*indicesPerMesh)
	if err := binary.Read(reader, binary.LittleEndian, &r.Indices); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *GradientFillRecord) Draw(ctx *context) {
	if r.Mode == gradientFillRectH || r.Mode == gradientFillRectV {
		for i := 0; i+1 < len(r.Indices); i += 2 {
			left, okLeft := gradientVertex(r.Vertices, r.Indices[i])
			right, okRight := gradientVertex(r.Vertices, r.Indices[i+1])
			if okLeft && okRight {
				drawGradientRect(ctx, left, right, r.Mode == gradientFillRectV)
			}
		}
		return
	}
	for i := 0; i+2 < len(r.Indices); i += 3 {
		v0, ok0 := gradientVertex(r.Vertices, r.Indices[i])
		v1, ok1 := gradientVertex(r.Vertices, r.Indices[i+1])
		v2, ok2 := gradientVertex(r.Vertices, r.Indices[i+2])
		if ok0 && ok1 && ok2 {
			drawGradientTriangle(ctx, v0, v1, v2)
		}
	}
}

func gradientVertex(vertices []TriVertex, index uint32) (TriVertex, bool) {
	if index >= uint32(len(vertices)) {
		return TriVertex{}, false
	}
	return vertices[index], true
}

func gradientColor(vertex TriVertex) color.RGBA {
	return color.RGBA{R: uint8(vertex.Red >> 8), G: uint8(vertex.Green >> 8), B: uint8(vertex.Blue >> 8), A: uint8(vertex.Alpha >> 8)}
}

func interpolateColor(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return color.RGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: uint8(float64(a.A) + (float64(b.A)-float64(a.A))*t),
	}
}

func drawGradientRect(ctx *context, a, b TriVertex, vertical bool) {
	x1, y1 := transformPoint(ctx, float64(a.X), float64(a.Y))
	x2, y2 := transformPoint(ctx, float64(b.X), float64(b.Y))
	left, right := x1, x2
	top, bottom := y1, y2
	if left > right {
		left, right = right, left
	}
	if top > bottom {
		top, bottom = bottom, top
	}
	if right <= left || bottom <= top {
		return
	}
	start, end := gradientColor(a), gradientColor(b)
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			t := float64(x-left) / float64(right-left)
			if vertical {
				t = float64(y-top) / float64(bottom-top)
			}
			setImagePixel(ctx, x, y, interpolateColor(start, end, t))
		}
	}
}

func drawGradientTriangle(ctx *context, a, b, c TriVertex) {
	ax, ay := transformPoint(ctx, float64(a.X), float64(a.Y))
	bx, by := transformPoint(ctx, float64(b.X), float64(b.Y))
	cx, cy := transformPoint(ctx, float64(c.X), float64(c.Y))
	minX := minInt(ax, minInt(bx, cx))
	maxX := maxInt(ax, maxInt(bx, cx))
	minY := minInt(ay, minInt(by, cy))
	maxY := maxInt(ay, maxInt(by, cy))
	denominator := float64((by-cy)*(ax-cx) + (cx-bx)*(ay-cy))
	if denominator == 0 {
		return
	}
	ca, cb, cc := gradientColor(a), gradientColor(b), gradientColor(c)
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			wa := float64((by-cy)*(x-cx)+(cx-bx)*(y-cy)) / denominator
			wb := float64((cy-ay)*(x-cx)+(ax-cx)*(y-cy)) / denominator
			wc := 1 - wa - wb
			if wa < 0 || wb < 0 || wc < 0 {
				continue
			}
			setImagePixel(ctx, x, y, color.RGBA{
				R: uint8(wa*float64(ca.R) + wb*float64(cb.R) + wc*float64(cc.R)),
				G: uint8(wa*float64(ca.G) + wb*float64(cb.G) + wc*float64(cc.G)),
				B: uint8(wa*float64(ca.B) + wb*float64(cb.B) + wc*float64(cc.B)),
				A: uint8(wa*float64(ca.A) + wb*float64(cb.A) + wc*float64(cc.A)),
			})
		}
	}
}

func setImagePixel(ctx *context, x, y int, c color.Color) {
	if imagePointInBounds(ctx, x, y) {
		ctx.img.Set(x, y, c)
	}
}

func imagePointInBounds(ctx *context, x, y int) bool {
	bounds := ctx.img.Bounds()
	return x >= bounds.Min.X && x < bounds.Max.X && y >= bounds.Min.Y && y < bounds.Max.Y
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func parseEMRText(header, payload []byte, wide bool) (EmrText, error) {
	var result EmrText
	if len(header) < 40 {
		return result, fmt.Errorf("short EMRTEXT header: %d", len(header))
	}
	reader := bytes.NewReader(header)
	if err := binary.Read(reader, binary.LittleEndian, &result.Reference); err != nil {
		return result, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &result.Chars); err != nil {
		return result, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &result.offString); err != nil {
		return result, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &result.Options); err != nil {
		return result, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &result.Rectangle); err != nil {
		return result, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &result.offDx); err != nil {
		return result, err
	}

	stringOffset := int64(result.offString) - 8
	if result.Chars > uint32(len(payload)) {
		return result, fmt.Errorf("invalid EMRTEXT character count %d", result.Chars)
	}
	if stringOffset >= 0 && stringOffset <= int64(len(payload)) {
		if wide {
			byteCount := int64(result.Chars) * 2
			if stringOffset+byteCount > int64(len(payload)) {
				return result, io.ErrUnexpectedEOF
			}
			data := make([]uint16, result.Chars)
			if err := binary.Read(bytes.NewReader(payload[stringOffset:stringOffset+byteCount]), binary.LittleEndian, &data); err != nil {
				return result, err
			}
			result.OutputString = string(utf16.Decode(data))
		} else {
			end := stringOffset + int64(result.Chars)
			if end > int64(len(payload)) {
				return result, io.ErrUnexpectedEOF
			}
			result.OutputString = string(payload[stringOffset:end])
		}
	}

	dxOffset := int64(result.offDx) - 8
	if result.offDx != 0 && dxOffset >= 0 && dxOffset < int64(len(payload)) {
		count := int(result.Chars)
		if result.Options&ETO_PDY != 0 {
			count *= 2
		}
		available := (len(payload) - int(dxOffset)) / 4
		if count > available {
			count = available
		}
		result.OutputDx = make([]uint32, count)
		if err := binary.Read(bytes.NewReader(payload[dxOffset:]), binary.LittleEndian, &result.OutputDx); err != nil {
			return result, err
		}
	}
	return result, nil
}

type ExttextoutaRecord struct {
	Record
	Bounds        RectL
	iGraphicsMode uint32
	exScale       float32
	eyScale       float32
	Text          EmrText
}

func readExttextoutaRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ExttextoutaRecord{Record: Record{Type: EMR_EXTTEXTOUTA, Size: size}}
	payload := make([]byte, int(size)-8)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	payloadReader := bytes.NewReader(payload)
	if err := binary.Read(payloadReader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	if err := binary.Read(payloadReader, binary.LittleEndian, &r.iGraphicsMode); err != nil {
		return nil, err
	}
	if err := binary.Read(payloadReader, binary.LittleEndian, &r.exScale); err != nil {
		return nil, err
	}
	if err := binary.Read(payloadReader, binary.LittleEndian, &r.eyScale); err != nil {
		return nil, err
	}
	var err error
	r.Text, err = parseEMRText(payload[28:], payload, false)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (r *ExttextoutaRecord) Draw(ctx *context) {
	drawEMRText(ctx, r.Text)
}

type PolytextoutRecord struct {
	Record
	Bounds        RectL
	iGraphicsMode uint32
	exScale       float32
	eyScale       float32
	Texts         []EmrText
	wide          bool
}

func readPolytextoutRecord(reader *bytes.Reader, size uint32, typ uint32, wide bool) (Recorder, error) {
	r := &PolytextoutRecord{Record: Record{Type: typ, Size: size}, wide: wide}
	payload := make([]byte, int(size)-8)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	payloadReader := bytes.NewReader(payload)
	if err := binary.Read(payloadReader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	if err := binary.Read(payloadReader, binary.LittleEndian, &r.iGraphicsMode); err != nil {
		return nil, err
	}
	if err := binary.Read(payloadReader, binary.LittleEndian, &r.exScale); err != nil {
		return nil, err
	}
	if err := binary.Read(payloadReader, binary.LittleEndian, &r.eyScale); err != nil {
		return nil, err
	}
	var count uint32
	if err := binary.Read(payloadReader, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	r.Texts = make([]EmrText, 0, count)
	for i := uint32(0); i < count; i++ {
		start := 32 + int(i)*40
		if start+40 > len(payload) {
			return nil, io.ErrUnexpectedEOF
		}
		text, err := parseEMRText(payload[start:start+40], payload, wide)
		if err != nil {
			return nil, err
		}
		r.Texts = append(r.Texts, text)
	}
	return r, nil
}

func readPolytextoutaRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	return readPolytextoutRecord(reader, size, EMR_POLYTEXTOUTA, false)
}

func readPolytextoutwRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	return readPolytextoutRecord(reader, size, EMR_POLYTEXTOUTW, true)
}

func (r *PolytextoutRecord) Draw(ctx *context) {
	for _, text := range r.Texts {
		drawEMRText(ctx, text)
	}
}

func drawEMRText(ctx *context, text EmrText) {
	tr := ctx.GetMatrixTransform()
	x := float64(text.Reference.X)
	y := float64(text.Reference.Y)
	nx := x*tr[0] + y*tr[2] + tr[4]
	ny := x*tr[1] + y*tr[3] + tr[5]
	sx := math.Hypot(tr[0], tr[1])
	sy := math.Hypot(tr[2], tr[3])
	if sx < 0.0001 {
		sx = 1
	}
	if sy < 0.0001 {
		sy = 1
	}

	ctx.SetMatrixTransform([6]float64{sx, 0, 0, sy, nx, ny})
	if text.Options&ETO_OPAQUE != 0 {
		ctx.SetFillColor(ctx.bkColor)
		ctx.MoveTo(float64(text.Rectangle.Left), float64(text.Rectangle.Top))
		ctx.LineTo(float64(text.Rectangle.Right), float64(text.Rectangle.Top))
		ctx.LineTo(float64(text.Rectangle.Right), float64(text.Rectangle.Bottom))
		ctx.LineTo(float64(text.Rectangle.Left), float64(text.Rectangle.Bottom))
		ctx.Close()
		ctx.Fill()
	}
	ctx.SetFillColor(ctx.textColor)
	if len(text.OutputDx) >= len([]rune(text.OutputString)) && len(text.OutputDx) > 0 {
		cursor := 0.0
		for i, ch := range []rune(text.OutputString) {
			ctx.FillStringAt(string(ch), cursor, 0)
			cursor += float64(text.OutputDx[i])
		}
	} else {
		ctx.FillStringAt(text.OutputString, 0, 0)
	}
	ctx.SetMatrixTransform(tr)
}

type SetbrushorgexRecord struct {
	Record
	Origin PointL
}

func readSetbrushorgexRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetbrushorgexRecord{Record: Record{Type: EMR_SETBRUSHORGEX, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Origin); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *SetbrushorgexRecord) Draw(ctx *context) {
	ctx.brushOrigin = r.Origin
}

type SetpixelvRecord struct {
	Record
	Point PointL
	Color ColorRef
}

type SettextjustificationRecord struct {
	Record
	BreakExtra int32
	BreakCount int32
}

func readSettextjustificationRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SettextjustificationRecord{Record: Record{Type: EMR_SETTEXTJUSTIFICATION, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.BreakExtra); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.BreakCount); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *SettextjustificationRecord) Draw(ctx *context) {
	ctx.breakExtra = r.BreakExtra
	ctx.breakCount = r.BreakCount
}

type ExtfloodfillRecord struct {
	Record
	Point PointL
	Color ColorRef
	Mode  uint32
}

func readExtfloodfillRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ExtfloodfillRecord{Record: Record{Type: EMR_EXTFLOODFILL, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Point); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Color); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Mode); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *ExtfloodfillRecord) Draw(ctx *context) {
	startX, startY := transformPoint(ctx, float64(r.Point.X), float64(r.Point.Y))
	if !imagePointInBounds(ctx, startX, startY) {
		return
	}
	target := color.NRGBAModel.Convert(ctx.img.At(startX, startY)).(color.NRGBA)
	fill := color.NRGBA{R: r.Color.Red, G: r.Color.Green, B: r.Color.Blue, A: 0xff}
	if target == fill {
		return
	}
	stack := []image.Point{{X: startX, Y: startY}}
	for len(stack) > 0 {
		last := len(stack) - 1
		point := stack[last]
		stack = stack[:last]
		if !imagePointInBounds(ctx, point.X, point.Y) {
			continue
		}
		current := color.NRGBAModel.Convert(ctx.img.At(point.X, point.Y)).(color.NRGBA)
		if r.Mode == 0 {
			if current == fill || current.R == r.Color.Red && current.G == r.Color.Green && current.B == r.Color.Blue {
				continue
			}
		} else if current != target {
			continue
		}
		ctx.img.Set(point.X, point.Y, fill)
		stack = append(stack,
			image.Point{X: point.X - 1, Y: point.Y},
			image.Point{X: point.X + 1, Y: point.Y},
			image.Point{X: point.X, Y: point.Y - 1},
			image.Point{X: point.X, Y: point.Y + 1},
		)
	}
}

func readSetpixelvRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetpixelvRecord{Record: Record{Type: EMR_SETPIXELV, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Point); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Color); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *SetpixelvRecord) Draw(ctx *context) {
	x, y := transformPoint(ctx, float64(r.Point.X), float64(r.Point.Y))
	ctx.img.Set(x, y, r.Color.GetColor())
}

type Setrop2Record struct {
	Record
	Mode uint32
}

func readSetrop2Record(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &Setrop2Record{Record: Record{Type: EMR_SETROP2, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Mode); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Setrop2Record) Draw(ctx *context) {
	ctx.rop2 = r.Mode
}

type SetmiterlimitRecord struct {
	Record
	Limit uint32
}

func readSetmiterlimitRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetmiterlimitRecord{Record: Record{Type: EMR_SETMITERLIMIT, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Limit); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *SetmiterlimitRecord) Draw(ctx *context) {
	ctx.miterLimit = float64(r.Limit)
}

type SetarcdirectionRecord struct {
	Record
	Direction uint32
}

func readSetarcdirectionRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetarcdirectionRecord{Record: Record{Type: EMR_SETARCDIRECTION, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Direction); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *SetarcdirectionRecord) Draw(ctx *context) {
	ctx.arcDirection = r.Direction
}

type OffsetcliprgnRecord struct {
	Record
	Offset PointL
}

type SetmetargnRecord struct {
	Record
}

func readSetmetargnRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	return &SetmetargnRecord{Record: Record{Type: EMR_SETMETARGN, Size: size}}, nil
}

type ExtselectcliprgnRecord struct {
	Record
	RegionDataSize uint32
	Mode           uint32
	RegionData     []byte
}

func readExtselectcliprgnRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ExtselectcliprgnRecord{Record: Record{Type: EMR_EXTSELECTCLIPRGN, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.RegionDataSize); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Mode); err != nil {
		return nil, err
	}
	if r.RegionDataSize > 0 {
		r.RegionData = make([]byte, r.RegionDataSize)
		if _, err := reader.Read(r.RegionData); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func readOffsetcliprgnRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &OffsetcliprgnRecord{Record: Record{Type: EMR_OFFSETCLIPRGN, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Offset); err != nil {
		return nil, err
	}
	return r, nil
}

type ExcludecliprectRecord struct {
	Record
	Rect RectL
}

func readExcludecliprectRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ExcludecliprectRecord{Record: Record{Type: EMR_EXCLUDECLIPRECT, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Rect); err != nil {
		return nil, err
	}
	return r, nil
}

type ScalewindowextexRecord struct {
	Record
	XNum, XDen, YNum, YDen int32
}

func readScalewindowextexRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ScalewindowextexRecord{Record: Record{Type: EMR_SCALEWINDOWEXTEX, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.XNum); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.XDen); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.YNum); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.YDen); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *ScalewindowextexRecord) Draw(ctx *context) {
	if r.XNum != 0 && r.YNum != 0 && r.XDen != 0 && r.YDen != 0 {
		ctx.Scale(float64(r.XDen)/float64(r.XNum), float64(r.YDen)/float64(r.YNum))
	}
}

type ScaleviewportextexRecord struct {
	Record
	XNum, XDen, YNum, YDen int32
}

func readScaleviewportextexRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ScaleviewportextexRecord{Record: Record{Type: EMR_SCALEVIEWPORTEXTEX, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.XNum); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.XDen); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.YNum); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.YDen); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *ScaleviewportextexRecord) Draw(ctx *context) {
	if r.XNum != 0 && r.YNum != 0 && r.XDen != 0 && r.YDen != 0 {
		ctx.Scale(float64(r.XNum)/float64(r.XDen), float64(r.YNum)/float64(r.YDen))
	}
}

type PolybezierRecord struct {
	Record
	Bounds RectL
	Count  uint32
	Points []PointL
}

func readPolybezierRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &PolybezierRecord{Record: Record{Type: EMR_POLYBEZIER, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}
	r.Points = make([]PointL, r.Count)
	if err := binary.Read(reader, binary.LittleEndian, &r.Points); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *PolybezierRecord) Draw(ctx *context) {
	if r.Count < 4 {
		return
	}
	ctx.MoveTo(float64(r.Points[0].X), float64(r.Points[0].Y))
	for i := 1; i+2 < int(r.Count); i += 3 {
		ctx.CubicCurveTo(
			float64(r.Points[i].X), float64(r.Points[i].Y),
			float64(r.Points[i+1].X), float64(r.Points[i+1].Y),
			float64(r.Points[i+2].X), float64(r.Points[i+2].Y),
		)
	}
	ctx.Stroke()
}

type PolybeziertoRecord struct {
	Record
	Bounds RectL
	Count  uint32
	Points []PointL
}

const (
	ptCloseFigure = 0x01
	ptLineTo      = 0x02
	ptBezierTo    = 0x04
	ptMoveTo      = 0x06
)

type PolydrawRecord struct {
	Record
	Bounds PointRect
	Count  uint32
	Points []PointL
	Types  []byte
}

// PointRect is kept local to the record parser to avoid coupling the public
// geometry types to the EMF record layout.
type PointRect = RectL

func readPolydrawRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &PolydrawRecord{Record: Record{Type: EMR_POLYDRAW, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}
	r.Points = make([]PointL, r.Count)
	if err := binary.Read(reader, binary.LittleEndian, &r.Points); err != nil {
		return nil, err
	}
	r.Types = make([]byte, r.Count)
	if _, err := reader.Read(r.Types); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *PolydrawRecord) Draw(ctx *context) {
	drawPolyDraw(ctx, r.Points, r.Types)
}

type Polydraw16Record struct {
	Record
	Bounds PointRect
	Count  uint32
	Points []PointS
	Types  []byte
}

func readPolydraw16Record(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &Polydraw16Record{Record: Record{Type: EMR_POLYDRAW16, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}
	r.Points = make([]PointS, r.Count)
	if err := binary.Read(reader, binary.LittleEndian, &r.Points); err != nil {
		return nil, err
	}
	r.Types = make([]byte, r.Count)
	if _, err := reader.Read(r.Types); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Polydraw16Record) Draw(ctx *context) {
	points := make([]PointL, len(r.Points))
	for i, point := range r.Points {
		points[i] = PointL{X: int32(point.X), Y: int32(point.Y)}
	}
	drawPolyDraw(ctx, points, r.Types)
}

func drawPolyDraw(ctx *context, points []PointL, types []byte) {
	for i := 0; i < len(points) && i < len(types); {
		typ := types[i]
		switch typ & 0x06 {
		case ptMoveTo:
			ctx.MoveTo(float64(points[i].X), float64(points[i].Y))
			i++
		case ptLineTo:
			ctx.LineTo(float64(points[i].X), float64(points[i].Y))
			i++
		case ptBezierTo:
			if i+2 >= len(points) || i+2 >= len(types) {
				i = len(points)
				break
			}
			ctx.CubicCurveTo(
				float64(points[i].X), float64(points[i].Y),
				float64(points[i+1].X), float64(points[i+1].Y),
				float64(points[i+2].X), float64(points[i+2].Y),
			)
			i += 3
		default:
			i++
		}
		if typ&ptCloseFigure != 0 {
			ctx.Close()
		}
	}
	ctx.Stroke()
}

func readPolybeziertoRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &PolybeziertoRecord{Record: Record{Type: EMR_POLYBEZIERTO, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}
	r.Points = make([]PointL, r.Count)
	if err := binary.Read(reader, binary.LittleEndian, &r.Points); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *PolybeziertoRecord) Draw(ctx *context) {
	for i := 0; i+2 < int(r.Count); i += 3 {
		ctx.CubicCurveTo(
			float64(r.Points[i].X), float64(r.Points[i].Y),
			float64(r.Points[i+1].X), float64(r.Points[i+1].Y),
			float64(r.Points[i+2].X), float64(r.Points[i+2].Y),
		)
	}
	ctx.Stroke()
}

type Polypolyline16Record struct {
	Record
	Bounds            RectL
	NumberOfPolylines uint32
	Count             uint32
	PointCounts       []uint32
	Points            []PointS
}

func readPolypolyline16Record(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &Polypolyline16Record{Record: Record{Type: EMR_POLYPOLYLINE16, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.NumberOfPolylines); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}
	r.PointCounts = make([]uint32, r.NumberOfPolylines)
	if err := binary.Read(reader, binary.LittleEndian, &r.PointCounts); err != nil {
		return nil, err
	}
	r.Points = make([]PointS, r.Count)
	if err := binary.Read(reader, binary.LittleEndian, &r.Points); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Polypolyline16Record) Draw(ctx *context) {
	idx := 0
	for _, count := range r.PointCounts {
		if count > 0 && idx < len(r.Points) {
			end := idx + int(count)
			if end > len(r.Points) {
				end = len(r.Points)
			}
			ctx.MoveTo(float64(r.Points[idx].X), float64(r.Points[idx].Y))
			for i := idx + 1; i < end; i++ {
				ctx.LineTo(float64(r.Points[i].X), float64(r.Points[i].Y))
			}
			ctx.Stroke()
			idx = end
		}
	}
}

type RoundrectRecord struct {
	Record
	Box    RectL
	Corner SizeL
}

func readRoundrectRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &RoundrectRecord{Record: Record{Type: EMR_ROUNDRECT, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Box); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Corner); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *RoundrectRecord) Draw(ctx *context) {
	drawRoundedRect(ctx, r.Box, float64(r.Corner.Cx)/2, float64(r.Corner.Cy)/2)
	ctx.FillStroke()
}

type ChordRecord struct {
	Record
	Box   RectL
	Start PointL
	End   PointL
}

type ArctoRecord struct {
	Record
	Box   RectL
	Start PointL
	End   PointL
}

func readArctoRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ArctoRecord{Record: Record{Type: EMR_ARCTO, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Box); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Start); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.End); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *ArctoRecord) Draw(ctx *context) {
	center, rx, ry, start, sweep := arcGeometry(ctx, r.Box, r.Start, r.End)
	ctx.LineTo(float64(r.Start.X), float64(r.Start.Y))
	ctx.ArcTo(float64(center.X), float64(center.Y), rx, ry, start, sweep)
	ctx.Stroke()
}

func readChordRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ChordRecord{Record: Record{Type: EMR_CHORD, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Box); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Start); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.End); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *ChordRecord) Draw(ctx *context) {
	center, rx, ry, start, sweep := arcGeometry(ctx, r.Box, r.Start, r.End)
	ctx.MoveTo(float64(r.Start.X), float64(r.Start.Y))
	ctx.ArcTo(float64(center.X), float64(center.Y), rx, ry, start, sweep)
	ctx.LineTo(float64(r.End.X), float64(r.End.Y))
	ctx.Close()
	ctx.FillStroke()
}

type PieRecord struct {
	Record
	Box   RectL
	Start PointL
	End   PointL
}

func readPieRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &PieRecord{Record: Record{Type: EMR_PIE, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Box); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Start); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.End); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *PieRecord) Draw(ctx *context) {
	center, rx, ry, start, sweep := arcGeometry(ctx, r.Box, r.Start, r.End)
	ctx.MoveTo(float64(center.X), float64(center.Y))
	ctx.LineTo(float64(r.Start.X), float64(r.Start.Y))
	ctx.ArcTo(float64(center.X), float64(center.Y), rx, ry, start, sweep)
	ctx.LineTo(float64(center.X), float64(center.Y))
	ctx.Close()
	ctx.FillStroke()
}

type AnglearcRecord struct {
	Record
	Center                 PointL
	Radius                 int32
	StartAngle, SweepAngle float32
}

func readAnglearcRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &AnglearcRecord{Record: Record{Type: EMR_ANGLEARC, Size: size}}
	if err := binary.Read(reader, binary.LittleEndian, &r.Center); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Radius); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.StartAngle); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.SweepAngle); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *AnglearcRecord) Draw(ctx *context) {
	start := float64(r.StartAngle) * math.Pi / 180
	sweep := float64(r.SweepAngle) * math.Pi / 180
	x := float64(r.Center.X) + math.Cos(start)*float64(r.Radius)
	y := float64(r.Center.Y) + math.Sin(start)*float64(r.Radius)
	ctx.LineTo(x, y)
	ctx.ArcTo(float64(r.Center.X), float64(r.Center.Y), float64(r.Radius), float64(r.Radius), start, sweep)
	ctx.Stroke()
}

func transformPoint(ctx *context, x, y float64) (int, int) {
	tr := ctx.GetMatrixTransform()
	return int(math.Round(x*tr[0] + y*tr[2] + tr[4])), int(math.Round(x*tr[1] + y*tr[3] + tr[5]))
}

func arcGeometry(ctx *context, box RectL, startPoint, endPoint PointL) (PointL, float64, float64, float64, float64) {
	center := box.Center()
	rx := (float64(box.Right) - float64(box.Left)) / 2
	ry := (float64(box.Bottom) - float64(box.Top)) / 2
	start := math.Atan2(float64(startPoint.Y-center.Y), float64(startPoint.X-center.X))
	end := math.Atan2(float64(endPoint.Y-center.Y), float64(endPoint.X-center.X))
	sweep := end - start
	if ctx.arcDirection == 1 {
		for sweep < 0 {
			sweep += 2 * math.Pi
		}
	} else {
		for sweep > 0 {
			sweep -= 2 * math.Pi
		}
	}
	return center, rx, ry, start, sweep
}

func drawRoundedRect(ctx *context, box RectL, rx, ry float64) {
	w := float64(box.Right - box.Left)
	h := float64(box.Bottom - box.Top)
	rx = math.Min(math.Abs(rx), w/2)
	ry = math.Min(math.Abs(ry), h/2)
	if rx == 0 || ry == 0 {
		ctx.MoveTo(float64(box.Left), float64(box.Top))
		ctx.LineTo(float64(box.Right), float64(box.Top))
		ctx.LineTo(float64(box.Right), float64(box.Bottom))
		ctx.LineTo(float64(box.Left), float64(box.Bottom))
		ctx.Close()
		return
	}
	k := 0.5522847498
	l, t := float64(box.Left), float64(box.Top)
	r, b := float64(box.Right), float64(box.Bottom)
	ctx.MoveTo(l+rx, t)
	ctx.LineTo(r-rx, t)
	ctx.CubicCurveTo(r-rx+k*rx, t, r, t+ry-k*ry, r, t+ry)
	ctx.LineTo(r, b-ry)
	ctx.CubicCurveTo(r, b-ry+k*ry, r-rx+k*rx, b, r-rx, b)
	ctx.LineTo(l+rx, b)
	ctx.CubicCurveTo(l+rx-k*rx, b, l, b-ry+k*ry, l, b-ry)
	ctx.LineTo(l, t+ry)
	ctx.CubicCurveTo(l, t+ry-k*ry, l+rx-k*rx, t, l+rx, t)
	ctx.Close()
}
