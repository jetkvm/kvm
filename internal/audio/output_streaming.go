package audio

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// OutputStreamer manages high-performance audio output streaming
type OutputStreamer struct {
	// Atomic fields must be first for proper alignment on ARM
	processedFrames int64 // Total processed frames counter (atomic)
	droppedFrames   int64 // Dropped frames counter (atomic)
	processingTime  int64 // Average processing time in nanoseconds (atomic)
	lastStatsTime   int64 // Last statistics update time (atomic)

	client     *AudioClient
	bufferPool *AudioBufferPool
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	running    bool
	mtx        sync.Mutex

	// Performance optimization fields
	batchSize      int           // Adaptive batch size for frame processing
	processingChan chan []byte   // Buffered channel for frame processing
	statsInterval  time.Duration // Statistics reporting interval
}

var (
	outputStreamingRunning int32
	outputStreamingCancel  context.CancelFunc
	outputStreamingLogger  *zerolog.Logger
)

func getOutputStreamingLogger() *zerolog.Logger {
	if outputStreamingLogger == nil {
		logger := logging.GetDefaultLogger().With().Str("component", "audio-output").Logger()
		outputStreamingLogger = &logger
	}
	return outputStreamingLogger
}

func NewOutputStreamer() (*OutputStreamer, error) {
	client := NewAudioClient()

	// Get initial batch size from adaptive buffer manager
	adaptiveManager := GetAdaptiveBufferManager()
	initialBatchSize := adaptiveManager.GetOutputBufferSize()

	ctx, cancel := context.WithCancel(context.Background())
	return &OutputStreamer{
		client:         client,
		bufferPool:     NewAudioBufferPool(GetMaxAudioFrameSize()), // Use existing buffer pool
		ctx:            ctx,
		cancel:         cancel,
		batchSize:      initialBatchSize,                                 // Use adaptive batch size
		processingChan: make(chan []byte, GetConfig().ChannelBufferSize), // Large buffer for smooth processing
		statsInterval:  5 * time.Second,                                  // Statistics every 5 seconds
		lastStatsTime:  time.Now().UnixNano(),
	}, nil
}

func (s *OutputStreamer) Start() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.running {
		return fmt.Errorf("output streamer already running")
	}

	// Connect to audio output server
	if err := s.client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to audio output server: %w", err)
	}

	s.running = true

	// Start multiple goroutines for optimal performance
	s.wg.Add(3)
	go s.streamLoop()     // Main streaming loop
	go s.processingLoop() // Frame processing loop
	go s.statisticsLoop() // Performance monitoring loop

	return nil
}

func (s *OutputStreamer) Stop() {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if !s.running {
		return
	}

	s.running = false
	s.cancel()

	// Close processing channel to signal goroutines
	close(s.processingChan)

	// Wait for all goroutines to finish
	s.wg.Wait()

	if s.client != nil {
		s.client.Close()
	}
}

func (s *OutputStreamer) streamLoop() {
	defer s.wg.Done()

	// Pin goroutine to OS thread for consistent performance
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Adaptive timing for frame reading
	frameInterval := time.Duration(GetConfig().OutputStreamingFrameIntervalMS) * time.Millisecond // 50 FPS base rate
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	// Batch size update ticker
	batchUpdateTicker := time.NewTicker(GetConfig().BufferUpdateInterval)
	defer batchUpdateTicker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-batchUpdateTicker.C:
			// Update batch size from adaptive buffer manager
			s.UpdateBatchSize()
		case <-ticker.C:
			// Read audio data from CGO with timing measurement
			startTime := time.Now()
			frameBuf := s.bufferPool.Get()
			n, err := CGOAudioReadEncode(frameBuf)
			processingDuration := time.Since(startTime)

			if err != nil {
				getOutputStreamingLogger().Warn().Err(err).Msg("Failed to read audio data")
				s.bufferPool.Put(frameBuf)
				atomic.AddInt64(&s.droppedFrames, 1)
				continue
			}

			if n > 0 {
				// Send frame for processing (non-blocking)
				frameData := make([]byte, n)
				copy(frameData, frameBuf[:n])

				select {
				case s.processingChan <- frameData:
					atomic.AddInt64(&s.processedFrames, 1)
					// Update processing time statistics
					atomic.StoreInt64(&s.processingTime, int64(processingDuration))
					// Report latency to adaptive buffer manager
					s.ReportLatency(processingDuration)
				default:
					// Processing channel full, drop frame
					atomic.AddInt64(&s.droppedFrames, 1)
				}
			}

			s.bufferPool.Put(frameBuf)
		}
	}
}

// processingLoop handles frame processing in a separate goroutine
func (s *OutputStreamer) processingLoop() {
	defer s.wg.Done()

	// Pin goroutine to OS thread for consistent performance
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Set high priority for audio output processing
	if err := SetAudioThreadPriority(); err != nil {
		getOutputStreamingLogger().Warn().Err(err).Msg("Failed to set audio output processing priority")
	}
	defer func() {
		if err := ResetThreadPriority(); err != nil {
			getOutputStreamingLogger().Warn().Err(err).Msg("Failed to reset thread priority")
		}
	}()

	for range s.processingChan {
		// Process frame (currently just receiving, but can be extended)
		if _, err := s.client.ReceiveFrame(); err != nil {
			if s.client.IsConnected() {
				getOutputStreamingLogger().Warn().Err(err).Msg("Failed to receive frame")
				atomic.AddInt64(&s.droppedFrames, 1)
			}
			// Try to reconnect if disconnected
			if !s.client.IsConnected() {
				if err := s.client.Connect(); err != nil {
					getOutputStreamingLogger().Warn().Err(err).Msg("Failed to reconnect")
				}
			}
		}
	}
}

// statisticsLoop monitors and reports performance statistics
func (s *OutputStreamer) statisticsLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.statsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.reportStatistics()
		}
	}
}

// reportStatistics logs current performance statistics
func (s *OutputStreamer) reportStatistics() {
	processed := atomic.LoadInt64(&s.processedFrames)
	dropped := atomic.LoadInt64(&s.droppedFrames)
	processingTime := atomic.LoadInt64(&s.processingTime)

	if processed > 0 {
		dropRate := float64(dropped) / float64(processed+dropped) * GetConfig().PercentageMultiplier
		avgProcessingTime := time.Duration(processingTime)

		getOutputStreamingLogger().Info().Int64("processed", processed).Int64("dropped", dropped).Float64("drop_rate", dropRate).Dur("avg_processing", avgProcessingTime).Msg("Output Audio Stats")

		// Get client statistics
		clientTotal, clientDropped := s.client.GetClientStats()
		getOutputStreamingLogger().Info().Int64("total", clientTotal).Int64("dropped", clientDropped).Msg("Client Stats")
	}
}

// GetStats returns streaming statistics
func (s *OutputStreamer) GetStats() (processed, dropped int64, avgProcessingTime time.Duration) {
	processed = atomic.LoadInt64(&s.processedFrames)
	dropped = atomic.LoadInt64(&s.droppedFrames)
	processingTimeNs := atomic.LoadInt64(&s.processingTime)
	avgProcessingTime = time.Duration(processingTimeNs)
	return
}

// GetDetailedStats returns comprehensive streaming statistics
func (s *OutputStreamer) GetDetailedStats() map[string]interface{} {
	processed := atomic.LoadInt64(&s.processedFrames)
	dropped := atomic.LoadInt64(&s.droppedFrames)
	processingTime := atomic.LoadInt64(&s.processingTime)

	stats := map[string]interface{}{
		"processed_frames":       processed,
		"dropped_frames":         dropped,
		"avg_processing_time_ns": processingTime,
		"batch_size":             s.batchSize,
		"channel_buffer_size":    cap(s.processingChan),
		"channel_current_size":   len(s.processingChan),
		"connected":              s.client.IsConnected(),
	}

	if processed+dropped > 0 {
		stats["drop_rate_percent"] = float64(dropped) / float64(processed+dropped) * GetConfig().PercentageMultiplier
	}

	// Add client statistics
	clientTotal, clientDropped := s.client.GetClientStats()
	stats["client_total_frames"] = clientTotal
	stats["client_dropped_frames"] = clientDropped

	return stats
}

// UpdateBatchSize updates the batch size from adaptive buffer manager
func (s *OutputStreamer) UpdateBatchSize() {
	s.mtx.Lock()
	adaptiveManager := GetAdaptiveBufferManager()
	s.batchSize = adaptiveManager.GetOutputBufferSize()
	s.mtx.Unlock()
}

// ReportLatency reports processing latency to adaptive buffer manager
func (s *OutputStreamer) ReportLatency(latency time.Duration) {
	adaptiveManager := GetAdaptiveBufferManager()
	adaptiveManager.UpdateLatency(latency)
}

// StartAudioOutputStreaming starts audio output streaming (capturing system audio)
func StartAudioOutputStreaming(send func([]byte)) error {
	if !atomic.CompareAndSwapInt32(&outputStreamingRunning, 0, 1) {
		return ErrAudioAlreadyRunning
	}

	// Initialize CGO audio capture
	if err := CGOAudioInit(); err != nil {
		atomic.StoreInt32(&outputStreamingRunning, 0)
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	outputStreamingCancel = cancel

	// Start audio capture loop
	go func() {
		defer func() {
			CGOAudioClose()
			atomic.StoreInt32(&outputStreamingRunning, 0)
			getOutputStreamingLogger().Info().Msg("Audio output streaming stopped")
		}()

		getOutputStreamingLogger().Info().Msg("Audio output streaming started")
		buffer := make([]byte, GetMaxAudioFrameSize())

		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Capture audio frame
				n, err := CGOAudioReadEncode(buffer)
				if err != nil {
					getOutputStreamingLogger().Warn().Err(err).Msg("Failed to read/encode audio")
					continue
				}
				if n > 0 {
					// Get frame buffer from pool to reduce allocations
					frame := GetAudioFrameBuffer()
					frame = frame[:n] // Resize to actual frame size
					copy(frame, buffer[:n])
					send(frame)
					// Return buffer to pool after sending
					PutAudioFrameBuffer(frame)
					RecordFrameReceived(n)
				}
				// Small delay to prevent busy waiting
				time.Sleep(GetConfig().ShortSleepDuration)
			}
		}
	}()

	return nil
}

// StopAudioOutputStreaming stops audio output streaming
func StopAudioOutputStreaming() {
	if atomic.LoadInt32(&outputStreamingRunning) == 0 {
		return
	}

	if outputStreamingCancel != nil {
		outputStreamingCancel()
		outputStreamingCancel = nil
	}

	// Wait for streaming to stop
	for atomic.LoadInt32(&outputStreamingRunning) == 1 {
		time.Sleep(GetConfig().ShortSleepDuration)
	}
}
