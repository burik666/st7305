# st7305

TinyGo driver for the **ST7305 monochrome RLCD controller** as used on the
[Waveshare ESP32-S3-RLCD-4.2](https://www.waveshare.com/esp32-s3-rlcd-4.2.htm)
(400 × 300 pixels, 1 bpp).

Tested on real hardware.

## Features

- Implements [`drivers.Displayer`](https://pkg.go.dev/tinygo.org/x/drivers#Displayer)
- **15 000 byte** frame buffer — no heap allocations after `Configure()`
- ITU-R BT.601 luma conversion for `SetPixel` colour input
- Hardware reset support (optional, pass `machine.NoPin` if not wired)
- Drawing primitives: lines, rectangles, circles (outline and filled)

## Wiring (Waveshare ESP32-S3-RLCD-4.2)

| Signal   | ESP32-S3 pin |
|----------|--------------|
| SPI CLK  | GPIO 11      |
| SPI MOSI | GPIO 12      |
| CS       | GPIO 40      |
| DC       | GPIO 5       |
| RST      | GPIO 41 (or `machine.NoPin`) |

## Installation

```
go get github.com/burik666/st7305
```

## Usage

```go
package main

import (
    "image/color"
    "machine"
    "time"

    "github.com/burik666/st7305"
)

func main() {
    spi := machine.SPI0
    spi.Configure(machine.SPIConfig{
        Frequency: 40_000_000,
        Mode:      0,
        SCK:       machine.GPIO11,
        SDO:       machine.GPIO12,
        SDI:       machine.NoPin,
    })

    display := st7305.New(spi, machine.GPIO40, machine.GPIO5, machine.GPIO41)
    display.Configure()

    // Draw a black diagonal line
    for i := int16(0); i < 300; i++ {
        display.SetPixel(i, i, color.RGBA{0, 0, 0, 255})
    }
    display.Display()

    for {
        time.Sleep(time.Hour)
    }
}
```

Flash the example:

```
tinygo flash -target=<your-target> ./examples/basic
```

## API

### Constructor

```go
func New(bus drivers.SPI, cs, dc, rst machine.Pin) *Device
```

### Setup

```go
func (d *Device) Configure()
```

Configures GPIO pins, resets the panel, runs the init sequence, and fills the
frame buffer with white. Call once before any drawing.

### drivers.Displayer interface

```go
func (d *Device) Size() (int16, int16)             // → (400, 300)
func (d *Device) SetPixel(x, y int16, c color.RGBA)
func (d *Device) Display() error
```

`SetPixel` accepts any `color.RGBA`; brightness is converted to 1-bit via the
ITU-R BT.601 luma formula (threshold at ~50 %).

### Drawing primitives

```go
func (d *Device) Fill(c color.RGBA)
func (d *Device) DrawLine(x0, y0, x1, y1 int16, c color.RGBA)
func (d *Device) DrawRect(x, y, w, h int16, c color.RGBA)
func (d *Device) FillRect(x, y, w, h int16, c color.RGBA)
func (d *Device) DrawCircle(cx, cy, r int16, c color.RGBA)
func (d *Device) FillCircle(cx, cy, r int16, c color.RGBA)
```

| Method | Description |
|--------|-------------|
| `Fill` | Clears the entire frame buffer to a single colour (fastest way to clear) |
| `DrawLine` | Bresenham line from (x0, y0) to (x1, y1) |
| `DrawRect` | Outline of an axis-aligned rectangle |
| `FillRect` | Solid axis-aligned rectangle |
| `DrawCircle` | Outline of a circle (midpoint algorithm) |
| `FillCircle` | Solid filled circle |

### Convenience colours

```go
st7305.Black  // color.RGBA{0, 0, 0, 255}
st7305.White  // color.RGBA{255, 255, 255, 255}
```

### Panel constants

```go
st7305.Width  = 400
st7305.Height = 300
```

## How the frame buffer works

The ST7305 uses an unusual 1 bpp layout: two horizontal pixels share one
nibble, and four rows share one byte per column group. The frame buffer is
`(Width/2) × (Height/4)` = **15 000 bytes**. The pixel address formula is:

```
invY  = (Height - 1) - y
index = (x >> 1) * 75 + (invY >> 2)
bit   = 7 − (((invY & 3) << 1) | (x & 1))
```

This matches the `InitLandscapeLUT` function in the
[Waveshare reference firmware](https://github.com/78/xiaozhi-esp32/blob/main/main/boards/waveshare/esp32-s3-rlcd-4.2/custom_lcd_display.cc).

## License

MIT — see [LICENSE](LICENSE).
