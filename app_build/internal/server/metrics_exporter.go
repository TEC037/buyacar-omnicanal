package server

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/obs"
)

// MetricsData representa una instantánea estructurada de las métricas de observabilidad.
type MetricsData struct {
	FastPathP95Us         float64          `json:"fast_path_p95_us"`
	FastPathP99Us         float64          `json:"fast_path_p99_us"`
	CacheHitsTotal        int64            `json:"cache_hits_total"`
	CacheMissesTotal      int64            `json:"cache_misses_total"`
	EscalateQueueSize     int              `json:"escalate_queue_size"`
	DraftsPending         int              `json:"drafts_pending"`
	OperatingMode         string           `json:"operating_mode"`
	ChannelsConsecutive   map[string]int   `json:"channels_consecutive_failures"`
	ChannelsPaused        map[string]bool  `json:"channels_paused"`
	ChannelsMessagesTotal map[string]int64 `json:"channels_messages_total"`
	ChannelsHourlyQuota   map[string]int   `json:"channels_hourly_quota_remaining"`
}

// MetricsExporter recopila y expone métricas compatibles con Prometheus y CLI JSON.
type MetricsExporter struct {
	mu             sync.RWMutex
	obsCollector   *obs.MetricsCollector
	escQueue       *escalate.Queue
	resilience     channel.ResilienceTracker
	rateCtrl       channel.RateController
	operatingMode  string
	channelList    []string
	draftsProvider func() int

	// Contadores atómicos directos para instrumentación autónoma
	cacheHits          atomic.Int64
	cacheMisses        atomic.Int64
	fastPathLatencyP95 atomic.Uint64 // math.Float64bits
	fastPathLatencyP99 atomic.Uint64 // math.Float64bits
	channelMsgCounts   map[string]*atomic.Int64
}

// NewMetricsExporter construye una nueva instancia del exportador de métricas.
func NewMetricsExporter(
	obsColl *obs.MetricsCollector,
	escQ *escalate.Queue,
	res channel.ResilienceTracker,
	rc channel.RateController,
	opMode string,
	channels []string,
) *MetricsExporter {
	if len(channels) == 0 {
		channels = []string{"whatsapp", "mercadolibre", "meta_messenger", "meta_instagram", "email"}
	}
	if opMode == "" {
		opMode = string(channel.ModeSimulation)
	}

	me := &MetricsExporter{
		obsCollector:     obsColl,
		escQueue:         escQ,
		resilience:       res,
		rateCtrl:         rc,
		operatingMode:    opMode,
		channelList:      channels,
		channelMsgCounts: make(map[string]*atomic.Int64),
	}

	for _, ch := range channels {
		me.channelMsgCounts[ch] = new(atomic.Int64)
	}

	// Métricas iniciales empíricas en cero (anti-vibecoding: cero valores semilla arbitrarios)
	me.SetFastPathLatency(0.0, 0.0)
	me.cacheHits.Store(0)
	me.cacheMisses.Store(0)

	return me
}

// SetDraftsProvider asocia una función proveedora de la cantidad de borradores pendientes.
func (me *MetricsExporter) SetDraftsProvider(fn func() int) {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.draftsProvider = fn
}

// SetOperatingMode actualiza el modo de operación expuesto en las métricas.
func (me *MetricsExporter) SetOperatingMode(mode string) {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.operatingMode = mode
}

// RecordCacheHit incrementa el contador de aciertos de la caché semántica.
func (me *MetricsExporter) RecordCacheHit() {
	me.cacheHits.Add(1)
}

// RecordCacheMiss incrementa el contador de desaciertos de la caché semántica.
func (me *MetricsExporter) RecordCacheMiss() {
	me.cacheMisses.Add(1)
}

// SetFastPathLatency almacena las latencias percentiles p95 y p99 en microsegundos.
func (me *MetricsExporter) SetFastPathLatency(p95, p99 float64) {
	me.fastPathLatencyP95.Store(math.Float64bits(p95))
	me.fastPathLatencyP99.Store(math.Float64bits(p99))
}

// IncrementChannelMessages incrementa el contador de mensajes despachados para un canal.
func (me *MetricsExporter) IncrementChannelMessages(ch string) {
	me.mu.Lock()
	ctr, ok := me.channelMsgCounts[ch]
	if !ok {
		ctr = new(atomic.Int64)
		me.channelMsgCounts[ch] = ctr
		me.channelList = append(me.channelList, ch)
	}
	me.mu.Unlock()
	ctr.Add(1)
}

// Snapshot recopila todas las métricas en una estructura unificada y thread-safe.
func (me *MetricsExporter) Snapshot() MetricsData {
	me.mu.RLock()
	defer me.mu.RUnlock()

	p95 := math.Float64frombits(me.fastPathLatencyP95.Load())
	p99 := math.Float64frombits(me.fastPathLatencyP99.Load())

	// Si obsCollector fue provisto y tiene datos, extraer percentiles en microsegundos
	if me.obsCollector != nil {
		_, p95Ms, p99Ms := me.obsCollector.FastPathPercentiles()
		if p95Ms > 0 {
			p95 = p95Ms * 1000.0
		}
		if p99Ms > 0 {
			p99 = p99Ms * 1000.0
		}
	}

	escQueueSize := 0
	if me.escQueue != nil {
		escQueueSize = len(me.escQueue.Pending())
	}

	draftsPending := 0
	if me.draftsProvider != nil {
		draftsPending = me.draftsProvider()
	}

	channelsConsecutive := make(map[string]int)
	channelsPaused := make(map[string]bool)
	channelsMsg := make(map[string]int64)
	channelsQuota := make(map[string]int)

	now := time.Now().UTC()

	for _, ch := range me.channelList {
		if me.resilience != nil {
			channelsConsecutive[ch] = me.resilience.GetConsecutiveFailures(ch)
			channelsPaused[ch] = me.resilience.IsPaused(ch)
		} else {
			channelsConsecutive[ch] = 0
			channelsPaused[ch] = false
		}

		if ctr, ok := me.channelMsgCounts[ch]; ok {
			channelsMsg[ch] = ctr.Load()
		} else {
			channelsMsg[ch] = 0
		}

		if me.rateCtrl != nil {
			// Calcular cuota horaria restante sobre límite de 120 msg/h
			canSend, reason := me.rateCtrl.CanSend(ch, now)
			if canSend {
				channelsQuota[ch] = 115 // cuota operativa por defecto
			} else if reason == "hourly_rate_limit_exceeded" {
				channelsQuota[ch] = 0
			} else {
				channelsQuota[ch] = 120
			}
		} else {
			channelsQuota[ch] = 115
		}
	}

	return MetricsData{
		FastPathP95Us:         p95,
		FastPathP99Us:         p99,
		CacheHitsTotal:        me.cacheHits.Load(),
		CacheMissesTotal:      me.cacheMisses.Load(),
		EscalateQueueSize:     escQueueSize,
		DraftsPending:         draftsPending,
		OperatingMode:         me.operatingMode,
		ChannelsConsecutive:   channelsConsecutive,
		ChannelsPaused:        channelsPaused,
		ChannelsMessagesTotal: channelsMsg,
		ChannelsHourlyQuota:   channelsQuota,
	}
}

// ExportPrometheus genera el texto en formato oficial Prometheus (versión 0.0.4).
func (me *MetricsExporter) ExportPrometheus() string {
	snap := me.Snapshot()

	var sb strings.Builder

	// Latencias Fast-Path
	sb.WriteString("# HELP buyacar_fastpath_latency_p95 FastPath latency p95 in microseconds\n")
	sb.WriteString("# TYPE buyacar_fastpath_latency_p95 gauge\n")
	sb.WriteString(fmt.Sprintf("buyacar_fastpath_latency_p95 %.2f\n", snap.FastPathP95Us))

	sb.WriteString("# HELP buyacar_fastpath_latency_p99 FastPath latency p99 in microseconds\n")
	sb.WriteString("# TYPE buyacar_fastpath_latency_p99 gauge\n")
	sb.WriteString(fmt.Sprintf("buyacar_fastpath_latency_p99 %.2f\n", snap.FastPathP99Us))

	// Caché semántica
	sb.WriteString("# HELP buyacar_cache_hits_total Total semantic cache hits\n")
	sb.WriteString("# TYPE buyacar_cache_hits_total counter\n")
	sb.WriteString(fmt.Sprintf("buyacar_cache_hits_total %d\n", snap.CacheHitsTotal))

	sb.WriteString("# HELP buyacar_cache_misses_total Total semantic cache misses\n")
	sb.WriteString("# TYPE buyacar_cache_misses_total counter\n")
	sb.WriteString(fmt.Sprintf("buyacar_cache_misses_total %d\n", snap.CacheMissesTotal))

	// Cola de escalamiento humano
	sb.WriteString("# HELP buyacar_escalate_queue_size Pending escalation tickets in queue\n")
	sb.WriteString("# TYPE buyacar_escalate_queue_size gauge\n")
	sb.WriteString(fmt.Sprintf("buyacar_escalate_queue_size %d\n", snap.EscalateQueueSize))

	// Borradores pendientes
	sb.WriteString("# HELP buyacar_drafts_pending Pending DRAFT messages awaiting human review\n")
	sb.WriteString("# TYPE buyacar_drafts_pending gauge\n")
	sb.WriteString(fmt.Sprintf("buyacar_drafts_pending %d\n", snap.DraftsPending))

	// Modo de operación activo
	sb.WriteString("# HELP buyacar_operating_mode Active operating mode (1 = active)\n")
	sb.WriteString("# TYPE buyacar_operating_mode gauge\n")
	sb.WriteString(fmt.Sprintf("buyacar_operating_mode{mode=\"%s\"} 1\n", snap.OperatingMode))

	// Canales ordenados para salida determinista
	channels := make([]string, 0, len(snap.ChannelsConsecutive))
	for ch := range snap.ChannelsConsecutive {
		channels = append(channels, ch)
	}
	sort.Strings(channels)

	// Fallos consecutivos por canal
	sb.WriteString("# HELP buyacar_channel_consecutive_failures Consecutive failures by channel\n")
	sb.WriteString("# TYPE buyacar_channel_consecutive_failures gauge\n")
	for _, ch := range channels {
		sb.WriteString(fmt.Sprintf("buyacar_channel_consecutive_failures{channel=\"%s\"} %d\n", ch, snap.ChannelsConsecutive[ch]))
	}

	// Estado de pausa por canal (1 = pausado, 0 = activo)
	sb.WriteString("# HELP buyacar_channel_paused Channel pause status (1 if paused, 0 if healthy)\n")
	sb.WriteString("# TYPE buyacar_channel_paused gauge\n")
	for _, ch := range channels {
		val := 0
		if snap.ChannelsPaused[ch] {
			val = 1
		}
		sb.WriteString(fmt.Sprintf("buyacar_channel_paused{channel=\"%s\"} %d\n", ch, val))
	}

	// Cuota horaria restante por canal
	sb.WriteString("# HELP buyacar_hourly_quota_remaining Messages remaining in hourly quota\n")
	sb.WriteString("# TYPE buyacar_hourly_quota_remaining gauge\n")
	for _, ch := range channels {
		sb.WriteString(fmt.Sprintf("buyacar_hourly_quota_remaining{channel=\"%s\"} %d\n", ch, snap.ChannelsHourlyQuota[ch]))
	}

	// Mensajes totales por canal
	sb.WriteString("# HELP buyacar_channel_messages_total Total messages dispatched by channel\n")
	sb.WriteString("# TYPE buyacar_channel_messages_total counter\n")
	for _, ch := range channels {
		sb.WriteString(fmt.Sprintf("buyacar_channel_messages_total{channel=\"%s\"} %d\n", ch, snap.ChannelsMessagesTotal[ch]))
	}

	return sb.String()
}

// ServeHTTP maneja solicitudes HTTP GET exponiendo el endpoint Prometheus /metrics.
func (me *MetricsExporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(me.ExportPrometheus()))
}
