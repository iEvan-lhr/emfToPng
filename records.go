package emf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"math"
	"os"

	"github.com/llgcode/draw2d"
)

type Recorder interface {
	Draw(*context)
}

type Record struct {
	Type, Size uint32
}

func checkedCount(size, fixed, count, elementSize uint32) (int, error) {
	if size < 8 || fixed > size-8 || elementSize == 0 {
		return 0, fmt.Errorf("invalid record payload size %d", size)
	}
	maxCount := (size - 8 - fixed) / elementSize
	if count > maxCount {
		return 0, fmt.Errorf("record count %d exceeds payload capacity %d", count, maxCount)
	}
	return int(count), nil
}

func (r *Record) Draw(ctx *context) {}

func readRecord(reader *bytes.Reader) (Recorder, error) {
	startOffset, err := reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	var rec Record
	if err := binary.Read(reader, binary.LittleEndian, &rec); err != nil {
		return nil, err
	}
	if rec.Size < 8 || int64(rec.Size) > int64(reader.Len()+8) {
		return nil, fmt.Errorf("invalid EMF record size %d for type %#x", rec.Size, rec.Type)
	}

	//fmt.Printf("Parsed record: Offset=%d, Type=%d (0x%x), Size=%d\n", startOffset, rec.Type, rec.Type, rec.Size)

	fn, ok := records[rec.Type]

	var parsed Recorder
	if ok && fn != nil {
		// Parse known records from a bounded payload. A malformed offset or
		// length must not make a record reader consume bytes from the next
		// record before the final alignment seek below.
		payload := make([]byte, rec.Size-8)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, err
		}
		parsed, err = fn(bytes.NewReader(payload), rec.Size)
		if err != nil {
			return nil, err
		}
	} else {
		// EMF permits optional and vendor records. Keep their position in the
		// stream so a single unsupported record does not discard the file.
		parsed = &RawRecord{Record: rec}
	}

	// Ensure the reader is positioned exactly at the end of this record.
	// This prevents any misalignment bugs in custom record parsers.
	targetOffset := startOffset + int64(rec.Size)
	_, err = reader.Seek(targetOffset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	return parsed, nil
}

type HeaderRecord struct {
	Record
	Bounds, Frame           RectL
	RecordSignature         uint32
	Version, Bytes, Records uint32
	Handles                 uint16
	nDescription            uint32
	offDescription          uint32
	nPalEntries             uint32
	Device, Millimeters     SizeL
}

func readHeaderRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &HeaderRecord{}
	r.Record = Record{Type: EMR_HEADER, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Frame); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.RecordSignature); err != nil {
		return nil, err
	}

	if r.RecordSignature != ENHMETA_SIGNATURE {
		return nil, fmt.Errorf("unknown signature %#v", r.RecordSignature)
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Version); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bytes); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Records); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Handles); err != nil {
		return nil, err
	}

	// Reserved
	reader.Seek(int64(2), io.SeekCurrent)

	if err := binary.Read(reader, binary.LittleEndian, &r.nDescription); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.offDescription); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.nPalEntries); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Device); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Millimeters); err != nil {
		return nil, err
	}
	// skip the rest of structure
	reader.Seek(int64(size), io.SeekStart)
	return r, nil
}

type SetwindowextexRecord struct {
	Record
	Extent SizeL
}

func readSetwindowextexRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetwindowextexRecord{}
	r.Record = Record{Type: EMR_SETWINDOWEXTEX, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Extent); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *SetwindowextexRecord) Draw(ctx *context) {
	ctx.we = &r.Extent
	ctx.updateMapping()
}

type SetwindoworgexRecord struct {
	Record
	Origin PointL
}

func readSetwindoworgexRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetwindoworgexRecord{}
	r.Record = Record{Type: EMR_SETWINDOWORGEX, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Origin); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *SetwindoworgexRecord) Draw(ctx *context) {
	ctx.wo = &r.Origin
	ctx.updateMapping()
}

type SetviewportextexRecord struct {
	Record
	Extent SizeL
}

func readSetviewportextexRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetviewportextexRecord{}
	r.Record = Record{Type: EMR_SETVIEWPORTEXTEX, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Extent); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *SetviewportextexRecord) Draw(ctx *context) {
	ctx.ve = &r.Extent
	ctx.updateMapping()
}

type SetviewportorgexRecord struct {
	Record
	Origin PointL
}

func readSetviewportorgexRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetviewportorgexRecord{}
	r.Record = Record{Type: EMR_SETVIEWPORTORGEX, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Origin); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *SetviewportorgexRecord) Draw(ctx *context) {
	ctx.vo = &r.Origin
	ctx.updateMapping()
}

type EOFRecord struct {
	Record
	nPalEntries, offPalEntries, SizeLast uint32
}

func readEOFRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &EOFRecord{}
	r.Record = Record{Type: EMR_EOF, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.nPalEntries); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.offPalEntries); err != nil {
		return nil, err
	}

	if r.nPalEntries > 0 {
		fmt.Fprintln(os.Stderr, "emf: nPalEntries found - ", r.nPalEntries)
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.SizeLast); err != nil {
		return nil, err
	}

	return r, nil
}

type SetmapmodeRecord struct {
	Record
	MapMode uint32
}

func readSetmapmodeRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetmapmodeRecord{}
	r.Record = Record{Type: EMR_SETMAPMODE, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.MapMode); err != nil {
		return nil, err
	}

	return r, nil
}

// https://www-user.tu-chemnitz.de/~heha/petzold/ch05f.htm
// http://msdn.microsoft.com/en-us/library/dd183475(v=vs.85).aspx
func (r *SetmapmodeRecord) Draw(ctx *context) {
	ctx.mm = r.MapMode
	ctx.updateMapping()
}

type SetbkmodeRecord struct {
	Record
	BackgroundMode uint32
}

func readSetbkmodeRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetbkmodeRecord{}
	r.Record = Record{Type: EMR_SETBKMODE, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.BackgroundMode); err != nil {
		return nil, err
	}

	return r, nil
}

type SetpolyfillmodeRecord struct {
	Record
	PolygonFillMode uint32
}

func readSetpolyfillmodeRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetpolyfillmodeRecord{}
	r.Record = Record{Type: EMR_SETPOLYFILLMODE, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.PolygonFillMode); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *SetpolyfillmodeRecord) Draw(ctx *context) {
	if r.PolygonFillMode == ALTERNATE {
		ctx.SetFillRule(draw2d.FillRuleEvenOdd)
	} else if r.PolygonFillMode == WINDING {
		ctx.SetFillRule(draw2d.FillRuleWinding)
	}
}

type SettextalignRecord struct {
	Record
	TextAlignmentMode uint32
}

func readSettextalignRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SettextalignRecord{}
	r.Record = Record{Type: EMR_SETTEXTALIGN, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.TextAlignmentMode); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *SettextalignRecord) Draw(ctx *context) {
	ctx.textAlign = r.TextAlignmentMode
}

type SetstretchbltmodeRecord struct {
	Record
	StretchMode uint32
}

func readSetstretchbltmodeRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetstretchbltmodeRecord{}
	r.Record = Record{Type: EMR_SETSTRETCHBLTMODE, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.StretchMode); err != nil {
		return nil, err
	}

	return r, nil
}

type SettextcolorRecord struct {
	Record
	Color ColorRef
}

func readSettextcolorRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SettextcolorRecord{}
	r.Record = Record{Type: EMR_SETTEXTCOLOR, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Color); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *SettextcolorRecord) Draw(ctx *context) {
	ctx.textColor = r.Color.GetColor()
}

type SetbkcolorRecord struct {
	Record
	Color ColorRef
}

func readSetbkcolorRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetbkcolorRecord{}
	r.Record = Record{Type: EMR_SETBKCOLOR, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Color); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *SetbkcolorRecord) Draw(ctx *context) {
	ctx.bkColor = r.Color.GetColor()
}

type MovetoexRecord struct {
	Record
	Offset PointL
}

func readMovetoexRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &MovetoexRecord{}
	r.Record = Record{Type: EMR_MOVETOEX, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Offset); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *MovetoexRecord) Draw(ctx *context) {
	ctx.MoveTo(float64(r.Offset.X), float64(r.Offset.Y))
}

type IntersectcliprectRecord struct {
	Record
	Clip RectL
}

func (r *IntersectcliprectRecord) Draw(ctx *context) {
	ctx.applyClipMask(ctx.clipRect(r.Clip), RGN_AND)
}

func readIntersectcliprectRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &IntersectcliprectRecord{}
	r.Record = Record{Type: EMR_INTERSECTCLIPRECT, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Clip); err != nil {
		return nil, err
	}

	return r, nil
}

type SavedcRecord struct {
	Record
}

func readSavedcRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	return &SavedcRecord{Record: Record{Type: EMR_SAVEDC, Size: size}}, nil
}

func (r *SavedcRecord) Draw(ctx *context) {
	ctx.saveState()
	ctx.Save()
}

type RestoredcRecord struct {
	Record
	SavedDC int32
}

func readRestoredcRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &RestoredcRecord{}
	r.Record = Record{Type: EMR_RESTOREDC, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.SavedDC); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *RestoredcRecord) Draw(ctx *context) {
	ctx.restoreState()
	ctx.Restore()
}

type SetworldtransformRecord struct {
	Record
	XForm XForm
}

func readSetworldtransformRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetworldtransformRecord{}
	r.Record = Record{Type: EMR_SETWORLDTRANSFORM, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.XForm); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *SetworldtransformRecord) Draw(ctx *context) {
	ctx.worldTransform = [6]float64{
		float64(r.XForm.M11), float64(r.XForm.M12),
		float64(r.XForm.M21), float64(r.XForm.M22),
		float64(r.XForm.Dx), float64(r.XForm.Dy),
	}
	ctx.updateCTM()
}

type ModifyworldtransformRecord struct {
	Record
	XForm                    XForm
	ModifyWorldTransformMode uint32
}

func readModifyworldtransformRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ModifyworldtransformRecord{}
	r.Record = Record{Type: EMR_MODIFYWORLDTRANSFORM, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.XForm); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.ModifyWorldTransformMode); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *ModifyworldtransformRecord) Draw(ctx *context) {
	xf := [6]float64{
		float64(r.XForm.M11),
		float64(r.XForm.M12),
		float64(r.XForm.M21),
		float64(r.XForm.M22),
		float64(r.XForm.Dx),
		float64(r.XForm.Dy),
	}

	curr := ctx.worldTransform
	var res [6]float64

	switch r.ModifyWorldTransformMode {
	case MWT_IDENTITY:
		res = [6]float64{1, 0, 0, 1, 0, 0}
	case MWT_LEFTMULTIPLY:
		res[0] = xf[0]*curr[0] + xf[1]*curr[2]
		res[1] = xf[0]*curr[1] + xf[1]*curr[3]
		res[2] = xf[2]*curr[0] + xf[3]*curr[2]
		res[3] = xf[2]*curr[1] + xf[3]*curr[3]
		res[4] = xf[4]*curr[0] + xf[5]*curr[2] + curr[4]
		res[5] = xf[4]*curr[1] + xf[5]*curr[3] + curr[5]
	case MWT_RIGHTMULTIPLY:
		res[0] = curr[0]*xf[0] + curr[1]*xf[2]
		res[1] = curr[0]*xf[1] + curr[1]*xf[3]
		res[2] = curr[2]*xf[0] + curr[3]*xf[2]
		res[3] = curr[2]*xf[1] + curr[3]*xf[3]
		res[4] = curr[4]*xf[0] + curr[5]*xf[2] + xf[4]
		res[5] = curr[4]*xf[1] + curr[5]*xf[3] + xf[5]
	case MWT_SET:
		res = xf
	default:
		return
	}

	ctx.worldTransform = res
	ctx.updateCTM()
}

type SelectobjectRecord struct {
	Record
	ihObject uint32
}

func readSelectobjectRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SelectobjectRecord{}
	r.Record = Record{Type: EMR_SELECTOBJECT, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.ihObject); err != nil {
		return nil, err
	}

	return r, nil
}

var StockObjects = map[uint32]interface{}{
	WHITE_BRUSH:         LogBrushEx{Color: ColorRef{Red: 255, Green: 255, Blue: 255}},
	LTGRAY_BRUSH:        LogBrushEx{Color: ColorRef{Red: 192, Green: 192, Blue: 192}},
	GRAY_BRUSH:          LogBrushEx{Color: ColorRef{Red: 128, Green: 128, Blue: 128}},
	DKGRAY_BRUSH:        LogBrushEx{Color: ColorRef{Red: 64, Green: 64, Blue: 64}},
	BLACK_BRUSH:         LogBrushEx{Color: ColorRef{Red: 0, Green: 0, Blue: 0}},
	NULL_BRUSH:          true,
	WHITE_PEN:           LogPen{ColorRef: ColorRef{Red: 255, Green: 255, Blue: 255}, Width: PointL{1, 0}},
	BLACK_PEN:           LogPen{ColorRef: ColorRef{Red: 0, Green: 0, Blue: 0}, Width: PointL{1, 0}},
	NULL_PEN:            true,
	SYSTEM_FONT:         LogFont{Height: 11},
	DEVICE_DEFAULT_FONT: LogFont{Height: 11},
}

func (r *SelectobjectRecord) Draw(ctx *context) {

	object, ok := StockObjects[r.ihObject]
	if !ok {
		object, ok = ctx.objects[r.ihObject]
		if !ok {
			fmt.Fprintf(os.Stderr, "emf: object 0x%x not found\n", r.ihObject)
			return
		}
	}

	switch o := object.(type) {
	case bool:
		if r.ihObject == NULL_PEN {
			ctx.SetStrokeColor(image.Transparent)
		} else if r.ihObject == NULL_BRUSH {
			ctx.SetFillColor(image.Transparent)
		}
	case LogPen:
		w := float64(o.Width.X)
		if w <= 0 {
			w = 1
		}
		ctx.SetLineWidth(w)
		ctx.SetStrokeColor(o.ColorRef.GetColor())
		applyPenStyle(ctx, o.PenStyle, w)
	case LogPenEx:
		w := float64(o.Width)
		if w <= 0 {
			w = 1
		}
		ctx.SetLineWidth(w)
		ctx.SetStrokeColor(o.ColorRef.GetColor())
		applyPenStyle(ctx, o.PenStyle, w)
	case LogBrushEx:
		ctx.SetFillColor(o.Color.GetColor())
	case *PatternBrushRecord:
		if o.BmiSrc.BitCount == 1 {
			ctx.SetFillColor(ctx.textColor)
		} else {
			c := o.getColor(1)
			if c.A == 0 {
				c = o.getColor(0)
			}
			ctx.SetFillColor(c)
		}
	case LogFont:
		h := float64(o.Height)
		if h < 0 {
			h = -h
		}
		if h == 0 {
			h = 12
		}
		ctx.SetFontSize(h)

		var family draw2d.FontFamily
		switch uint8(o.PitchAndFamily) & 0xF0 {
		case 0x10: // FF_ROMAN
			family = draw2d.FontFamilySerif
		case 0x30: // FF_MODERN
			family = draw2d.FontFamilyMono
		default:
			family = draw2d.FontFamilySans
		}

		var style draw2d.FontStyle
		if o.Weight >= 700 {
			style |= draw2d.FontStyleBold
		}
		if o.Italic != 0 {
			style |= draw2d.FontStyleItalic
		}

		ctx.SetFontData(draw2d.FontData{
			Name:   o.Facename,
			Family: family,
			Style:  style,
		})
	}
}

type CreatepenRecord struct {
	Record
	ihPen  uint32
	LogPen LogPen
}

func readCreatepenRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &CreatepenRecord{}
	r.Record = Record{Type: EMR_CREATEPEN, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.ihPen); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.LogPen); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *CreatepenRecord) Draw(ctx *context) {
	ctx.objects[r.ihPen] = r.LogPen
}

type CreatebrushindirectRecord struct {
	Record
	ihBrush  uint32
	LogBrush LogBrushEx
}

func readCreatebrushindirectRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &CreatebrushindirectRecord{}
	r.Record = Record{Type: EMR_CREATEBRUSHINDIRECT, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.ihBrush); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.LogBrush); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *CreatebrushindirectRecord) Draw(ctx *context) {
	ctx.objects[r.ihBrush] = r.LogBrush
}

type DeleteobjectRecord struct {
	Record
	ihObject uint32
}

func readDeleteobjectRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &DeleteobjectRecord{}
	r.Record = Record{Type: EMR_DELETEOBJECT, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.ihObject); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *DeleteobjectRecord) Draw(ctx *context) {
	delete(ctx.objects, r.ihObject)
}

type RectangleRecord struct {
	Record
	Box RectL
}

func readRectangleRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &RectangleRecord{}
	r.Record = Record{Type: EMR_RECTANGLE, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Box); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *RectangleRecord) Draw(ctx *context) {
	x1, y1, x2, y2 := float64(r.Box.Left), float64(r.Box.Top), float64(r.Box.Right), float64(r.Box.Bottom)
	ctx.MoveTo(x1, y1)
	ctx.LineTo(x2, y1)
	ctx.LineTo(x2, y2)
	ctx.LineTo(x1, y2)
	ctx.LineTo(x1, y1)
	ctx.FillStroke()
}

type ArcRecord struct {
	Record
	Box   RectL
	Start PointL
	End   PointL
}

func readArcRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ArcRecord{}
	r.Record = Record{Type: EMR_ARC, Size: size}

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

func (r *ArcRecord) Draw(ctx *context) {
	center, rx, ry, start, sweep := arcGeometry(ctx, r.Box, r.Start, r.End)
	ctx.MoveTo(float64(r.Start.X), float64(r.Start.Y))
	ctx.ArcTo(float64(center.X), float64(center.Y), rx, ry, start, sweep)
	ctx.Stroke()
}

type LinetoRecord struct {
	Record
	Point PointL
}

func readLinetoRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &LinetoRecord{}
	r.Record = Record{Type: EMR_LINETO, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Point); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *LinetoRecord) Draw(ctx *context) {
	ctx.LineTo(float64(r.Point.X), float64(r.Point.Y))
	ctx.Stroke()
}

type BeginpathRecord struct {
	Record
}

func readBeginpathRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	return &BeginpathRecord{Record{Type: EMR_BEGINPATH, Size: size}}, nil
}

func (r *BeginpathRecord) Draw(ctx *context) {
	ctx.BeginPath()
}

type EndpathRecord struct {
	Record
}

func readEndpathRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	return &EndpathRecord{Record{Type: EMR_ENDPATH, Size: size}}, nil
}

func (r *EndpathRecord) Draw(ctx *context) {
	// EndPath does not close the current subpath. CloseFigure does that.
}

type ClosefigureRecord struct {
	Record
}

func readClosefigureRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	return &ClosefigureRecord{Record{Type: EMR_CLOSEFIGURE, Size: size}}, nil
}

func (r *ClosefigureRecord) Draw(ctx *context) {
	ctx.Close()
}

type FillpathRecord struct {
	Record
	Bounds RectL
}

func readFillpathRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &FillpathRecord{}
	r.Record = Record{Type: EMR_FILLPATH, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *FillpathRecord) Draw(ctx *context) {
	ctx.Fill()
}

type StrokeandfillpathRecord struct {
	Record
	Bounds RectL
}

func readStrokeandfillpathRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &StrokeandfillpathRecord{}
	r.Record = Record{Type: EMR_STROKEANDFILLPATH, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *StrokeandfillpathRecord) Draw(ctx *context) {
	ctx.Fill()
	ctx.Stroke()
}

type StrokepathRecord struct {
	Record
	Bounds RectL
}

func readStrokepathRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &StrokepathRecord{}
	r.Record = Record{Type: EMR_STROKEPATH, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *StrokepathRecord) Draw(ctx *context) {
	ctx.Stroke()
}

type SelectclippathRecord struct {
	Record
	RegionMode uint32
}

func readSelectclippathRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SelectclippathRecord{}
	r.Record = Record{Type: EMR_SELECTCLIPPATH, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.RegionMode); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *SelectclippathRecord) Draw(ctx *context) {
	ctx.applyClipMask(ctx.clipPath(), r.RegionMode)
	ctx.BeginPath()
}

type CommentRecord struct {
	Record
}

func readCommentRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &CommentRecord{}
	r.Record = Record{Type: EMR_COMMENT, Size: size}
	// skip record data
	reader.Seek(int64(r.Size-8), io.SeekCurrent)
	return r, nil
}

type ExtcreatefontindirectwRecord struct {
	Record
	ihFonts uint32
	elw     LogFont
}

func readExtcreatefontindirectwRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ExtcreatefontindirectwRecord{}
	r.Record = Record{Type: EMR_EXTCREATEFONTINDIRECTW, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.ihFonts); err != nil {
		return nil, err
	}

	var err error

	r.elw, err = readLogFont(reader)
	if err != nil {
		return nil, err
	}

	// skip the rest because we read only limited amount of data (LogFont) here
	reader.Seek(int64(r.Size-(12+92)), io.SeekCurrent)

	return r, nil
}

func (r *ExtcreatefontindirectwRecord) Draw(ctx *context) {
	ctx.objects[r.ihFonts] = r.elw
}

type ExttextoutwRecord struct {
	Record
	Bounds           RectL
	iGraphicsMode    uint32
	exScale, eyScale float32
	wEmrText         EmrText
}

func readExttextoutwRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ExttextoutwRecord{}
	r.Record = Record{Type: EMR_EXTTEXTOUTW, Size: size}
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
	r.wEmrText, err = parseEMRText(payload[28:], payload, true)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (r *ExttextoutwRecord) Draw(ctx *context) {
	drawEMRText(ctx, r.wEmrText)
}

type Polybezier16Record struct {
	Record
	Bounds  RectL
	Count   uint32
	aPoints []PointS
}

func readPolybezier16Record(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &Polybezier16Record{}
	r.Record = Record{Type: EMR_POLYBEZIER16, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}

	pointCount, err := checkedCount(size, 20, r.Count, 4)
	if err != nil {
		return nil, err
	}
	r.aPoints = make([]PointS, pointCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.aPoints); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Polybezier16Record) Draw(ctx *context) {
	if r.Count < 4 {
		return
	}
	ctx.MoveTo(float64(r.aPoints[0].X), float64(r.aPoints[0].Y))
	for i := 1; i+2 < int(r.Count); i = i + 3 {
		ctx.CubicCurveTo(
			float64(r.aPoints[i].X), float64(r.aPoints[i].Y),
			float64(r.aPoints[i+1].X), float64(r.aPoints[i+1].Y),
			float64(r.aPoints[i+2].X), float64(r.aPoints[i+2].Y),
		)
	}
	ctx.Stroke()
}

type Polygon16Record struct {
	Record
	Bounds  RectL
	Count   uint32
	aPoints []PointS
}

func readPolygon16Record(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &Polygon16Record{}
	r.Record = Record{Type: EMR_POLYGON16, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}

	pointCount, err := checkedCount(size, 20, r.Count, 4)
	if err != nil {
		return nil, err
	}
	r.aPoints = make([]PointS, pointCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.aPoints); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Polygon16Record) Draw(ctx *context) {
	ctx.MoveTo(float64(r.aPoints[0].X), float64(r.aPoints[0].Y))
	for i := 1; i < int(r.Count); i++ {
		ctx.LineTo(float64(r.aPoints[i].X), float64(r.aPoints[i].Y))
	}
	ctx.Close()
	ctx.FillStroke()
}

type Polyline16Record struct {
	Record
	Bounds  RectL
	Count   uint32
	aPoints []PointS
}

func readPolyline16Record(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &Polyline16Record{}
	r.Record = Record{Type: EMR_POLYLINE16, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}

	pointCount, err := checkedCount(size, 20, r.Count, 4)
	if err != nil {
		return nil, err
	}
	r.aPoints = make([]PointS, pointCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.aPoints); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Polyline16Record) Draw(ctx *context) {
	if r.Count == 0 {
		return
	}
	ctx.MoveTo(float64(r.aPoints[0].X), float64(r.aPoints[0].Y))
	for i := 1; i < int(r.Count); i++ {
		ctx.LineTo(float64(r.aPoints[i].X), float64(r.aPoints[i].Y))
	}
	ctx.Stroke()
}

type Polybezierto16Record struct {
	Record
	Bounds  RectL
	Count   uint32
	aPoints []PointS
}

func readPolybezierto16Record(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &Polybezierto16Record{}
	r.Record = Record{Type: EMR_POLYBEZIERTO16, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}

	pointCount, err := checkedCount(size, 20, r.Count, 4)
	if err != nil {
		return nil, err
	}
	r.aPoints = make([]PointS, pointCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.aPoints); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Polybezierto16Record) Draw(ctx *context) {
	for i := 0; i+2 < int(r.Count); i = i + 3 {
		ctx.CubicCurveTo(
			float64(r.aPoints[i].X), float64(r.aPoints[i].Y),
			float64(r.aPoints[i+1].X), float64(r.aPoints[i+1].Y),
			float64(r.aPoints[i+2].X), float64(r.aPoints[i+2].Y),
		)
	}
	ctx.Stroke()
}

type Polylineto16Record struct {
	Record
	Bounds  RectL
	Count   uint32
	aPoints []PointS
}

func readPolylineto16Record(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &Polylineto16Record{}
	r.Record = Record{Type: EMR_POLYLINETO16, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}

	pointCount, err := checkedCount(size, 20, r.Count, 4)
	if err != nil {
		return nil, err
	}
	r.aPoints = make([]PointS, pointCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.aPoints); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Polylineto16Record) Draw(ctx *context) {
	for i := 0; i < int(r.Count); i++ {
		ctx.LineTo(float64(r.aPoints[i].X), float64(r.aPoints[i].Y))
	}
	ctx.Stroke()
}

type Polypolygon16Record struct {
	Record
	Bounds            RectL
	NumberOfPolygons  uint32
	Count             uint32
	PolygonPointCount []uint32
	aPoints           []PointS
}

func readPolypolygon16Record(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &Polypolygon16Record{}
	r.Record = Record{Type: EMR_POLYPOLYGON16, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.NumberOfPolygons); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}

	polygonCount, err := checkedCount(size, 24, r.NumberOfPolygons, 4)
	if err != nil {
		return nil, err
	}
	r.PolygonPointCount = make([]uint32, polygonCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.PolygonPointCount); err != nil {
		return nil, err
	}

	pointCount, err := checkedCount(size, 24+r.NumberOfPolygons*4, r.Count, 4)
	if err != nil {
		return nil, err
	}
	r.aPoints = make([]PointS, pointCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.aPoints); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Polypolygon16Record) Draw(ctx *context) {
	idx := 0
	for p := 0; p < int(r.NumberOfPolygons); p++ {
		pCount := int(r.PolygonPointCount[p])
		ctx.MoveTo(float64(r.aPoints[idx].X), float64(r.aPoints[idx].Y))
		for i := 1; i < pCount; i++ {
			ctx.LineTo(float64(r.aPoints[idx+i].X), float64(r.aPoints[idx+i].Y))
		}
		idx += pCount
		ctx.Close()
	}
	ctx.FillStroke()
}

type ExtcreatepenRecord struct {
	Record
	ihPen           uint32
	offBmi, cbBmi   uint32
	offBits, cbBits uint32
	elp             LogPenEx
	BmiSrc          DibHeaderInfo
	BitsSrc         []byte
}

func readExtcreatepenRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &ExtcreatepenRecord{}
	r.Record = Record{Type: EMR_EXTCREATEPEN, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.ihPen); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.offBmi); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cbBmi); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.offBits); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cbBits); err != nil {
		return nil, err
	}

	var err error
	r.elp, err = readLogPenEx(reader)
	if err != nil {
		return nil, err
	}

	// offset for bitmap info less than possible minimum
	// assuming there is no bitmap
	if r.offBmi < 52 {
		return r, nil
	}

	// BitmapBuffer
	// skipping UndefinedSpace
	reader.Seek(int64(r.offBmi-52-(r.elp.NumStyleEntries*4)), io.SeekCurrent)

	// record does not contain bitmap
	if r.cbBmi == 0 {
		return r, nil
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.BmiSrc); err != nil {
		return nil, err
	}

	r.BitsSrc, err = readRecordBytes(reader, uint64(r.cbBits))
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (r *ExtcreatepenRecord) Draw(ctx *context) {
	ctx.objects[r.ihPen] = r.elp
}

type SeticmmodeRecord struct {
	Record
	ICMMode uint32
}

func readSeticmmodeRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SeticmmodeRecord{}
	r.Record = Record{Type: EMR_SETICMMODE, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.ICMMode); err != nil {
		return nil, err
	}

	return r, nil
}

// map of readers for records
var records = map[uint32]func(*bytes.Reader, uint32) (Recorder, error){
	EMR_HEADER:                  readHeaderRecord,
	EMR_POLYBEZIER:              readPolybezierRecord,
	EMR_POLYGON:                 readPolygonRecord,
	EMR_POLYLINE:                readPolylineRecord,
	EMR_POLYBEZIERTO:            readPolybeziertoRecord,
	EMR_POLYLINETO:              readPolylinetoRecord,
	EMR_POLYPOLYLINE:            readPolypolylineRecord,
	EMR_POLYPOLYGON:             readPolypolygonRecord,
	EMR_SETWINDOWEXTEX:          readSetwindowextexRecord,
	EMR_SETWINDOWORGEX:          readSetwindoworgexRecord,
	EMR_SETVIEWPORTEXTEX:        readSetviewportextexRecord,
	EMR_SETVIEWPORTORGEX:        readSetviewportorgexRecord,
	EMR_SETBRUSHORGEX:           readSetbrushorgexRecord,
	EMR_EOF:                     readEOFRecord,
	EMR_SETPIXELV:               readSetpixelvRecord,
	EMR_SETMAPPERFLAGS:          nil,
	EMR_SETMAPMODE:              readSetmapmodeRecord,
	EMR_SETBKMODE:               readSetbkmodeRecord,
	EMR_SETPOLYFILLMODE:         readSetpolyfillmodeRecord,
	EMR_SETROP2:                 readSetrop2Record,
	EMR_SETSTRETCHBLTMODE:       readSetstretchbltmodeRecord,
	EMR_SETTEXTALIGN:            readSettextalignRecord,
	EMR_SETCOLORADJUSTMENT:      nil,
	EMR_SETTEXTCOLOR:            readSettextcolorRecord,
	EMR_SETBKCOLOR:              readSetbkcolorRecord,
	EMR_OFFSETCLIPRGN:           readOffsetcliprgnRecord,
	EMR_MOVETOEX:                readMovetoexRecord,
	EMR_SETMETARGN:              readSetmetargnRecord,
	EMR_EXCLUDECLIPRECT:         readExcludecliprectRecord,
	EMR_INTERSECTCLIPRECT:       readIntersectcliprectRecord,
	EMR_SCALEVIEWPORTEXTEX:      readScaleviewportextexRecord,
	EMR_SCALEWINDOWEXTEX:        readScalewindowextexRecord,
	EMR_SAVEDC:                  readSavedcRecord,
	EMR_RESTOREDC:               readRestoredcRecord,
	EMR_SETWORLDTRANSFORM:       readSetworldtransformRecord,
	EMR_MODIFYWORLDTRANSFORM:    readModifyworldtransformRecord,
	EMR_SELECTOBJECT:            readSelectobjectRecord,
	EMR_CREATEPEN:               readCreatepenRecord,
	EMR_CREATEBRUSHINDIRECT:     readCreatebrushindirectRecord,
	EMR_DELETEOBJECT:            readDeleteobjectRecord,
	EMR_ANGLEARC:                readAnglearcRecord,
	EMR_ELLIPSE:                 readEllipseRecord,
	EMR_RECTANGLE:               readRectangleRecord,
	EMR_ROUNDRECT:               readRoundrectRecord,
	EMR_ARC:                     readArcRecord,
	EMR_CHORD:                   readChordRecord,
	EMR_PIE:                     readPieRecord,
	EMR_SELECTPALETTE:           nil,
	EMR_CREATEPALETTE:           nil,
	EMR_SETPALETTEENTRIES:       nil,
	EMR_RESIZEPALETTE:           nil,
	EMR_REALIZEPALETTE:          nil,
	EMR_EXTFLOODFILL:            readExtfloodfillRecord,
	EMR_LINETO:                  readLinetoRecord,
	EMR_ARCTO:                   readArctoRecord,
	EMR_POLYDRAW:                readPolydrawRecord,
	EMR_SETARCDIRECTION:         readSetarcdirectionRecord,
	EMR_SETMITERLIMIT:           readSetmiterlimitRecord,
	EMR_BEGINPATH:               readBeginpathRecord,
	EMR_ENDPATH:                 readEndpathRecord,
	EMR_CLOSEFIGURE:             readClosefigureRecord,
	EMR_FILLPATH:                readFillpathRecord,
	EMR_STROKEANDFILLPATH:       readStrokeandfillpathRecord,
	EMR_STROKEPATH:              readStrokepathRecord,
	EMR_FLATTENPATH:             nil,
	EMR_WIDENPATH:               nil,
	EMR_SELECTCLIPPATH:          readSelectclippathRecord,
	EMR_ABORTPATH:               nil,
	EMR_COMMENT:                 readCommentRecord,
	EMR_FILLRGN:                 readFillrgnRecord,
	EMR_FRAMERGN:                readFramergnRecord,
	EMR_INVERTRGN:               readInvertrgnRecord,
	EMR_PAINTRGN:                readPaintrgnRecord,
	EMR_EXTSELECTCLIPRGN:        readExtselectcliprgnRecord,
	EMR_BITBLT:                  readBitbltRecord,
	EMR_STRETCHBLT:              readStretchbltRecord,
	EMR_MASKBLT:                 readMaskbltRecord,
	EMR_PLGBLT:                  nil,
	EMR_SETDIBITSTODEVICE:       readSetdibitstodeviceRecord,
	EMR_STRETCHDIBITS:           readStretchdibitsRecord,
	EMR_EXTCREATEFONTINDIRECTW:  readExtcreatefontindirectwRecord,
	EMR_EXTTEXTOUTA:             readExttextoutaRecord,
	EMR_EXTTEXTOUTW:             readExttextoutwRecord,
	EMR_POLYBEZIER16:            readPolybezier16Record,
	EMR_POLYGON16:               readPolygon16Record,
	EMR_POLYLINE16:              readPolyline16Record,
	EMR_POLYBEZIERTO16:          readPolybezierto16Record,
	EMR_POLYLINETO16:            readPolylineto16Record,
	EMR_POLYPOLYLINE16:          readPolypolyline16Record,
	EMR_POLYPOLYGON16:           readPolypolygon16Record,
	EMR_POLYDRAW16:              readPolydraw16Record,
	EMR_CREATEMONOBRUSH:         readCreatemonobrushRecord,
	EMR_CREATEDIBPATTERNBRUSHPT: readCreatedibpatternbrushptRecord,
	EMR_EXTCREATEPEN:            readExtcreatepenRecord,
	EMR_POLYTEXTOUTA:            readPolytextoutaRecord,
	EMR_POLYTEXTOUTW:            readPolytextoutwRecord,
	EMR_SETICMMODE:              readSeticmmodeRecord,
	EMR_CREATECOLORSPACE:        nil,
	EMR_SETCOLORSPACE:           nil,
	EMR_DELETECOLORSPACE:        nil,
	EMR_GLSRECORD:               nil,
	EMR_GLSBOUNDEDRECORD:        nil,
	EMR_PIXELFORMAT:             nil,
	EMR_DRAWESCAPE:              nil,
	EMR_EXTESCAPE:               nil,
	EMR_SMALLTEXTOUT:            nil,
	EMR_FORCEUFIMAPPING:         nil,
	EMR_NAMEDESCAPE:             nil,
	EMR_COLORCORRECTPALETTE:     nil,
	EMR_SETICMPROFILEA:          nil,
	EMR_SETICMPROFILEW:          nil,
	EMR_ALPHABLEND:              readAlphablendRecord,
	EMR_SETLAYOUT:               readSetlayoutRecord,
	EMR_TRANSPARENTBLT:          readTransparentbltRecord,
	EMR_GRADIENTFILL:            readGradientFillRecord,
	EMR_SETLINKEDUFIS:           nil,
	EMR_SETTEXTJUSTIFICATION:    readSettextjustificationRecord,
	EMR_COLORMATCHTOTARGETW:     nil,
	EMR_CREATECOLORSPACEW:       nil,
}

type PolygonRecord struct {
	Record
	Bounds  RectL
	Count   uint32
	aPoints []PointL
}

func readPolygonRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &PolygonRecord{}
	r.Record = Record{Type: EMR_POLYGON, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}

	pointCount, err := checkedCount(size, 20, r.Count, 8)
	if err != nil {
		return nil, err
	}
	r.aPoints = make([]PointL, pointCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.aPoints); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *PolygonRecord) Draw(ctx *context) {
	if r.Count == 0 {
		return
	}
	ctx.MoveTo(float64(r.aPoints[0].X), float64(r.aPoints[0].Y))
	for i := 1; i < int(r.Count); i++ {
		ctx.LineTo(float64(r.aPoints[i].X), float64(r.aPoints[i].Y))
	}
	ctx.Close()
	ctx.FillStroke()
}

type PolylineRecord struct {
	Record
	Bounds  RectL
	Count   uint32
	aPoints []PointL
}

func readPolylineRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &PolylineRecord{}
	r.Record = Record{Type: EMR_POLYLINE, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}

	pointCount, err := checkedCount(size, 20, r.Count, 8)
	if err != nil {
		return nil, err
	}
	r.aPoints = make([]PointL, pointCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.aPoints); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *PolylineRecord) Draw(ctx *context) {
	if r.Count == 0 {
		return
	}
	ctx.MoveTo(float64(r.aPoints[0].X), float64(r.aPoints[0].Y))
	for i := 1; i < int(r.Count); i++ {
		ctx.LineTo(float64(r.aPoints[i].X), float64(r.aPoints[i].Y))
	}
	ctx.Stroke()
}

type PolylinetoRecord struct {
	Record
	Bounds  RectL
	Count   uint32
	aPoints []PointL
}

func readPolylinetoRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &PolylinetoRecord{}
	r.Record = Record{Type: EMR_POLYLINETO, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}

	pointCount, err := checkedCount(size, 20, r.Count, 8)
	if err != nil {
		return nil, err
	}
	r.aPoints = make([]PointL, pointCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.aPoints); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *PolylinetoRecord) Draw(ctx *context) {
	for i := 0; i < int(r.Count); i++ {
		ctx.LineTo(float64(r.aPoints[i].X), float64(r.aPoints[i].Y))
	}
	ctx.Stroke()
}

type PolypolylineRecord struct {
	Record
	Bounds             RectL
	NumberOfPolylines  uint32
	Count              uint32
	PolylinePointCount []uint32
	aPoints            []PointL
}

func readPolypolylineRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &PolypolylineRecord{}
	r.Record = Record{Type: EMR_POLYPOLYLINE, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.NumberOfPolylines); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}

	polylineCount, err := checkedCount(size, 24, r.NumberOfPolylines, 4)
	if err != nil {
		return nil, err
	}
	r.PolylinePointCount = make([]uint32, polylineCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.PolylinePointCount); err != nil {
		return nil, err
	}

	pointCount, err := checkedCount(size, 24+r.NumberOfPolylines*4, r.Count, 8)
	if err != nil {
		return nil, err
	}
	r.aPoints = make([]PointL, pointCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.aPoints); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *PolypolylineRecord) Draw(ctx *context) {
	idx := 0
	for p := 0; p < int(r.NumberOfPolylines); p++ {
		pCount := int(r.PolylinePointCount[p])
		if pCount < 2 {
			idx += pCount
			continue
		}
		ctx.MoveTo(float64(r.aPoints[idx].X), float64(r.aPoints[idx].Y))
		for i := 1; i < pCount; i++ {
			ctx.LineTo(float64(r.aPoints[idx+i].X), float64(r.aPoints[idx+i].Y))
		}
		idx += pCount
		ctx.Stroke()
	}
}

type PolypolygonRecord struct {
	Record
	Bounds            RectL
	NumberOfPolygons  uint32
	Count             uint32
	PolygonPointCount []uint32
	aPoints           []PointL
}

func readPolypolygonRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &PolypolygonRecord{}
	r.Record = Record{Type: EMR_POLYPOLYGON, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.NumberOfPolygons); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.Count); err != nil {
		return nil, err
	}

	polygonCount, err := checkedCount(size, 24, r.NumberOfPolygons, 4)
	if err != nil {
		return nil, err
	}
	r.PolygonPointCount = make([]uint32, polygonCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.PolygonPointCount); err != nil {
		return nil, err
	}

	pointCount, err := checkedCount(size, 24+r.NumberOfPolygons*4, r.Count, 8)
	if err != nil {
		return nil, err
	}
	r.aPoints = make([]PointL, pointCount)
	if err := binary.Read(reader, binary.LittleEndian, &r.aPoints); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *PolypolygonRecord) Draw(ctx *context) {
	idx := 0
	for p := 0; p < int(r.NumberOfPolygons); p++ {
		pCount := int(r.PolygonPointCount[p])
		if pCount < 2 {
			idx += pCount
			continue
		}
		ctx.MoveTo(float64(r.aPoints[idx].X), float64(r.aPoints[idx].Y))
		for i := 1; i < pCount; i++ {
			ctx.LineTo(float64(r.aPoints[idx+i].X), float64(r.aPoints[idx+i].Y))
		}
		idx += pCount
		ctx.Close()
	}
	ctx.FillStroke()
}

type EllipseRecord struct {
	Record
	Box RectL
}

func readEllipseRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &EllipseRecord{}
	r.Record = Record{Type: EMR_ELLIPSE, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Box); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *EllipseRecord) Draw(ctx *context) {
	center := r.Box.Center()
	rx := (float64(r.Box.Right) - float64(r.Box.Left) - 1) / 2
	ry := (float64(r.Box.Bottom) - float64(r.Box.Top) - 1) / 2

	ctx.ArcTo(float64(center.X), float64(center.Y), rx, ry, 0, 2*math.Pi)
	ctx.Close()
	ctx.FillStroke()
}

type SetlayoutRecord struct {
	Record
	LayoutMode uint32
}

func readSetlayoutRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetlayoutRecord{}
	r.Record = Record{Type: EMR_SETLAYOUT, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.LayoutMode); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *SetlayoutRecord) Draw(ctx *context) {
	// GDI Layout Mode: 1 = LAYOUT_RTL (Right-to-Left)
	if r.LayoutMode == 1 {
		tr := ctx.GetMatrixTransform()
		tr[0] = -tr[0]
		tr[1] = -tr[1]
		tr[4] = float64(ctx.w) - tr[4]
		ctx.SetMatrixTransform(tr)
	}
}
