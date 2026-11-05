package channel_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/channel"
)

func TestRateController_Defaults(t *testing.T) {
	cfg := channel.DefaultRateConfig()
	if cfg.MaxMessagesPerHour != 120 {
		t.Fatalf("expected MaxMessagesPerHour=120, got %d", cfg.MaxMessagesPerHour)
	}
	if cfg.MaxMessagesPerDay != 250 {
		t.Fatalf("expected MaxMessagesPerDay=250, got %d", cfg.MaxMessagesPerDay)
	}
	if cfg.ChatDelayBase != 3200*time.Millisecond {
		t.Fatalf("expected ChatDelayBase=3200ms, got %v", cfg.ChatDelayBase)
	}
	if cfg.ChannelDelayBase != 2500*time.Millisecond {
		t.Fatalf("expected ChannelDelayBase=2500ms, got %v", cfg.ChannelDelayBase)
	}
	if cfg.JitterPercent != 0.30 {
		t.Fatalf("expected JitterPercent=0.30, got %f", cfg.JitterPercent)
	}

	rc := channel.NewMemoryRateController(channel.RateConfig{})
	// Verify that zero/negative values get populated with defaults
	d := rc.CalculateChatDelay(42)
	if d < 2240*time.Millisecond || d > 4160*time.Millisecond {
		t.Fatalf("expected chat delay within default bounds, got %v", d)
	}
}

func TestRateController_HourlyRateLimitExactBoundary_120(t *testing.T) {
	rc := channel.NewMemoryRateController(channel.DefaultRateConfig())
	t0 := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	ch := "whatsapp"

	// 1. Send exactly 120 messages in rolling hour (every 25 seconds = 50 minutes total)
	for i := 1; i <= 120; i++ {
		ts := t0.Add(time.Duration(i) * 25 * time.Second)
		canSend, reason := rc.CanSend(ch, ts)
		if !canSend {
			t.Fatalf("msg %d should be permitted, got reason: %s", i, reason)
		}
		rc.RecordSend(ch, ts)
	}

	// Verify counts
	tCheck := t0.Add(55 * time.Minute)
	hCount, dCount := rc.GetCounts(ch, tCheck)
	if hCount != 120 || dCount != 120 {
		t.Fatalf("expected counts (120, 120), got (%d, %d)", hCount, dCount)
	}

	// 2. Message 121 within same hour must be blocked
	canSend121, reason121 := rc.CanSend(ch, tCheck)
	if canSend121 {
		t.Fatalf("message 121 must be rejected by hourly rate limiter")
	}
	if reason121 != "hourly_rate_limit_exceeded" {
		t.Fatalf("expected reason hourly_rate_limit_exceeded, got: %s", reason121)
	}

	// 3. Roll past the 1-hour window of the first message
	// First msg was at t0 + 25s. At t0 + 1h + 30s, the first message has aged out (>1h)
	tRoll := t0.Add(1*time.Hour + 30*time.Second)
	canSendAfter, _ := rc.CanSend(ch, tRoll)
	if !canSendAfter {
		t.Fatalf("expected canSend=true after 1 hour rolled past oldest message")
	}
}

func TestRateController_DailyRateLimitExactBoundary_250(t *testing.T) {
	rc := channel.NewMemoryRateController(channel.DefaultRateConfig())
	t0 := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	ch := "email_pilot"

	// 1. Send 250 messages across 20 hours (averaging 12.5 msgs/hr, well under 120/hr limit)
	for i := 1; i <= 250; i++ {
		ts := t0.Add(time.Duration(i) * 4 * time.Minute)
		canSend, reason := rc.CanSend(ch, ts)
		if !canSend {
			t.Fatalf("msg %d should be permitted, got reason: %s", i, reason)
		}
		rc.RecordSend(ch, ts)
	}

	// Verify daily count
	tCheck := t0.Add(21 * time.Hour)
	_, dCount := rc.GetCounts(ch, tCheck)
	if dCount != 250 {
		t.Fatalf("expected daily count 250, got %d", dCount)
	}

	// 2. Message 251 within 24h must be blocked by daily limit
	canSend251, reason251 := rc.CanSend(ch, tCheck)
	if canSend251 {
		t.Fatalf("message 251 must be blocked by daily rate limiter")
	}
	if reason251 != "daily_rate_limit_exceeded" {
		t.Fatalf("expected reason daily_rate_limit_exceeded, got: %s", reason251)
	}

	// 3. Roll past 24 hours of first message (first was at t0 + 4min)
	tRoll := t0.Add(24*time.Hour + 5*time.Minute)
	canSendAfter, _ := rc.CanSend(ch, tRoll)
	if !canSendAfter {
		t.Fatalf("expected canSend=true after rolling window passes 24 hours")
	}
}

func TestRateController_ChannelIsolation(t *testing.T) {
	rc := channel.NewMemoryRateController(channel.DefaultRateConfig())
	t0 := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

	// Exhaust quota for "whatsapp"
	for i := 1; i <= 120; i++ {
		ts := t0.Add(time.Duration(i) * 10 * time.Second)
		rc.RecordSend("whatsapp", ts)
	}

	tCheck := t0.Add(25 * time.Minute)
	// whatsapp is blocked
	canSendWA, _ := rc.CanSend("whatsapp", tCheck)
	if canSendWA {
		t.Fatalf("whatsapp should be rate limited")
	}

	// "email" and "mercadolibre" must be completely unaffected
	canSendEmail, _ := rc.CanSend("email", tCheck)
	if !canSendEmail {
		t.Fatalf("email should NOT be affected by whatsapp quota exhaustion")
	}

	canSendML, _ := rc.CanSend("mercadolibre", tCheck)
	if !canSendML {
		t.Fatalf("mercadolibre should NOT be affected by whatsapp quota exhaustion")
	}
}

func TestRateController_DeterministicJitterBounds(t *testing.T) {
	rc := channel.NewMemoryRateController(channel.DefaultRateConfig())

	chatMin := 2240 * time.Millisecond // 3200 * 0.70
	chatMax := 4160 * time.Millisecond // 3200 * 1.30

	channelMin := 1750 * time.Millisecond // 2500 * 0.70
	channelMax := 3250 * time.Millisecond // 2500 * 1.30

	// Test across 10,000 distinct seeds
	for seed := int64(1); seed <= 10000; seed++ {
		chatDelay := rc.CalculateChatDelay(seed)
		if chatDelay < chatMin || chatDelay > chatMax {
			t.Fatalf("seed %d: chat delay %v outside [%v, %v]", seed, chatDelay, chatMin, chatMax)
		}

		chanDelay := rc.CalculateChannelDelay(seed)
		if chanDelay < channelMin || chanDelay > channelMax {
			t.Fatalf("seed %d: channel delay %v outside [%v, %v]", seed, chanDelay, channelMin, channelMax)
		}
	}
}

func TestRateController_DeterministicJitterReproducibility(t *testing.T) {
	rc := channel.NewMemoryRateController(channel.DefaultRateConfig())

	seeds := []int64{0, 1, 42, 1337, 99999999, -12345}
	for _, seed := range seeds {
		c1 := rc.CalculateChatDelay(seed)
		ch1 := rc.CalculateChannelDelay(seed)

		for repeat := 0; repeat < 50; repeat++ {
			if rc.CalculateChatDelay(seed) != c1 {
				t.Fatalf("non-deterministic chat delay for seed %d", seed)
			}
			if rc.CalculateChannelDelay(seed) != ch1 {
				t.Fatalf("non-deterministic channel delay for seed %d", seed)
			}
		}
	}
}

func TestRateController_WaitWithContext(t *testing.T) {
	rc := channel.NewMemoryRateController(channel.DefaultRateConfig())

	// 1. Normal completion for small delay
	ctx := context.Background()
	start := time.Now()
	err := rc.WaitWithContext(ctx, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("expected nil error on normal wait, got %v", err)
	}
	if time.Since(start) < 9*time.Millisecond {
		t.Fatalf("wait returned too quickly: %v", time.Since(start))
	}

	// 2. Context cancellation during wait
	ctxCancel, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()
	startCancel := time.Now()
	errCancel := rc.WaitWithContext(ctxCancel, 2*time.Second)
	if errCancel != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", errCancel)
	}
	if time.Since(startCancel) > 200*time.Millisecond {
		t.Fatalf("cancellation took too long: %v", time.Since(startCancel))
	}

	// 3. Pre-canceled context
	ctxPre, cancelPre := context.WithCancel(context.Background())
	cancelPre()
	if err := rc.WaitWithContext(ctxPre, 5*time.Second); err != context.Canceled {
		t.Fatalf("expected immediate context.Canceled, got %v", err)
	}

	// 4. Zero or negative delay
	if err := rc.WaitWithContext(context.Background(), 0); err != nil {
		t.Fatalf("expected nil error on 0 delay, got %v", err)
	}
	if err := rc.WaitWithContext(context.Background(), -10*time.Millisecond); err != nil {
		t.Fatalf("expected nil error on negative delay, got %v", err)
	}
}

func TestRateController_ConcurrentAccess(t *testing.T) {
	rc := channel.NewMemoryRateController(channel.DefaultRateConfig())
	channels := []string{"whatsapp", "email", "mercadolibre", "web"}
	now := time.Now()

	var wg sync.WaitGroup
	const goroutines = 40
	const opsPerGoroutine = 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		ch := channels[g%len(channels)]
		seed := int64(g * 1000)

		go func(channelID string, baseSeed int64) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				ts := now.Add(time.Duration(i) * time.Second)
				canSend, _ := rc.CanSend(channelID, ts)
				if canSend {
					rc.RecordSend(channelID, ts)
				}
				_ = rc.CalculateChatDelay(baseSeed + int64(i))
				_ = rc.CalculateChannelDelay(baseSeed + int64(i))
				_, _ = rc.GetCounts(channelID, ts)
			}
		}(ch, seed)
	}

	wg.Wait()
}

func TestRateController_Reset(t *testing.T) {
	rc := channel.NewMemoryRateController(channel.DefaultRateConfig())
	now := time.Now()

	rc.RecordSend("ch1", now)
	rc.RecordSend("ch2", now)

	h1, _ := rc.GetCounts("ch1", now)
	if h1 != 1 {
		t.Fatalf("expected 1 record for ch1, got %d", h1)
	}

	// Reset only ch1
	rc.Reset("ch1")
	h1After, _ := rc.GetCounts("ch1", now)
	if h1After != 0 {
		t.Fatalf("expected 0 for ch1 after reset, got %d", h1After)
	}
	h2After, _ := rc.GetCounts("ch2", now)
	if h2After != 1 {
		t.Fatalf("ch2 should remain intact after ch1 reset, got %d", h2After)
	}

	// Reset all
	rc.Reset("")
	h2Final, _ := rc.GetCounts("ch2", now)
	if h2Final != 0 {
		t.Fatalf("expected 0 for ch2 after full reset, got %d", h2Final)
	}
}

func TestRateController_InvalidInputs(t *testing.T) {
	rc := channel.NewMemoryRateController(channel.DefaultRateConfig())

	canSend, reason := rc.CanSend("", time.Now())
	if canSend || reason != "invalid_channel_id" {
		t.Fatalf("expected false and invalid_channel_id for empty channel, got (%v, %s)", canSend, reason)
	}

	// Empty channel RecordSend is a no-op
	rc.RecordSend("", time.Now())

	// Zero time uses current time
	canSendZero, _ := rc.CanSend("whatsapp", time.Time{})
	if !canSendZero {
		t.Fatalf("expected canSend=true with zero time on fresh controller")
	}
}
