package emf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	"github.com/disintegration/imaging"
)

func readRecordBytes(reader *bytes.Reader, size uint64) ([]byte, error) {
	if size > uint64(reader.Len()) {
		return nil, io.ErrUnexpectedEOF
	}
	data := make([]byte, int(size))
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}
	return data, nil
}

type bitmapRecord struct {
	Record
	Bounds                       RectL
	xDest, yDest, cxDest, cyDest int32
	BitBltRasterOperation        uint32
	xSrc, ySrc                   int32
	XformSrc                     XForm
	BkColorSrc                   ColorRef
	UsageSrc                     uint32
	offBmiSrc, cbBmiSrc          uint32
	offBitsSrc, cbBitsSrc        uint32
	// only for EMR_STRETCHBLT
	cxSrc, cySrc int32

	BmiSrc     BitmapInfoHeader
	ColorTable []byte
	BitsSrc    []byte
}

func readDIBBlock(reader *bytes.Reader, offBmi, offBits, cbBits uint32) (BitmapInfoHeader, []byte, []byte, error) {
	var header BitmapInfoHeader
	if offBmi == 0 {
		return header, nil, nil, nil
	}
	if offBmi < 8 || offBits < offBmi+40 {
		return header, nil, nil, fmt.Errorf("invalid DIB offsets bmi=%d bits=%d", offBmi, offBits)
	}
	if _, err := reader.Seek(int64(offBmi)-8, io.SeekStart); err != nil {
		return header, nil, nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &header); err != nil {
		return header, nil, nil, err
	}
	colorTableSize := int64(offBits) - int64(offBmi) - 40
	colorTable, err := readRecordBytes(reader, uint64(colorTableSize))
	if err != nil {
		return header, nil, nil, err
	}
	bits, err := readRecordBytes(reader, uint64(cbBits))
	if err != nil {
		return header, nil, nil, err
	}
	return header, colorTable, bits, nil
}

// unified reader function for EMR_BITBLT and EMR_STRETCHBLT
func (r *bitmapRecord) read(reader *bytes.Reader) (Recorder, error) {
	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.xDest); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.yDest); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cxDest); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cyDest); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.BitBltRasterOperation); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.xSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.ySrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.XformSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.BkColorSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.UsageSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.offBmiSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cbBmiSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.offBitsSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cbBitsSrc); err != nil {
		return nil, err
	}

	if r.Type == EMR_STRETCHBLT {
		if err := binary.Read(reader, binary.LittleEndian, &r.cxSrc); err != nil {
			return nil, err
		}

		if err := binary.Read(reader, binary.LittleEndian, &r.cySrc); err != nil {
			return nil, err
		}
	}

	// no bitmap data
	if r.offBmiSrc == 0 {
		return r, nil
	}

	// defined record size to skip UndefinedSpace
	var rsize uint32
	if r.Type == EMR_STRETCHBLT {
		rsize = 108
	} else if r.Type == EMR_BITBLT || r.Type == EMR_ALPHABLEND {
		rsize = 100
	}
	if rsize == 0 || r.offBmiSrc < rsize {
		return nil, fmt.Errorf("invalid bitmap offset %d for record type %#x", r.offBmiSrc, r.Type)
	}

	// BitmapBuffer
	// skipping UndefinedSpace1
	if _, err := reader.Seek(int64(r.offBmiSrc)-int64(rsize), io.SeekCurrent); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.BmiSrc); err != nil {
		return nil, err
	}

	// Read ColorTable
	colorTableSize := int64(r.offBitsSrc) - int64(r.offBmiSrc) - 40
	if colorTableSize > 0 {
		var err error
		r.ColorTable, err = readRecordBytes(reader, uint64(colorTableSize))
		if err != nil {
			return nil, err
		}
	}

	var err error
	r.BitsSrc, err = readRecordBytes(reader, uint64(r.cbBitsSrc))
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (r *bitmapRecord) getColor(idx int) color.RGBA {
	offset := idx * 4
	if offset+3 < len(r.ColorTable) {
		return color.RGBA{
			R: r.ColorTable[offset+2],
			G: r.ColorTable[offset+1],
			B: r.ColorTable[offset+0],
			A: 0xff,
		}
	}
	if r.BmiSrc.BitCount == BI_BITCOUNT_1 {
		if idx == 0 {
			return color.RGBA{0, 0, 0, 0}
		}
		return color.RGBA{255, 255, 255, 0xff}
	}
	val := uint8(idx)
	if r.BmiSrc.BitCount == BI_BITCOUNT_2 {
		val = uint8(idx * 17)
	}
	return color.RGBA{val, val, val, 0xff}
}

func (r *bitmapRecord) decodeRLE(width, height, bitCount int, topDown bool) image.Image {
	if bitCount != 4 && bitCount != 8 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	x, y := 0, 0
	setIndex := func(idx int) {
		if x >= 0 && x < width && y >= 0 && y < height {
			dstY := height - 1 - y
			if topDown {
				dstY = y
			}
			img.SetRGBA(x, dstY, r.getColor(idx))
		}
		x++
	}

	data := r.BitsSrc
	for offset := 0; offset+1 < len(data); {
		count := int(data[offset])
		value := data[offset+1]
		offset += 2
		if count != 0 {
			for i := 0; i < count; i++ {
				idx := int(value)
				if bitCount == 4 {
					if i&1 == 0 {
						idx = int(value >> 4)
					} else {
						idx = int(value & 0x0f)
					}
				}
				setIndex(idx)
			}
			continue
		}

		switch value {
		case 0:
			x = 0
			y++
		case 1:
			return img
		case 2:
			if offset+1 >= len(data) {
				return nil
			}
			x += int(data[offset])
			y += int(data[offset+1])
			offset += 2
		default:
			absoluteCount := int(value)
			if bitCount == 8 {
				if offset+absoluteCount > len(data) {
					return nil
				}
				for i := 0; i < absoluteCount; i++ {
					setIndex(int(data[offset+i]))
				}
				offset += absoluteCount
				if absoluteCount&1 != 0 {
					offset++
				}
				if offset > len(data) {
					return nil
				}
				continue
			}

			byteCount := (absoluteCount + 1) / 2
			if offset+byteCount > len(data) {
				return nil
			}
			for i := 0; i < absoluteCount; i++ {
				packed := data[offset+i/2]
				if i&1 == 0 {
					setIndex(int(packed >> 4))
				} else {
					setIndex(int(packed & 0x0f))
				}
			}
			offset += byteCount
			if byteCount&1 != 0 {
				offset++
			}
			if offset > len(data) {
				return nil
			}
		}
	}
	return nil
}

func (r *bitmapRecord) readDIBImage() image.Image {
	if r.offBmiSrc == 0 || r.BmiSrc.Width == 0 || r.BmiSrc.Height == 0 {
		return nil
	}
	width := int(r.BmiSrc.Width)
	heightValue := int(r.BmiSrc.Height)
	topDown := heightValue < 0
	if width < 0 {
		width = -width
	}
	height := heightValue
	if height < 0 {
		height = -height
	}
	if width <= 0 || height <= 0 {
		return nil
	}

	if r.BmiSrc.BitCount == BI_BITCOUNT_0 || r.BmiSrc.Compression == BI_PNG || r.BmiSrc.Compression == BI_JPEG {
		img, _, err := image.Decode(bytes.NewReader(r.BitsSrc))
		if err != nil {
			fmt.Fprintln(os.Stderr, "emf: failed to decode embedded JPEG/PNG image:", err)
			return nil
		}
		return img
	}

	bitCount := int(r.BmiSrc.BitCount)
	if bitCount != 1 && bitCount != 4 && bitCount != 8 && bitCount != 16 && bitCount != 24 && bitCount != 32 {
		fmt.Fprintln(os.Stderr, "emf: unsupported bitmap bit count", r.BmiSrc.BitCount)
		return nil
	}
	if r.BmiSrc.Compression == BI_RLE4 || r.BmiSrc.Compression == BI_RLE8 {
		expectedBitCount := 4
		if r.BmiSrc.Compression == BI_RLE8 {
			expectedBitCount = 8
		}
		if bitCount != expectedBitCount {
			return nil
		}
		return r.decodeRLE(width, height, bitCount, topDown)
	}
	if r.BmiSrc.Compression != BI_RGB && r.BmiSrc.Compression != BI_BITFIELDS {
		fmt.Fprintln(os.Stderr, "emf: unsupported bitmap compression", r.BmiSrc.Compression)
		return nil
	}

	bytesPerLine := ((width*bitCount + 31) / 32) * 4
	if bytesPerLine <= 0 || bytesPerLine > len(r.BitsSrc) {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	var masks [3]uint32
	switch bitCount {
	case 16:
		masks = [3]uint32{0x7c00, 0x03e0, 0x001f}
	case 32:
		masks = [3]uint32{0x00ff0000, 0x0000ff00, 0x000000ff}
	}
	if r.BmiSrc.Compression == BI_BITFIELDS && len(r.ColorTable) >= 12 {
		masks[0] = binary.LittleEndian.Uint32(r.ColorTable[0:4])
		masks[1] = binary.LittleEndian.Uint32(r.ColorTable[4:8])
		masks[2] = binary.LittleEndian.Uint32(r.ColorTable[8:12])
	}

	for row := 0; row < height; row++ {
		srcRow := row
		if !topDown {
			srcRow = height - 1 - row
		}
		start := srcRow * bytesPerLine
		if start < 0 || start+bytesPerLine > len(r.BitsSrc) {
			return nil
		}
		line := r.BitsSrc[start : start+bytesPerLine]
		for x := 0; x < width; x++ {
			var c color.RGBA
			switch bitCount {
			case 1:
				idx := int((line[x/8] >> uint(7-x%8)) & 1)
				c = r.getColor(idx)
			case 4:
				value := line[x/2]
				idx := int(value >> 4)
				if x%2 != 0 {
					idx = int(value & 0x0f)
				}
				c = r.getColor(idx)
			case 8:
				c = r.getColor(int(line[x]))
			case 16:
				value := uint32(binary.LittleEndian.Uint16(line[x*2:]))
				c = color.RGBA{R: maskComponent(value, masks[0]), G: maskComponent(value, masks[1]), B: maskComponent(value, masks[2]), A: 0xff}
			case 24:
				offset := x * 3
				c = color.RGBA{R: line[offset+2], G: line[offset+1], B: line[offset], A: 0xff}
			case 32:
				offset := x * 4
				alpha := line[offset+3]
				if alpha == 0 {
					alpha = 0xff
				}
				if r.BmiSrc.Compression == BI_BITFIELDS {
					value := binary.LittleEndian.Uint32(line[offset:])
					c = color.RGBA{R: maskComponent(value, masks[0]), G: maskComponent(value, masks[1]), B: maskComponent(value, masks[2]), A: alpha}
				} else {
					c = color.RGBA{R: line[offset+2], G: line[offset+1], B: line[offset], A: alpha}
				}
			}
			img.SetRGBA(x, row, c)
		}
	}
	return img
}

func maskComponent(value, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := uint(0)
	for ((mask >> shift) & 1) == 0 {
		shift++
	}
	max := mask >> shift
	component := (value & mask) >> shift
	return uint8((component*255 + max/2) / max)
}

func (r *bitmapRecord) readImage() image.Image {
	return r.readDIBImage()
}

func (r *bitmapRecord) readImageLegacy() image.Image {

	if r.offBmiSrc == 0 {
		return nil
	}

	if r.BmiSrc.BitCount == BI_BITCOUNT_0 {
		img, _, err := image.Decode(bytes.NewReader(r.BitsSrc))
		if err != nil {
			fmt.Fprintln(os.Stderr, "emf: failed to decode embedded JPEG/PNG image:", err)
			return nil
		}
		return img
	}

	// bytes per pixel
	bpp, ok := map[uint16]int{
		BI_BITCOUNT_1: 0,
		BI_BITCOUNT_2: 0,
		BI_BITCOUNT_3: 1,
		BI_BITCOUNT_5: 3,
		BI_BITCOUNT_4: 2,
		BI_BITCOUNT_6: 4,
	}[r.BmiSrc.BitCount]

	if !ok {
		fmt.Fprintln(os.Stderr, "emf: unsupported bitmap type", r.BmiSrc.BitCount)
		return nil
	}

	// src image width and height
	width, height := int(r.BmiSrc.Width), int(r.BmiSrc.Height)
	// bytes per line with padding to 4 bytes
	bpl := ((width*int(r.BmiSrc.BitCount) + 31) & 0xFFFFFFE0) / 8

	switch r.BmiSrc.BitCount {
	case BI_BITCOUNT_1:
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		mask := []byte{0x80, 0x40, 0x20, 0x10, 0x08, 0x04, 0x02, 0x01}

		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				p := img.PixOffset(x, height-y-1)
				idx := 0
				if (r.BitsSrc[y*bpl+x/8] & mask[x%8]) > 0 {
					idx = 1
				}
				c := r.getColor(idx)
				img.Pix[p+0] = c.R
				img.Pix[p+1] = c.G
				img.Pix[p+2] = c.B
				img.Pix[p+3] = 0xff
			}
		}
		return img

	case BI_BITCOUNT_2:
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				p := img.PixOffset(x, height-y-1)
				byteVal := r.BitsSrc[y*bpl+x/2]
				var idx int
				if x%2 == 0 {
					idx = int((byteVal >> 4) & 0x0f)
				} else {
					idx = int(byteVal & 0x0f)
				}
				c := r.getColor(idx)
				img.Pix[p+0] = c.R
				img.Pix[p+1] = c.G
				img.Pix[p+2] = c.B
				img.Pix[p+3] = 0xff
			}
		}
		return img

	case BI_BITCOUNT_3:
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				p := img.PixOffset(x, height-y-1)
				idx := int(r.BitsSrc[y*bpl+x])
				c := r.getColor(idx)
				img.Pix[p+0] = c.R
				img.Pix[p+1] = c.G
				img.Pix[p+2] = c.B
				img.Pix[p+3] = 0xff
			}
		}
		return img

	case BI_BITCOUNT_4:
		if r.BmiSrc.Compression != BI_RGB {
			fmt.Fprintln(os.Stderr, "emf: unsupported compression type", r.BmiSrc.Compression)
			return nil
		}

		img := image.NewRGBA(image.Rect(0, 0, width, height))
		ix := 0
		// BMP images are stored bottom-up
		for y := height - 1; y >= 0; y-- {
			b := r.BitsSrc[y*bpl : y*bpl+bpl]
			p := img.Pix[ix*img.Stride : ix*img.Stride+img.Stride]
			for i, j := 0, 0; i < len(p); i, j = i+4, j+bpp {
				// The relative intensities of red, green, and blue
				// are represented with 5 bits for each color component.
				c := uint16(b[j+1])<<8 | uint16(b[j])
				p[i+0] = uint8((c>>10)&0x001f) * 8
				p[i+1] = uint8((c>>5)&0x001f) * 8
				p[i+2] = uint8(c&0x001f) * 8
				p[i+3] = 0xff
			}
			ix = ix + 1
		}
		return img

	case BI_BITCOUNT_5, BI_BITCOUNT_6:
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		ix := 0
		// BMP images are stored bottom-up
		for y := height - 1; y >= 0; y-- {
			b := r.BitsSrc[y*bpl : y*bpl+bpl]
			p := img.Pix[ix*img.Stride : ix*img.Stride+img.Stride]
			for i, j := 0, 0; i < len(p); i, j = i+4, j+bpp {
				// color in BMP stored in BGR order
				p[i+0] = b[j+2]
				p[i+1] = b[j+1]
				p[i+2] = b[j+0]
				p[i+3] = 0xff
			}
			ix = ix + 1
		}
		return img
	}
	return nil
}

func (r *bitmapRecord) Draw(ctx *context) {
	img := r.readImage()
	if img == nil {
		return
	}
	if r.Type == EMR_ALPHABLEND {
		img = applySourceAlpha(img, uint8((r.BitBltRasterOperation>>16)&0xff))
	}
	if r.Type == EMR_TRANSPARENTBLT {
		img = applyTransparentColor(img, r.BkColorSrc)
	}

	destX, destY := r.xDest, r.yDest
	destW, destH := r.cxDest, r.cyDest
	if destW == 0 || destH == 0 {
		destX, destY = r.Bounds.Left, r.Bounds.Top
		destW, destH = r.Bounds.Width(), r.Bounds.Height()
	}
	if destW == 0 || destH == 0 {
		return
	}

	srcW, srcH := r.cxSrc, r.cySrc
	if r.Type == EMR_BITBLT || r.Type == EMR_ALPHABLEND {
		srcW, srcH = r.cxDest, r.cyDest
	}
	if srcW == 0 || srcH == 0 {
		srcW, srcH = int32(img.Bounds().Dx()), int32(img.Bounds().Dy())
	}
	if r.Type == EMR_STRETCHDIBITS || r.Type == EMR_SETDIBITSTODEVICE || r.Type == EMR_TRANSPARENTBLT || r.Type == EMR_STRETCHBLT || r.Type == EMR_BITBLT || r.Type == EMR_ALPHABLEND {
		srcRect := image.Rect(int(r.xSrc), int(r.ySrc), int(r.xSrc+srcW), int(r.ySrc+srcH))
		srcRect = srcRect.Intersect(img.Bounds())
		if !srcRect.Empty() {
			img = imaging.Crop(img, srcRect)
		}
	}

	left, top := transformPoint(ctx, float64(destX), float64(destY))
	right, bottom := transformPoint(ctx, float64(destX+destW), float64(destY+destH))
	if right < left {
		left, right = right, left
	}
	if bottom < top {
		top, bottom = bottom, top
	}
	if right <= left || bottom <= top {
		return
	}
	if img.Bounds().Dx() != right-left || img.Bounds().Dy() != bottom-top {
		img = imaging.Resize(img, right-left, bottom-top, imaging.CatmullRom)
	}

	destination := image.Rect(left, top, right, bottom)
	if r.Type == EMR_BITBLT || r.Type == EMR_STRETCHBLT {
		ctx.paintWithClip(func() { drawRasterOperation(ctx, destination, img, r.BitBltRasterOperation) })
		return
	}
	ctx.drawImage(destination, img)
}

func drawRasterOperation(ctx *context, dst image.Rectangle, src image.Image, operation uint32) {
	if (operation>>16)&0xff == 0xcc {
		ctx.drawImage(dst, src)
		return
	}
	for y := dst.Min.Y; y < dst.Max.Y; y++ {
		for x := dst.Min.X; x < dst.Max.X; x++ {
			if !imagePointInBounds(ctx, x, y) || !ctx.clipAllows(x, y) {
				continue
			}
			sx := src.Bounds().Min.X + (x-dst.Min.X)*src.Bounds().Dx()/maxInt(1, dst.Dx())
			sy := src.Bounds().Min.Y + (y-dst.Min.Y)*src.Bounds().Dy()/maxInt(1, dst.Dy())
			source := color.RGBAModel.Convert(src.At(sx, sy)).(color.RGBA)
			destination := color.RGBAModel.Convert(ctx.img.At(x, y)).(color.RGBA)
			result, ok := applyRasterOperation(source, destination, operation)
			if ok {
				ctx.img.Set(x, y, result)
			}
		}
	}
}

func applyRasterOperation(source, destination color.RGBA, operation uint32) (color.RGBA, bool) {
	switch (operation >> 16) & 0xff {
	case 0x00: // BLACKNESS
		return color.RGBA{A: 0xff}, true
	case 0xaa: // DST
		return destination, true
	case 0x55: // DSTINVERT
		return color.RGBA{R: ^destination.R, G: ^destination.G, B: ^destination.B, A: 0xff}, true
	case 0x66: // SRCINVERT
		return color.RGBA{R: source.R ^ destination.R, G: source.G ^ destination.G, B: source.B ^ destination.B, A: 0xff}, true
	case 0x88: // SRCAND
		return color.RGBA{R: source.R & destination.R, G: source.G & destination.G, B: source.B & destination.B, A: 0xff}, true
	case 0xee: // SRCPAINT
		return color.RGBA{R: source.R | destination.R, G: source.G | destination.G, B: source.B | destination.B, A: 0xff}, true
	case 0xff: // WHITENESS
		return color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, true
	default:
		return source, true
	}
}

func applySourceAlpha(src image.Image, alpha uint8) image.Image {
	if alpha == 0 {
		return image.NewRGBA(src.Bounds())
	}
	dst := image.NewRGBA(src.Bounds())
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			ca := uint8((a * uint32(alpha)) / 0xffff)
			dst.SetRGBA(x, y, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), ca})
		}
	}
	return dst
}

func applyTransparentColor(src image.Image, key ColorRef) image.Image {
	dst := image.NewRGBA(src.Bounds())
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			c := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
			if c.R == key.Red && c.G == key.Green && c.B == key.Blue {
				c.A = 0
			}
			dst.Set(x, y, c)
		}
	}
	return dst
}

type BitbltRecord struct {
	bitmapRecord
}

func readBitbltRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &BitbltRecord{}
	r.Record = Record{Type: EMR_BITBLT, Size: size}
	return r.read(reader)
}

type MaskbltRecord struct {
	Record
	Bounds          RectL
	XDest, YDest    int32
	CxDest, CyDest  int32
	RasterOperation uint32
	XSrc, YSrc      int32
	XformSrc        XForm
	BkColorSrc      ColorRef
	UsageSrc        uint32
	OffBmiSrc       uint32
	CbBmiSrc        uint32
	OffBitsSrc      uint32
	CbBitsSrc       uint32
	XMask, YMask    int32
	UsageMask       uint32
	OffBmiMask      uint32
	CbBmiMask       uint32
	OffBitsMask     uint32
	CbBitsMask      uint32
	Source          bitmapRecord
	Mask            bitmapRecord
}

func readMaskbltRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &MaskbltRecord{Record: Record{Type: EMR_MASKBLT, Size: size}}
	for _, value := range []interface{}{
		&r.Bounds, &r.XDest, &r.YDest, &r.CxDest, &r.CyDest,
		&r.RasterOperation, &r.XSrc, &r.YSrc, &r.XformSrc,
		&r.BkColorSrc, &r.UsageSrc, &r.OffBmiSrc, &r.CbBmiSrc,
		&r.OffBitsSrc, &r.CbBitsSrc, &r.XMask, &r.YMask,
		&r.UsageMask, &r.OffBmiMask, &r.CbBmiMask, &r.OffBitsMask,
		&r.CbBitsMask,
	} {
		if err := binary.Read(reader, binary.LittleEndian, value); err != nil {
			return nil, err
		}
	}

	sourceHeader, sourceTable, sourceBits, err := readDIBBlock(reader, r.OffBmiSrc, r.OffBitsSrc, r.CbBitsSrc)
	if err != nil {
		return nil, err
	}
	maskHeader, maskTable, maskBits, err := readDIBBlock(reader, r.OffBmiMask, r.OffBitsMask, r.CbBitsMask)
	if err != nil {
		return nil, err
	}
	r.Source = bitmapRecord{Record: r.Record, offBmiSrc: r.OffBmiSrc, BmiSrc: sourceHeader, ColorTable: sourceTable, BitsSrc: sourceBits}
	r.Mask = bitmapRecord{Record: r.Record, offBmiSrc: r.OffBmiMask, BmiSrc: maskHeader, ColorTable: maskTable, BitsSrc: maskBits}
	return r, nil
}

func (r *MaskbltRecord) Draw(ctx *context) {
	source := r.Source.readImage()
	mask := r.Mask.readImage()
	if source == nil || mask == nil || r.CxDest == 0 || r.CyDest == 0 {
		return
	}
	srcRect := image.Rect(int(r.XSrc), int(r.YSrc), int(r.XSrc+r.CxDest), int(r.YSrc+r.CyDest)).Intersect(source.Bounds())
	maskRect := image.Rect(int(r.XMask), int(r.YMask), int(r.XMask+r.CxDest), int(r.YMask+r.CyDest)).Intersect(mask.Bounds())
	if srcRect.Empty() || maskRect.Empty() {
		return
	}
	source = imaging.Crop(source, srcRect)
	mask = imaging.Crop(mask, maskRect)

	left, top := transformPoint(ctx, float64(r.XDest), float64(r.YDest))
	right, bottom := transformPoint(ctx, float64(r.XDest+r.CxDest), float64(r.YDest+r.CyDest))
	if right < left {
		left, right = right, left
	}
	if bottom < top {
		top, bottom = bottom, top
	}
	if right <= left || bottom <= top {
		return
	}
	if source.Bounds().Dx() != right-left || source.Bounds().Dy() != bottom-top {
		source = imaging.Resize(source, right-left, bottom-top, imaging.CatmullRom)
	}
	if mask.Bounds().Dx() != right-left || mask.Bounds().Dy() != bottom-top {
		mask = imaging.Resize(mask, right-left, bottom-top, imaging.NearestNeighbor)
	}
	foreground := ((r.RasterOperation >> 16) & 0xff) << 16
	background := ((r.RasterOperation >> 24) & 0xff) << 16
	drawMaskRasterOperation(ctx, image.Rect(left, top, right, bottom), source, mask, foreground, background)
}

func drawMaskRasterOperation(ctx *context, dst image.Rectangle, source, mask image.Image, foreground, background uint32) {
	ctx.paintWithClip(func() {
		for y := dst.Min.Y; y < dst.Max.Y; y++ {
			for x := dst.Min.X; x < dst.Max.X; x++ {
				if !imagePointInBounds(ctx, x, y) || !ctx.clipAllows(x, y) {
					continue
				}
				sx := source.Bounds().Min.X + (x-dst.Min.X)*source.Bounds().Dx()/maxInt(1, dst.Dx())
				sy := source.Bounds().Min.Y + (y-dst.Min.Y)*source.Bounds().Dy()/maxInt(1, dst.Dy())
				mx := mask.Bounds().Min.X + (x-dst.Min.X)*mask.Bounds().Dx()/maxInt(1, dst.Dx())
				my := mask.Bounds().Min.Y + (y-dst.Min.Y)*mask.Bounds().Dy()/maxInt(1, dst.Dy())
				selected := color.GrayModel.Convert(mask.At(mx, my)).(color.Gray).Y >= 128
				operation := background
				if selected {
					operation = foreground
				}
				result, _ := applyRasterOperation(
					color.RGBAModel.Convert(source.At(sx, sy)).(color.RGBA),
					color.RGBAModel.Convert(ctx.img.At(x, y)).(color.RGBA),
					operation,
				)
				ctx.img.Set(x, y, result)
			}
		}
	})
}

type StretchbltRecord struct {
	bitmapRecord
}

type AlphablendRecord struct {
	bitmapRecord
}

func readAlphablendRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &AlphablendRecord{}
	r.Record = Record{Type: EMR_ALPHABLEND, Size: size}
	return r.read(reader)
}

func readStretchbltRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &StretchbltRecord{}
	r.Record = Record{Type: EMR_STRETCHBLT, Size: size}
	return r.read(reader)
}

type StretchdibitsRecord struct {
	// brings two unused fields: XformSrc and BkColorSrc
	bitmapRecord
}

type SetdibitstodeviceRecord struct {
	bitmapRecord
}

func readSetdibitstodeviceRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &SetdibitstodeviceRecord{}
	r.Record = Record{Type: EMR_SETDIBITSTODEVICE, Size: size}
	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.xDest); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.yDest); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.xSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.ySrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.cxSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.cySrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.offBmiSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.cbBmiSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.offBitsSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.cbBitsSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.UsageSrc); err != nil {
		return nil, err
	}
	var startScan, scanCount uint32
	if err := binary.Read(reader, binary.LittleEndian, &startScan); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &scanCount); err != nil {
		return nil, err
	}
	r.cxDest = r.cxSrc
	r.cyDest = r.cySrc
	if r.offBmiSrc == 0 {
		return r, nil
	}
	if r.offBmiSrc < 76 {
		return nil, fmt.Errorf("invalid EMR_SETDIBITSTODEVICE bitmap offset %d", r.offBmiSrc)
	}
	if _, err := reader.Seek(int64(r.offBmiSrc)-76, io.SeekCurrent); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.BmiSrc); err != nil {
		return nil, err
	}
	colorTableSize := int64(r.offBitsSrc) - int64(r.offBmiSrc) - 40
	if colorTableSize > 0 {
		var err error
		r.ColorTable, err = readRecordBytes(reader, uint64(colorTableSize))
		if err != nil {
			return nil, err
		}
	}
	var err error
	r.BitsSrc, err = readRecordBytes(reader, uint64(r.cbBitsSrc))
	if err != nil {
		return nil, err
	}
	return r, nil
}

type TransparentbltRecord struct {
	bitmapRecord
}

func readTransparentbltRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &TransparentbltRecord{}
	r.Record = Record{Type: EMR_TRANSPARENTBLT, Size: size}
	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.xDest); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.yDest); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.cxDest); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.cyDest); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.xSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.ySrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.cxSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.cySrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.XformSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.BkColorSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.UsageSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.offBmiSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.cbBmiSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.offBitsSrc); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.cbBitsSrc); err != nil {
		return nil, err
	}
	if r.offBmiSrc == 0 {
		return r, nil
	}
	if r.offBmiSrc < 104 {
		return nil, fmt.Errorf("invalid EMR_TRANSPARENTBLT bitmap offset %d", r.offBmiSrc)
	}
	if _, err := reader.Seek(int64(r.offBmiSrc)-104, io.SeekCurrent); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.BmiSrc); err != nil {
		return nil, err
	}
	colorTableSize := int64(r.offBitsSrc) - int64(r.offBmiSrc) - 40
	if colorTableSize > 0 {
		var err error
		r.ColorTable, err = readRecordBytes(reader, uint64(colorTableSize))
		if err != nil {
			return nil, err
		}
	}
	var err error
	r.BitsSrc, err = readRecordBytes(reader, uint64(r.cbBitsSrc))
	if err != nil {
		return nil, err
	}
	return r, nil
}

func readStretchdibitsRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	r := &StretchdibitsRecord{}
	r.Record = Record{Type: EMR_STRETCHDIBITS, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.Bounds); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.xDest); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.yDest); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.xSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.ySrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cxSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cySrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.offBmiSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cbBmiSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.offBitsSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cbBitsSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.UsageSrc); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.BitBltRasterOperation); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cxDest); err != nil {
		return nil, err
	}

	if err := binary.Read(reader, binary.LittleEndian, &r.cyDest); err != nil {
		return nil, err
	}

	// no bitmap data
	if r.offBmiSrc == 0 {
		return r, nil
	}
	if r.offBmiSrc < 80 {
		return nil, fmt.Errorf("invalid EMR_STRETCHDIBITS bitmap offset %d", r.offBmiSrc)
	}

	// BitmapBuffer
	// skipping UndefinedSpace1
	if _, err := reader.Seek(int64(r.offBmiSrc)-80, io.SeekCurrent); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.BmiSrc); err != nil {
		return nil, err
	}

	// Read ColorTable
	colorTableSize := int64(r.offBitsSrc) - int64(r.offBmiSrc) - 40
	if colorTableSize > 0 {
		var err error
		r.ColorTable, err = readRecordBytes(reader, uint64(colorTableSize))
		if err != nil {
			return nil, err
		}
	}

	var err error
	r.BitsSrc, err = readRecordBytes(reader, uint64(r.cbBitsSrc))
	if err != nil {
		return nil, err
	}

	return r, nil
}

type PatternBrushRecord struct {
	Record
	ihBrush    uint32
	Usage      uint32
	offBmi     uint32
	cbBmi      uint32
	offBits    uint32
	cbBits     uint32
	BmiSrc     BitmapInfoHeader
	ColorTable []byte
	BitsSrc    []byte
}

func readPatternBrushRecord(reader *bytes.Reader, size uint32, recType uint32) (Recorder, error) {
	r := &PatternBrushRecord{}
	r.Record = Record{Type: recType, Size: size}

	if err := binary.Read(reader, binary.LittleEndian, &r.ihBrush); err != nil {
		return nil, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &r.Usage); err != nil {
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

	if r.offBmi == 0 {
		return r, nil
	}

	// Seek to BmiSrc relative to the start of the record.
	// The fields before BmiSrc are: Type (4), Size (4), ihBrush (4), Usage (4), offBmi (4), cbBmi (4), offBits (4), cbBits (4).
	// Total size of these fields is 32 bytes.
	// So we seek relative to 32.
	reader.Seek(int64(r.offBmi-32), io.SeekCurrent)
	if err := binary.Read(reader, binary.LittleEndian, &r.BmiSrc); err != nil {
		return nil, err
	}

	// Read ColorTable and BitsSrc
	colorTableSize := int64(r.offBits) - int64(r.offBmi) - 40
	if colorTableSize > 0 {
		var err error
		r.ColorTable, err = readRecordBytes(reader, uint64(colorTableSize))
		if err != nil {
			return nil, err
		}
	}

	var err error
	r.BitsSrc, err = readRecordBytes(reader, uint64(r.cbBits))
	if err != nil {
		return nil, err
	}

	return r, nil
}

func readCreatemonobrushRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	return readPatternBrushRecord(reader, size, EMR_CREATEMONOBRUSH)
}

func readCreatedibpatternbrushptRecord(reader *bytes.Reader, size uint32) (Recorder, error) {
	return readPatternBrushRecord(reader, size, EMR_CREATEDIBPATTERNBRUSHPT)
}

func (r *PatternBrushRecord) Draw(ctx *context) {
	ctx.objects[r.ihBrush] = r
}

func (r *PatternBrushRecord) getColor(idx int) color.RGBA {
	offset := idx * 4
	if offset+3 < len(r.ColorTable) {
		return color.RGBA{
			R: r.ColorTable[offset+2],
			G: r.ColorTable[offset+1],
			B: r.ColorTable[offset+0],
			A: 0xff,
		}
	}
	if r.BmiSrc.BitCount == BI_BITCOUNT_1 {
		if idx == 0 {
			return color.RGBA{0, 0, 0, 0}
		}
		return color.RGBA{255, 255, 255, 0xff}
	}
	val := uint8(idx)
	if r.BmiSrc.BitCount == BI_BITCOUNT_2 {
		val = uint8(idx * 17)
	}
	return color.RGBA{val, val, val, 0xff}
}
