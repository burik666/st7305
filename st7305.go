//go:build tinygo

// Package st7305 provides a TinyGo driver for the ST7305 monochrome RLCD
// controller as used on the Waveshare ESP32-S3-RLCD-4.2 (400×300 pixels, 1 bpp).
//
// The driver implements the [drivers.Displayer] interface from
// tinygo.org/x/drivers, so it can be used as a drop-in display backend:
//
//	d := st7305.New(spi, csPin, dcPin, rstPin)
//	d.Configure()
//	d.SetPixel(10, 20, color.RGBA{0, 0, 0, 255}) // black pixel
//	d.Display()
//
// Memory footprint: 15 000 bytes for the frame buffer; no heap allocations
// after [Device.Configure] returns.
//
// Init sequence ported from the Waveshare reference firmware:
// https://github.com/78/xiaozhi-esp32/blob/main/main/boards/waveshare/esp32-s3-rlcd-4.2/custom_lcd_display.cc
package st7305

import (
	"image/color"
	"machine"
	"time"

	"tinygo.org/x/drivers"
)

// Ensure Device satisfies drivers.Displayer at compile time.
var _ drivers.Displayer = (*Device)(nil)

// Panel dimensions in pixels.
const (
	Width  = 400
	Height = 300
)

// Internal layout constants derived from the panel geometry.
// The ST7305 stores 4 rows per column-byte, so the frame buffer is
// arranged as (Width/2) columns × (Height/4) bytes per column.
const (
	rowsPerByte = 4
	colBytes    = Height / rowsPerByte // 75 bytes per column group
	frameBytes  = (Width / 2) * colBytes // 15 000 bytes total
)

// Black and White are convenience colours for use with [Device.SetPixel],
// [Device.Fill], [Device.DrawLine], etc.
var (
	Black = color.RGBA{0, 0, 0, 255}
	White = color.RGBA{255, 255, 255, 255}
)

// Device holds the SPI bus, control pins, and the 15 kB frame buffer.
// Create one with [New] and call [Device.Configure] before use.
type Device struct {
	bus    drivers.SPI
	cs     machine.Pin
	dc     machine.Pin
	rst    machine.Pin
	buf    [frameBytes]byte
	cmdBuf [1]byte // scratch buffer for single-byte SPI writes
}

// New returns a new Device bound to the given SPI bus and control pins.
// Pass machine.NoPin for rst if the reset pin is not connected.
func New(bus drivers.SPI, cs, dc, rst machine.Pin) *Device {
	return &Device{bus: bus, cs: cs, dc: dc, rst: rst}
}

// Configure sets up the GPIO pins, resets the panel, runs the init sequence,
// and fills the frame buffer with white.
// Call this once before any drawing or [Device.Display] calls.
func (d *Device) Configure() {
	d.cs.Configure(machine.PinConfig{Mode: machine.PinOutput})
	d.dc.Configure(machine.PinConfig{Mode: machine.PinOutput})
	if d.rst != machine.NoPin {
		d.rst.Configure(machine.PinConfig{Mode: machine.PinOutput})
	}

	d.cs.High()
	d.dc.High()

	d.hardReset()
	d.initRegs()
	d.Fill(White)
}

// ── drivers.Displayer ─────────────────────────────────────────────────────────

// Size returns the panel resolution: (400, 300).
func (d *Device) Size() (int16, int16) { return Width, Height }

// SetPixel draws a single pixel into the in-memory frame buffer.
// The colour is converted to a 1-bit value using the ITU-R BT.601 luma formula:
// pixels below ~50 % brightness are drawn black, all others white.
// Out-of-bounds coordinates are silently ignored.
func (d *Device) SetPixel(x, y int16, c color.RGBA) {
	if uint16(x) >= Width || uint16(y) >= Height {
		return
	}
	d.setPixelFast(x, y, lumaBlack(c))
}

// Display transfers the complete frame buffer to the panel over SPI.
// Call this after all drawing calls for a frame.
func (d *Device) Display() error {
	d.cmdData(0x2A, 0x12, 0x2A) // CASET: panel-specific column window
	d.cmdData(0x2B, 0x00, 0xC7) // RASET: row window

	// The Waveshare firmware requires CS to remain asserted continuously
	// across the RAMWR command byte and all subsequent pixel data.
	d.cs.Low()
	d.dc.Low()
	d.cmdBuf[0] = 0x2C // RAMWR
	d.bus.Tx(d.cmdBuf[:], nil)
	d.dc.High()
	d.bus.Tx(d.buf[:], nil)
	d.cs.High()

	return nil
}

// ── Drawing primitives ────────────────────────────────────────────────────────

// Fill sets every pixel in the frame buffer to c.
// This is much faster than calling SetPixel for every pixel individually:
// it writes all 15 000 bytes in a single loop without per-pixel address math.
func (d *Device) Fill(c color.RGBA) {
	var v byte
	if !lumaBlack(c) {
		v = 0xFF // white: all bits set
	}
	for i := range d.buf {
		d.buf[i] = v
	}
}

// DrawLine draws a 1-pixel-wide line from (x0, y0) to (x1, y1) using
// Bresenham's algorithm. No heap allocations.
func (d *Device) DrawLine(x0, y0, x1, y1 int16, c color.RGBA) {
	black := lumaBlack(c)
	dx := abs16(x1 - x0)
	dy := abs16(y1 - y0)
	sx := int16(1)
	if x0 > x1 {
		sx = -1
	}
	sy := int16(1)
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy

	for {
		if uint16(x0) < Width && uint16(y0) < Height {
			d.setPixelFast(x0, y0, black)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := err * 2
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// DrawRect draws the outline of an axis-aligned rectangle with top-left corner
// at (x, y) and the given width and height.
func (d *Device) DrawRect(x, y, w, h int16, c color.RGBA) {
	x1 := x + w - 1
	y1 := y + h - 1
	d.DrawLine(x, y, x1, y, c)   // top
	d.DrawLine(x, y1, x1, y1, c) // bottom
	d.DrawLine(x, y, x, y1, c)   // left
	d.DrawLine(x1, y, x1, y1, c) // right
}

// FillRect fills an axis-aligned rectangle with top-left corner at (x, y)
// and the given width and height.
func (d *Device) FillRect(x, y, w, h int16, c color.RGBA) {
	black := lumaBlack(c)
	// Clamp to panel bounds.
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > Width {
		w = Width - x
	}
	if y+h > Height {
		h = Height - y
	}
	if w <= 0 || h <= 0 {
		return
	}
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			d.setPixelFast(col, row, black)
		}
	}
}

// DrawCircle draws the outline of a circle with centre (cx, cy) and radius r
// using the midpoint circle algorithm. No heap allocations.
func (d *Device) DrawCircle(cx, cy, r int16, c color.RGBA) {
	black := lumaBlack(c)
	x := r
	y := int16(0)
	err := int16(0)

	for x >= y {
		d.setIfInBounds(cx+x, cy+y, black)
		d.setIfInBounds(cx+y, cy+x, black)
		d.setIfInBounds(cx-y, cy+x, black)
		d.setIfInBounds(cx-x, cy+y, black)
		d.setIfInBounds(cx-x, cy-y, black)
		d.setIfInBounds(cx-y, cy-x, black)
		d.setIfInBounds(cx+y, cy-x, black)
		d.setIfInBounds(cx+x, cy-y, black)

		y++
		if err <= 0 {
			err += 2*y + 1
		} else {
			x--
			err += 2*(y-x) + 1
		}
	}
}

// FillCircle fills a circle with centre (cx, cy) and radius r.
func (d *Device) FillCircle(cx, cy, r int16, c color.RGBA) {
	black := lumaBlack(c)
	x := r
	y := int16(0)
	err := int16(0)

	for x >= y {
		d.hline(cx-x, cx+x, cy+y, black)
		d.hline(cx-x, cx+x, cy-y, black)
		d.hline(cx-y, cx+y, cy+x, black)
		d.hline(cx-y, cx+y, cy-x, black)

		y++
		if err <= 0 {
			err += 2*y + 1
		} else {
			x--
			err += 2*(y-x) + 1
		}
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// lumaBlack returns true if c should be rendered as black.
// Uses ITU-R BT.601 coefficients scaled by 1000.
func lumaBlack(c color.RGBA) bool {
	return uint32(c.R)*299+uint32(c.G)*587+uint32(c.B)*114 < 50_000
}

// setPixelFast writes one pixel without bounds checking.
// Caller must ensure 0 ≤ x < Width and 0 ≤ y < Height.
func (d *Device) setPixelFast(x, y int16, black bool) {
	// The panel stores pixels bottom-to-top within each column group.
	// Pixel address formula (derived from Waveshare InitLandscapeLUT):
	//   invY  = (Height - 1) - y
	//   index = (x >> 1) * colBytes + (invY >> 2)
	//   bit   = 7 - (((invY & 3) << 1) | (x & 1))
	invY := uint16(Height-1) - uint16(y)
	idx := (uint16(x)>>1)*colBytes + (invY >> 2)
	bit := uint8(7 - (((invY&3)<<1) | uint16(x&1)))

	if black {
		d.buf[idx] &^= 1 << bit
	} else {
		d.buf[idx] |= 1 << bit
	}
}

// setIfInBounds calls setPixelFast only when (x, y) is within the panel.
func (d *Device) setIfInBounds(x, y int16, black bool) {
	if uint16(x) < Width && uint16(y) < Height {
		d.setPixelFast(x, y, black)
	}
}

// hline draws a horizontal span from x0 to x1 (inclusive) at row y,
// clamping to the panel bounds.
func (d *Device) hline(x0, x1, y int16, black bool) {
	if uint16(y) >= Height {
		return
	}
	if x0 < 0 {
		x0 = 0
	}
	if x1 >= Width {
		x1 = Width - 1
	}
	for x := x0; x <= x1; x++ {
		d.setPixelFast(x, y, black)
	}
}

func abs16(x int16) int16 {
	if x < 0 {
		return -x
	}
	return x
}

// ── Hardware initialisation ───────────────────────────────────────────────────

// initRegs sends the vendor-specified register sequence to the ST7305.
// Values are taken verbatim from the Waveshare reference driver.
func (d *Device) initRegs() {
	d.cmdData(0xD6, 0x17, 0x02)
	d.cmdData(0xD1, 0x01)
	d.cmdData(0xC0, 0x11, 0x04)
	d.cmdData(0xC1, 0x69, 0x69, 0x69, 0x69)
	d.cmdData(0xC2, 0x19, 0x19, 0x19, 0x19)
	d.cmdData(0xC4, 0x4B, 0x4B, 0x4B, 0x4B)
	d.cmdData(0xC5, 0x19, 0x19, 0x19, 0x19)
	d.cmdData(0xD8, 0x80, 0xE9)
	d.cmdData(0xB2, 0x02)
	d.cmdData(0xB3, 0xE5, 0xF6, 0x05, 0x46, 0x77, 0x77, 0x77, 0x77, 0x76, 0x45)
	d.cmdData(0xB4, 0x05, 0x46, 0x77, 0x77, 0x77, 0x77, 0x76, 0x45)
	d.cmdData(0x62, 0x32, 0x03, 0x1F)
	d.cmdData(0xB7, 0x13)
	d.cmdData(0xB0, 0x64)
	d.cmd(0x11)                  // Sleep Out
	time.Sleep(200 * time.Millisecond)
	d.cmdData(0xC9, 0x00)
	d.cmdData(0x36, 0x48)        // MADCTL: landscape orientation
	d.cmdData(0x3A, 0x11)        // COLMOD: 1 bpp
	d.cmdData(0xB9, 0x20)
	d.cmdData(0xB8, 0x29)
	d.cmd(0x21)                  // Display Inversion On
	d.cmdData(0x2A, 0x12, 0x2A) // CASET
	d.cmdData(0x2B, 0x00, 0xC7) // RASET
	d.cmdData(0x35, 0x00)        // Tearing Effect Line On
	d.cmdData(0xD0, 0xFF)
	d.cmd(0x38)                  // Idle Mode Off
	d.cmd(0x29)                  // Display On
}

// ── SPI helpers ───────────────────────────────────────────────────────────────

// hardReset pulses the RST pin to perform a hardware reset.
// Does nothing if rst was set to machine.NoPin.
func (d *Device) hardReset() {
	if d.rst == machine.NoPin {
		return
	}
	d.rst.High()
	time.Sleep(50 * time.Millisecond)
	d.rst.Low()
	time.Sleep(20 * time.Millisecond)
	d.rst.High()
	time.Sleep(50 * time.Millisecond)
}

// cmd sends a single command byte with DC low (command mode).
func (d *Device) cmd(c byte) {
	d.cs.Low()
	d.dc.Low()
	d.cmdBuf[0] = c
	d.bus.Tx(d.cmdBuf[:], nil)
	d.cs.High()
}

// data sends one or more parameter bytes with DC high (data mode).
func (d *Device) data(b ...byte) {
	d.cs.Low()
	d.dc.High()
	d.bus.Tx(b, nil)
	d.cs.High()
}

// cmdData sends a command byte followed by zero or more parameter bytes.
func (d *Device) cmdData(c byte, b ...byte) {
	d.cmd(c)
	if len(b) > 0 {
		d.data(b...)
	}
}
