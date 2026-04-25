package crypto

import (
	"math/rand"
	"sync"
	"time"
)

// ObfuscatorConfig holds configuration for traffic obfuscation
type ObfuscatorConfig struct {
	Enabled          bool `mapstructure:"enabled" json:"enabled"`
	MinDummyInterval int  `mapstructure:"min_dummy_interval_ms" json:"min_dummy_interval_ms"` // ms
	MaxDummyInterval int  `mapstructure:"max_dummy_interval_ms" json:"max_dummy_interval_ms"` // ms
	JitterPercent    int  `mapstructure:"timing_jitter_percent" json:"timing_jitter_percent"`
	MinFrameSize     int  `mapstructure:"min_frame_size" json:"min_frame_size"`
}

// DefaultObfuscatorConfig returns default configuration
func DefaultObfuscatorConfig() ObfuscatorConfig {
	return ObfuscatorConfig{
		Enabled:          true,
		MinDummyInterval: 5000,
		MaxDummyInterval: 15000,
		JitterPercent:    15,
		MinFrameSize:     128,
	}
}

// SendFunc is the callback used to send encrypted frames
type SendFunc func([]byte) error

// Obfuscator manages dummy traffic injection and timing jitter
type Obfuscator struct {
	config    ObfuscatorConfig
	session   *Session
	sendFn    SendFunc
	stopCh    chan struct{}
	wg        sync.WaitGroup
	rng       *rand.Rand
	mu        sync.Mutex
	closeOnce sync.Once
}

// NewObfuscator creates a new obfuscator
func NewObfuscator(config ObfuscatorConfig, session *Session, sendFn SendFunc) *Obfuscator {
	return &Obfuscator{
		config:  config,
		session: session,
		sendFn:  sendFn,
		stopCh:  make(chan struct{}),
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Start begins the dummy traffic injection loop
func (o *Obfuscator) Start() {
	if !o.config.Enabled {
		return
	}
	o.wg.Add(1)
	go o.dummyLoop()
}

// Stop stops the obfuscator (idempotent)
func (o *Obfuscator) Stop() {
	o.closeOnce.Do(func() {
		close(o.stopCh)
	})
	o.wg.Wait()
}

// JitterDuration applies a random jitter to a base duration
func (o *Obfuscator) JitterDuration(base time.Duration) time.Duration {
	if o.config.JitterPercent <= 0 {
		return base
	}
	o.mu.Lock()
	jitterFactor := 1.0 + (o.rng.Float64()*2-1)*(float64(o.config.JitterPercent)/100.0)
	o.mu.Unlock()
	result := time.Duration(float64(base) * jitterFactor)
	if result < 0 {
		result = base / 2
	}
	return result
}

// RandomInterval returns a random interval between min and max
func (o *Obfuscator) RandomInterval() time.Duration {
	o.mu.Lock()
	minMs := o.config.MinDummyInterval
	maxMs := o.config.MaxDummyInterval
	interval := minMs + o.rng.Intn(maxMs-minMs+1)
	o.mu.Unlock()
	return time.Duration(interval) * time.Millisecond
}

func (o *Obfuscator) dummyLoop() {
	defer o.wg.Done()

	for {
		interval := o.RandomInterval()
		select {
		case <-o.stopCh:
			return
		case <-time.After(interval):
			o.sendDummy()
		}
	}
}

func (o *Obfuscator) sendDummy() {
	if o.session == nil || o.sendFn == nil {
		return
	}
	frame, err := o.session.EncryptDummy()
	if err != nil {
		return
	}
	_ = o.sendFn(frame)
}
