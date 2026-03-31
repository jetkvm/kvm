package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"time"
)

const (
	// Stripe pattern layout:
	// 3 sync stripes (black, white, black) + 43 data stripes = 46 stripes
	// Each stripe: 4px wide + 1px black separator = 5px per stripe
	// Total: 46 * 5 = 230px width, but we use full height for robustness
	stripeWidth    = 4  // pixels per stripe
	stripeSep      = 1  // separator between stripes
	stripeStep     = stripeWidth + stripeSep
	numSyncStripes = 3  // sync pattern: black, white, black
	numDataBits    = 43 // enough for unix millis until year 2250
	totalStripes   = numSyncStripes + numDataBits
	windowWidth    = totalStripes * stripeStep // 230px
	windowHeight   = 200
)

// renderStripePattern renders a binary stripe pattern encoding the given timestamp
// into a BGRA pixel buffer. Each vertical stripe represents one bit.
// Sync pattern (3 stripes): black, white, black — lets the decoder find alignment.
// Data: 43 stripes encoding the millisecond timestamp, MSB first.
func renderStripePattern(buf []byte, timestampMs int64) {
	// Clear to black
	for i := range buf {
		buf[i] = 0
	}
	// Set alpha to 255 for all pixels
	for i := 3; i < len(buf); i += 4 {
		buf[i] = 255
	}

	// Encode sync stripes: black(0), white(1), black(0)
	syncBits := [numSyncStripes]bool{false, true, false}
	for i, on := range syncBits {
		if on {
			fillStripe(buf, i, 255) // white
		}
		// black stripes are already 0
	}

	// Encode data bits, MSB first
	for bit := 0; bit < numDataBits; bit++ {
		shift := uint(numDataBits - 1 - bit)
		if (timestampMs>>shift)&1 == 1 {
			fillStripe(buf, numSyncStripes+bit, 255) // white
		}
	}
}

// fillStripe fills a single vertical stripe with the given grayscale value.
func fillStripe(buf []byte, stripeIndex int, val uint8) {
	x0 := stripeIndex * stripeStep
	for y := 0; y < windowHeight; y++ {
		for dx := 0; dx < stripeWidth; dx++ {
			x := x0 + dx
			off := (y*windowWidth + x) * 4
			buf[off+0] = val // B
			buf[off+1] = val // G
			buf[off+2] = val // R
			// alpha already set
		}
	}
}

// runStripeDisplay opens an X11 window and renders a binary stripe pattern
// encoding the current timestamp. Each frame carries its own timestamp for
// end-to-end video pipeline latency measurement.
func (a *Agent) runQRDisplay(ctx context.Context) {
	defer func() {
		a.qrMu.Lock()
		a.qrCancel = nil
		a.qrMu.Unlock()
	}()

	x11, err := X11Connect()
	if err != nil {
		log.Printf("ERROR: stripe display failed to connect to X11: %v", err)
		return
	}
	defer x11.Close()

	wid, err := x11.CreateWindow(0, 0, windowWidth, windowHeight)
	if err != nil {
		log.Printf("ERROR: stripe display failed to create window: %v", err)
		return
	}
	defer x11.DestroyWindow(wid)

	if err := x11.MapWindow(wid); err != nil {
		log.Printf("ERROR: stripe display failed to map window: %v", err)
		return
	}

	gc, err := x11.CreateGC(wid)
	if err != nil {
		log.Printf("ERROR: stripe display failed to create GC: %v", err)
		return
	}

	log.Printf("Stripe latency display started (%dx%d, %d data bits)", windowWidth, windowHeight, numDataBits)

	pixelBuf := make([]byte, windowWidth*windowHeight*4)

	// Drain X11 events/errors asynchronously
	go func() {
		discard := make([]byte, 4096)
		for {
			if _, err := x11.conn.Read(discard); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(2 * time.Millisecond) // fast but allows X11 to complete each frame
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Stripe latency display stopped")
			return
		case <-ticker.C:
		}

		renderStripePattern(pixelBuf, time.Now().UnixMilli())

		if err := x11.PutImage(wid, gc, windowWidth, windowHeight, pixelBuf); err != nil {
			log.Printf("ERROR: stripe display PutImage failed: %v", err)
			return
		}
	}
}

// startVisualNoise launches a fullscreen terminal blasting random base64 text.
// This stresses the video encoder with high-motion content.
func (a *Agent) startVisualNoise() {
	a.noiseMu.Lock()
	defer a.noiseMu.Unlock()

	a.stopVisualNoiseLocked()

	ctx, cancel := context.WithCancel(context.Background())
	a.noiseCancel = cancel

	cmd := exec.CommandContext(ctx,
		"gnome-terminal", "--full-screen", "--",
		"bash", "-c", "while true; do head -c 2000 /dev/urandom | base64; sleep 0.05; done",
	)
	cmd.Env = append(os.Environ(), "DISPLAY=:0")
	if err := cmd.Start(); err != nil {
		log.Printf("ERROR: failed to start visual noise: %v", err)
		cancel()
		a.noiseCancel = nil
		return
	}

	go func() {
		_ = cmd.Wait()
	}()

	log.Println("Visual noise started")
}

// stopVisualNoise stops the visual noise terminal.
func (a *Agent) stopVisualNoise() {
	a.noiseMu.Lock()
	defer a.noiseMu.Unlock()
	a.stopVisualNoiseLocked()
}

func (a *Agent) stopVisualNoiseLocked() {
	if a.noiseCancel != nil {
		a.noiseCancel()
		a.noiseCancel = nil
	}
	// Kill any leftover urandom processes
	_ = exec.Command("pkill", "-f", "head -c 2000 /dev/urandom").Run()
	// Close the gnome-terminal window
	_ = exec.Command("wmctrl", "-c", ":ACTIVE:").Run()
	log.Println("Visual noise stopped")
}
