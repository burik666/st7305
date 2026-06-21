//go:build tinygo

// Example: demonstrates all st7305 driver features.
//
// Each step renders a different scene and holds it for one second:
//
//  1. Fill           — clear the screen to black then white
//  2. SetPixel       — checkerboard pattern + luma-conversion check
//  3. DrawLine       — diagonal cross and a line fan (Bresenham)
//  4. DrawRect       — nested concentric outlines
//  5. FillRect       — vertical stripes with white cutouts
//  6. DrawCircle     — concentric circles (midpoint algorithm)
//  7. FillCircle     — filled circles combined with outlines
//  8. Final scene    — all primitives composed together
//
// Flash with:
//
//	tinygo flash -target=<your-target> ./examples/basic
//
// Adjust the pin constants below to match your board wiring.
package main

import (
	"image/color"
	"machine"
	"time"

	"github.com/burik666/st7305"
)

// ── Pin assignment — edit to match your board ─────────────────────────────────
//
// ESP32 / ESP32-S3 pin names use the GPIO numbering scheme: machine.GPIO0,
// machine.GPIO1, etc. The GP* aliases exist only on RP2040/RP2350 (Pico).
//
// Waveshare ESP32-S3-RLCD-4.2 verified wiring:
//
//	SCK  → GPIO11   (RLCD_SCK,   set in SPIConfig below)
//	MOSI → GPIO12   (RLCD_DIN,   set in SPIConfig below)
//	CS   → GPIO40   (RLCD_CS)
//	DC   → GPIO5    (RLCD_DS, Data/Command select)
//	RST  → GPIO41   (RLCD_RESET, or machine.NoPin if not connected)

const (
	pinSCK = machine.GPIO11
	pinSDO = machine.GPIO12
	pinCS  = machine.GPIO40
	pinDC  = machine.GPIO5
	pinRST = machine.GPIO41 // use machine.NoPin if RST is not connected
)

// ─────────────────────────────────────────────────────────────────────────────

func main() {
	// On ESP32 / ESP32-S3, SPI0 is the general-purpose user SPI bus.
	// SCK and SDO (MOSI) must be specified explicitly.
	spi := machine.SPI0
	spi.Configure(machine.SPIConfig{
		Frequency: 40_000_000,
		Mode:      0,
		SCK:       pinSCK,
		SDO:       pinSDO,
		SDI:       machine.NoPin,
	})

	d := st7305.New(spi, pinCS, pinDC, pinRST)
	d.Configure() // initialises the panel; frame buffer is filled white

	time.Sleep(500 * time.Millisecond)

	// ── 1. Fill ───────────────────────────────────────────────────────────────
	// The fastest way to clear the screen: a single pass over the 15 kB buffer.

	d.Fill(st7305.Black)
	d.Display()
	time.Sleep(500 * time.Millisecond)

	d.Fill(st7305.White)
	d.Display()
	time.Sleep(500 * time.Millisecond)

	// ── 2. SetPixel ───────────────────────────────────────────────────────────
	// Draws individual pixels. Accepts any color.RGBA; brightness is converted
	// to 1-bit via the ITU-R BT.601 luma formula (threshold at ~50 %).

	// 16×16 checkerboard in the top-left corner.
	for py := int16(0); py < 16; py++ {
		for px := int16(0); px < 16; px++ {
			if (px+py)%2 == 0 {
				d.SetPixel(px, py, st7305.Black)
			}
		}
	}

	// Luma conversion check: dark grey → black, light grey → white.
	d.SetPixel(20, 4, color.RGBA{0x10, 0x10, 0x10, 0xFF}) // dark  → black
	d.SetPixel(22, 4, color.RGBA{0xC0, 0xC0, 0xC0, 0xFF}) // light → white

	d.Display()
	time.Sleep(time.Second)

	// ── 3. DrawLine ───────────────────────────────────────────────────────────
	// Bresenham's algorithm — any direction, no heap allocations.

	d.Fill(st7305.White)

	// Full-screen diagonal cross.
	d.DrawLine(0, 0, st7305.Width-1, st7305.Height-1, st7305.Black)
	d.DrawLine(st7305.Width-1, 0, 0, st7305.Height-1, st7305.Black)

	// Fan of lines from the left-centre to the top and bottom edges.
	for i := int16(0); i <= 10; i++ {
		y := i * (st7305.Height / 10)
		d.DrawLine(0, st7305.Height/2, st7305.Width-1, y, st7305.Black)
	}

	d.Display()
	time.Sleep(time.Second)

	// ── 4. DrawRect ───────────────────────────────────────────────────────────
	// Draws the outline of an axis-aligned rectangle.

	d.Fill(st7305.White)

	for i := int16(0); i < 8; i++ {
		margin := i * 16
		d.DrawRect(
			margin, margin,
			st7305.Width-2*margin,
			st7305.Height-2*margin,
			st7305.Black,
		)
	}

	d.Display()
	time.Sleep(time.Second)

	// ── 5. FillRect ───────────────────────────────────────────────────────────
	// Fills a solid axis-aligned rectangle.
	// Vertical black stripes, then white cutouts drawn on top.

	d.Fill(st7305.White)

	stripeW := int16(20)
	for col := int16(0); col < st7305.Width; col += stripeW * 2 {
		d.FillRect(col, 0, stripeW, st7305.Height, st7305.Black)
	}
	for row := int16(10); row < st7305.Height-10; row += 40 {
		d.FillRect(80, row, 240, 20, st7305.White)
	}

	d.Display()
	time.Sleep(time.Second)

	// ── 6. DrawCircle ─────────────────────────────────────────────────────────
	// Midpoint circle algorithm — 8 symmetric points per iteration.

	d.Fill(st7305.White)

	cx := int16(st7305.Width / 2)
	cy := int16(st7305.Height / 2)
	for r := int16(10); r < 140; r += 15 {
		d.DrawCircle(cx, cy, r, st7305.Black)
	}

	d.Display()
	time.Sleep(time.Second)

	// ── 7. FillCircle ─────────────────────────────────────────────────────────
	// Three filled circles on a black background; outlines drawn on top.

	d.Fill(st7305.Black)

	d.FillCircle(100, st7305.Height/2, 60, st7305.White)
	d.FillCircle(200, st7305.Height/2, 60, st7305.Black) // hole punched in white
	d.FillCircle(300, st7305.Height/2, 60, st7305.White)

	d.DrawCircle(100, st7305.Height/2, 60, st7305.Black)
	d.DrawCircle(200, st7305.Height/2, 60, st7305.White)
	d.DrawCircle(300, st7305.Height/2, 60, st7305.Black)

	d.Display()
	time.Sleep(time.Second)

	// ── 8. Final scene — all primitives together ──────────────────────────────

	d.Fill(st7305.White)

	// Double border.
	d.DrawRect(0, 0, st7305.Width, st7305.Height, st7305.Black)
	d.DrawRect(2, 2, st7305.Width-4, st7305.Height-4, st7305.Black)

	// Solid header bar.
	d.FillRect(0, 0, st7305.Width, 30, st7305.Black)

	// White circles in the header as decoration.
	for i := int16(0); i < 5; i++ {
		d.FillCircle(40+i*80, 15, 10, st7305.White)
		d.DrawCircle(40+i*80, 15, 10, st7305.Black)
	}

	// Grid of lines filling the body.
	for y := int16(40); y < st7305.Height-10; y += 20 {
		d.DrawLine(10, y, st7305.Width-10, y, st7305.Black)
	}
	for x := int16(10); x < st7305.Width-10; x += 20 {
		d.DrawLine(x, 40, x, st7305.Height-10, st7305.Black)
	}

	// Large circle centred on the grid.
	d.FillCircle(st7305.Width/2, st7305.Height/2+15, 70, st7305.White)
	d.DrawCircle(st7305.Width/2, st7305.Height/2+15, 70, st7305.Black)
	d.DrawCircle(st7305.Width/2, st7305.Height/2+15, 50, st7305.Black)

	d.Display()

	// Hold the final frame.
	for {
		time.Sleep(time.Hour)
	}
}
