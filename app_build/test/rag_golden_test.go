package test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/ai"
	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/core"
	"buyacar.com.co/omnicanal/internal/crm"
	"buyacar.com.co/omnicanal/internal/escalate"
	"buyacar.com.co/omnicanal/internal/fast"
	"buyacar.com.co/omnicanal/internal/guard"
	"buyacar.com.co/omnicanal/internal/inventory"
	"buyacar.com.co/omnicanal/internal/rag"
	"buyacar.com.co/omnicanal/internal/skills"
	"buyacar.com.co/omnicanal/internal/store"
)

// goldenMockAIProvider simula respuestas estructuradas de IA para pruebas doradas E2E.
type goldenMockAIProvider struct {
	mu           sync.Mutex
	replies      map[string][]byte
	defaultReply []byte
	lastCompact  skills.CompactContext
}

func newGoldenMockAIProvider(defaultReply []byte) *goldenMockAIProvider {
	return &goldenMockAIProvider{
		replies:      make(map[string][]byte),
		defaultReply: defaultReply,
	}
}

func (p *goldenMockAIProvider) SetReplyForIntent(intent string, replyJSON []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.replies[intent] = replyJSON
}

func (p *goldenMockAIProvider) Name() string { return "ollama" }

func (p *goldenMockAIProvider) GenerateJSON(ctx context.Context, compact skills.CompactContext) ([]byte, int, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastCompact = compact
	if rep, ok := p.replies[string(compact.Intent)]; ok {
		return rep, compact.EstimatedTokens, 60, nil
	}
	return p.defaultReply, compact.EstimatedTokens, 60, nil
}

func makeAIResponseJSON(reply, intent string, escalate bool, reason string) []byte {
	var rPtr *string
	if reason != "" {
		rPtr = &reason
	}
	out := ai.StructuredOutput{
		ConfigID: "cfg-golden-m4",
		Trace: ai.TraceInfo{
			TimestampUTC: time.Now().UTC().Format(time.RFC3339),
			RequestID:    "req-golden-01",
		},
		ProposedReply:       reply,
		Intent:              intent,
		Confidence:          0.95,
		ConversationSummary: "Consulta golden test",
		CRMUpdate: ai.CRMUpdate{
			TasksToCreate: []string{},
		},
		Governance: ai.Governance{
			Escalate: escalate,
			Reason:   rPtr,
		},
	}
	b, _ := json.Marshal(out)
	return b
}

func goldenVehicles() []inventory.Vehicle {
	return []inventory.Vehicle{
		{
			ID:            "veh-duster-2021",
			Brand:         "Renault",
			Model:         "Duster",
			Year:          2021,
			PriceCOP:      72000000,
			MileageKm:     45000,
			Transmission:  "Mecánica",
			Engine:        "1.6L 16V 114 HP",
			Traction:      "4x2",
			Color:         "Gris Estrella",
			BodyType:      "SUV",
			PlateLastChar: "5",
			SOATValid:     true,
			TecnoValid:    true,
			Status:        "available",
			OfficialURL:   "https://buyacar.com.co/vehiculos/renault-duster-zen-2021",
		},
		{
			ID:            "veh-001",
			Brand:         "Toyota",
			Model:         "Hilux",
			Year:          2022,
			PriceCOP:      185000000,
			MileageKm:     42000,
			Transmission:  "Automática",
			Engine:        "2.8L Turbo Diesel",
			Traction:      "4x4",
			Color:         "Blanco Perlado",
			BodyType:      "Pick-up",
			PlateLastChar: "4",
			SOATValid:     true,
			TecnoValid:    true,
			Status:        "available",
			OfficialURL:   "https://buyacar.com.co/vehiculos/toyota-hilux-2022",
		},
		{
			ID:            "veh-stepway-2020",
			Brand:         "Renault",
			Model:         "Stepway",
			Year:          2020,
			PriceCOP:      54000000,
			MileageKm:     52000,
			Transmission:  "Mecánica",
			Engine:        "1.6L 16V 111 HP",
			Traction:      "4x2",
			Color:         "Naranja Ocre",
			BodyType:      "Hatchback",
			PlateLastChar: "3",
			SOATValid:     true,
			TecnoValid:    true,
			Status:        "available",
			OfficialURL:   "https://buyacar.com.co/vehiculos/renault-sandero-stepway-2020",
		},
	}
}

func setupGoldenPipeline(t testing.TB, aiProv ai.Provider, ragEng rag.Engine, now time.Time) (*core.Pipeline, *channel.StubChannel, channel.Session, *crm.MemoryManager, *escalate.Queue) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	eng := core.NewEngine(core.DefaultSafeConfig(), st)

	waStub := channel.NewStubChannel("whatsapp", 650)
	_ = eng.RegisterChannel(waStub)

	sess, err := waStub.Open(ctx, channel.Link{ID: "link-wa-01", URL: "https://buyacar.com.co/wa", Active: true})
	if err != nil {
		t.Fatalf("failed to open stub channel session: %v", err)
	}

	crmMgr := crm.NewMemoryManager()
	escQ := escalate.NewQueue()
	aiOrch := ai.NewOrchestrator(nil, aiProv, skills.NewApprovedCache())

	invCache := inventory.NewCache(15, 30)
	invCache.LoadSnapshot(goldenVehicles(), `W/"buyacar-golden-etag"`, now)

	pipe := core.NewPipeline(core.PipelineConfig{
		Engine:    eng,
		InvCache:  invCache,
		Fast:      fast.NewEngine(fast.DefaultBusinessTexts(), 0.80),
		Guard:     guard.NewGuard([]string{"buyacar.com.co"}),
		AIOrch:    aiOrch,
		EscQueue:  escQ,
		CRM:       crmMgr,
		RAGEngine: ragEng,
	})

	return pipe, waStub, sess, crmMgr, escQ
}

func loadIndexedGoldenRAG(t testing.TB) *rag.InMemoryEngine {
	ctx := context.Background()
	engine := rag.NewEngine()
	chunks, err := rag.LoadEmbeddedFixtures(ctx, engine)
	if err != nil {
		t.Fatalf("failed to load embedded RAG fixtures: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatalf("expected non-empty RAG chunks from embedded fixtures")
	}
	return engine
}

// 1. Vehicle catalog query (specs, km, prices in Boyacá).
func TestRAGGolden_VehicleCatalog(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	ragEng := loadIndexedGoldenRAG(t)

	// Verificación de recuperación léxico-vectorial RAG
	scored, err := ragEng.Retrieve(ctx, rag.Query{
		Text:      "Renault Duster 2021 camioneta SUV precio kilometraje Tunja Boyacá",
		Category:  "catalog",
		TopK:      5,
		MaxTokens: 600,
	})
	if err != nil {
		t.Fatalf("retrieval failed: %v", err)
	}
	if len(scored) == 0 {
		t.Fatalf("expected at least 1 retrieved vehicle chunk")
	}

	foundDuster := false
	for _, sc := range scored {
		if strings.Contains(sc.Chunk.Content, "Duster") || strings.Contains(sc.Chunk.Title, "Duster") {
			foundDuster = true
			if !strings.Contains(sc.Chunk.Content, "72000000") {
				t.Errorf("expected Duster price 72000000 in chunk content: %s", sc.Chunk.Content)
			}
			if !strings.Contains(sc.Chunk.Content, "45000") {
				t.Errorf("expected Duster mileage 45000 in chunk content: %s", sc.Chunk.Content)
			}
		}
	}
	if !foundDuster {
		t.Fatalf("expected to find Renault Duster in top chunks")
	}

	// Ejecución en Pipeline E2E
	validReply := "Contamos con la Renault Duster Zen 2021 en Tunja: motor 1.6L, 45000 km, precio de $72000000 COP. WhatsApp 3151212223. Ver más en https://buyacar.com.co/vehiculos/renault-duster-zen-2021"
	aiProv := newGoldenMockAIProvider(makeAIResponseJSON(validReply, "price", false, ""))
	pipe, waStub, sess, _, _ := setupGoldenPipeline(t, aiProv, ragEng, now)

	conv := channel.Conversation{ID: "conv-golden-cat-01", ChannelID: "whatsapp", CustomerAlias: "Comprador_Duster", UpdatedAt: now}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Qué precio y kilometraje tiene la Renault Duster 2021?", Timestamp: now.Add(-1 * time.Minute)},
	})

	outcome, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("pipeline failure: %v", err)
	}
	if outcome.Escalated {
		t.Fatalf("expected non-escalated outcome, got reason: %s", outcome.EscalationReason)
	}
	if outcome.SendStatus != channel.SendSimulated {
		t.Fatalf("expected SendSimulated, got %s", outcome.SendStatus)
	}
	if !strings.Contains(outcome.SentReply, "72000000") || !strings.Contains(outcome.SentReply, "45000") {
		t.Fatalf("expected reply with verified price and mileage, got: %s", outcome.SentReply)
	}
}

// 2. Warranty policy query (12m/20k km, coverage, workshops in Tunja/Duitama).
func TestRAGGolden_WarrantyPolicy(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	ragEng := loadIndexedGoldenRAG(t)

	// Verificación de recuperación
	scored, err := ragEng.Retrieve(ctx, rag.Query{
		Text:      "cuánto tiempo de garantía legal tienen los usados y qué cubre en talleres de Tunja y Duitama",
		Category:  "warranty",
		TopK:      5,
		MaxTokens: 800,
	})
	if err != nil {
		t.Fatalf("retrieval failed: %v", err)
	}
	if len(scored) == 0 {
		t.Fatalf("expected at least 1 warranty chunk")
	}

	// Comprobar contenido y cifras respaldadas en los fragmentos
	contentCombined := ""
	for _, sc := range scored {
		contentCombined += sc.Chunk.Content + " "
	}
	if !strings.Contains(contentCombined, "20000") && !strings.Contains(contentCombined, "20.000") {
		t.Fatalf("expected warranty chunk to specify 20000 km, got: %s", contentCombined)
	}
	if !strings.Contains(contentCombined, "Tunja") || !strings.Contains(contentCombined, "Duitama") {
		t.Fatalf("expected workshops in Tunja and Duitama in warranty docs")
	}

	// Ejecución en Pipeline E2E con cifra de 5 dígitos (20000 km) respaldada por RAG
	validReply := "Nuestra garantía mecánica certificada cubre motor y caja por 12 meses o 20000 km en talleres Tunja (Av. Universitaria # 45-12) y Duitama. Citas al 3160502717. https://buyacar.com.co/garantia"
	aiProv := newGoldenMockAIProvider(makeAIResponseJSON(validReply, "complex_query", false, ""))
	pipe, waStub, sess, _, _ := setupGoldenPipeline(t, aiProv, ragEng, now)

	conv := channel.Conversation{ID: "conv-golden-warr-01", ChannelID: "whatsapp", CustomerAlias: "Cliente_Garantia", UpdatedAt: now}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Cuál es la política de garantía de BuyaCAR y qué talleres atienden en Boyacá?", Timestamp: now.Add(-1 * time.Minute)},
	})

	outcome, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("pipeline failure: %v", err)
	}
	if outcome.Escalated {
		t.Fatalf("expected warranty reply with backed 20000 km to be approved, got escalation: %s", outcome.EscalationReason)
	}
	if !strings.Contains(outcome.SentReply, "20000") {
		t.Fatalf("expected 20000 in sent reply, got: %s", outcome.SentReply)
	}
}

// 3. Credit requirements query (employees, independents, 10% down payment, allied banks).
func TestRAGGolden_CreditRequirements(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	ragEng := loadIndexedGoldenRAG(t)

	scored, err := ragEng.Retrieve(ctx, rag.Query{
		Text:      "requisitos para crédito vehicular de empleados e independientes cuota inicial bancos aliados Boyacá",
		Category:  "credit",
		TopK:      5,
		MaxTokens: 800,
	})
	if err != nil {
		t.Fatalf("retrieval failed: %v", err)
	}
	if len(scored) == 0 {
		t.Fatalf("expected credit chunks")
	}

	contentCombined := ""
	for _, sc := range scored {
		contentCombined += sc.Chunk.Content + " "
	}
	if !strings.Contains(contentCombined, "Bancolombia") || !strings.Contains(contentCombined, "Davivienda") {
		t.Fatalf("expected allied banks in credit chunks: %s", contentCombined)
	}
	if !strings.Contains(contentCombined, "cuota inicial") {
		t.Fatalf("expected cuota inicial in credit chunks: %s", contentCombined)
	}

	// Ejecución en Pipeline E2E
	validReply := "Simulación indicativa de cuota inicial según perfil con Bancolombia, Davivienda y Finandina. Empleados: certificación laboral y nóminas. Independientes: RUT y extractos. WhatsApp 3151212223. https://buyacar.com.co/credito"
	aiProv := newGoldenMockAIProvider(makeAIResponseJSON(validReply, "complex_query", false, ""))
	pipe, waStub, sess, _, _ := setupGoldenPipeline(t, aiProv, ragEng, now)

	conv := channel.Conversation{ID: "conv-golden-cred-01", ChannelID: "whatsapp", CustomerAlias: "Cliente_Credito", UpdatedAt: now}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Qué requisitos piden para crédito de vehículo si soy empleado o independiente?", Timestamp: now.Add(-1 * time.Minute)},
	})

	outcome, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("pipeline failure: %v", err)
	}
	if outcome.Escalated {
		t.Fatalf("expected credit reply to be approved, got escalation: %s", outcome.EscalationReason)
	}
	if outcome.SendStatus != channel.SendSimulated {
		t.Fatalf("expected SendSimulated, got %s", outcome.SendStatus)
	}
}

// 4. FAQ Boyacá/Tunja query (hours, address Av. Universitaria, peritaje, retoma).
func TestRAGGolden_FAQBoyacaTunja(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	ragEng := loadIndexedGoldenRAG(t)

	scored, err := ragEng.Retrieve(ctx, rag.Query{
		Text:      "horarios de atención sede Tunja frente a Unicentro peritaje retoma separación",
		Category:  "faq",
		TopK:      5,
		MaxTokens: 800,
	})
	if err != nil {
		t.Fatalf("retrieval failed: %v", err)
	}
	if len(scored) == 0 {
		t.Fatalf("expected FAQ chunks")
	}

	contentCombined := ""
	for _, sc := range scored {
		contentCombined += sc.Chunk.Content + " "
	}
	if !strings.Contains(contentCombined, "Avenida Universitaria") {
		t.Fatalf("expected Av. Universitaria in FAQ chunks: %s", contentCombined)
	}
	if !strings.Contains(contentCombined, "150 puntos") {
		t.Fatalf("expected 150 points peritaje in FAQ chunks: %s", contentCombined)
	}

	// Ejecución en Pipeline E2E con teléfonos de contacto oficiales respaldados (3151212223 / 3022866969)
	validReply := "Atendemos en Tunja de lunes a sábado de 8:00 am a 6:00 pm frente a la Toyota. Peritaje de 150 puntos y avalúo en 45 minutos. Llámanos al 3022866969 o WhatsApp 3151212223. Info https://buyacar.com.co/faq"
	aiProv := newGoldenMockAIProvider(makeAIResponseJSON(validReply, "complex_query", false, ""))
	pipe, waStub, sess, _, _ := setupGoldenPipeline(t, aiProv, ragEng, now)

	conv := channel.Conversation{ID: "conv-golden-faq-01", ChannelID: "whatsapp", CustomerAlias: "Cliente_FAQ", UpdatedAt: now}
	waStub.SeedThread(conv, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Cómo es el proceso de revisión pericial de 150 puntos y cuáles son las líneas de contacto?", Timestamp: now.Add(-1 * time.Minute)},
	})

	outcome, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
	if err != nil {
		t.Fatalf("pipeline failure: %v", err)
	}
	if outcome.Escalated {
		t.Fatalf("expected FAQ reply with backed numbers to be approved, got escalation: %s", outcome.EscalationReason)
	}
	if !strings.Contains(outcome.SentReply, "3022866969") {
		t.Fatalf("expected 3022866969 in sent reply, got: %s", outcome.SentReply)
	}
}

// 5. Anti-hallucination test: backed numbers pass, unbacked 5+ digit numbers are blocked and escalated.
func TestRAGGolden_AntiHallucinationEnforcement(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	ragEng := loadIndexedGoldenRAG(t)

	// Caso A: Cifras respaldadas en RAG (20000 km, 72000000 precio, 3022866969 telefono) pasan
	backedReply := "Garantía de 20000 km, vehículo a 72000000 COP y teléfono 3022866969. WhatsApp 3151212223. https://buyacar.com.co/garantia"
	aiProvBacked := newGoldenMockAIProvider(makeAIResponseJSON(backedReply, "complex_query", false, ""))
	pipeBacked, waStubBacked, sessBacked, _, _ := setupGoldenPipeline(t, aiProvBacked, ragEng, now)

	convA := channel.Conversation{ID: "conv-backed-pass", ChannelID: "whatsapp", CustomerAlias: "Cliente_A", UpdatedAt: now}
	waStubBacked.SeedThread(convA, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Cuáles son las condiciones de compra y garantía en detalle?", Timestamp: now.Add(-1 * time.Minute)},
	})

	outA, err := pipeBacked.ProcessConversation(ctx, waStubBacked, sessBacked, convA, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outA.Escalated {
		t.Fatalf("expected backed numbers to be approved, got escalation: %s", outA.EscalationReason)
	}

	// Caso B: Cifra de 5+ dígitos inventada (88888888) es bloqueada deterministamente en CP5 y escala
	hallucinatedReply := "Te ofrecemos el vehículo con un bono especial en $88888888 COP. WhatsApp 3151212223. https://buyacar.com.co"
	aiProvUnbacked := newGoldenMockAIProvider(makeAIResponseJSON(hallucinatedReply, "complex_query", false, ""))
	pipeUnbacked, waStubUnbacked, sessUnbacked, _, escQueue := setupGoldenPipeline(t, aiProvUnbacked, ragEng, now)

	convB := channel.Conversation{ID: "conv-unbacked-block", ChannelID: "whatsapp", CustomerAlias: "Cliente_B", UpdatedAt: now}
	waStubUnbacked.SeedThread(convB, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Qué políticas de garantía y precio especial aplican para este automotor?", Timestamp: now.Add(-1 * time.Minute)},
	})

	outB, err := pipeUnbacked.ProcessConversation(ctx, waStubUnbacked, sessUnbacked, convB, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !outB.Escalated {
		t.Fatalf("expected hallucinated number 88888888 to be escalated")
	}
	if !strings.HasPrefix(outB.EscalationReason, "hallucinated_number_blocked:88888888") {
		t.Fatalf("expected escalation reason to specify hallucinated number, got: %s", outB.EscalationReason)
	}

	// Verificar que se haya emitido el ticket a escalate.Queue
	tickets := escQueue.Pending()
	if len(tickets) == 0 {
		t.Fatalf("expected ticket in escalate queue")
	}
	if tickets[0].ConversationID != "conv-unbacked-block" {
		t.Fatalf("expected ticket for conv-unbacked-block, got %s", tickets[0].ConversationID)
	}

	// Caso C: URL no autorizada es bloqueada
	badURLReply := "Visita nuestro catálogo en https://sitio-no-oficial.com/carros. WhatsApp 3151212223."
	aiProvBadURL := newGoldenMockAIProvider(makeAIResponseJSON(badURLReply, "complex_query", false, ""))
	pipeBadURL, waBadURL, sessBadURL, _, _ := setupGoldenPipeline(t, aiProvBadURL, ragEng, now)

	convC := channel.Conversation{ID: "conv-bad-url", ChannelID: "whatsapp", CustomerAlias: "Cliente_C", UpdatedAt: now}
	waBadURL.SeedThread(convC, []channel.Message{
		{ID: "m1", Sender: "customer", Text: "¿Dónde veo fotos del catálogo en línea?", Timestamp: now.Add(-1 * time.Minute)},
	})
	outC, _ := pipeBadURL.ProcessConversation(ctx, waBadURL, sessBadURL, convC, now)
	if !outC.Escalated || !strings.Contains(outC.EscalationReason, "unauthorized_url_domain") {
		t.Fatalf("expected escalation for unauthorized URL, got: %+v", outC)
	}
}

// 6. PII redaction test: customer cédula/phones redacted under Ley 1581.
func TestRAGGolden_PIIRedactionLey1581(t *testing.T) {
	// 1. Redacción de cédula y teléfono de cliente en texto crudo
	customerMsg := "Buenos días, mi cédula es 1049654321, mi celular personal es 3209876543 y mi correo es juan_perez@gmail.com. Deseo información."
	redacted, count := guard.RedactPII(customerMsg)

	if count < 3 {
		t.Fatalf("expected at least 3 redactions (cédula, phone, email), got %d: %s", count, redacted)
	}
	if strings.Contains(redacted, "1049654321") {
		t.Fatalf("customer cédula leaked: %s", redacted)
	}
	if strings.Contains(redacted, "3209876543") {
		t.Fatalf("customer phone leaked: %s", redacted)
	}
	if strings.Contains(redacted, "juan_perez@gmail.com") {
		t.Fatalf("customer email leaked: %s", redacted)
	}
	if !strings.Contains(redacted, "[PII_NUM_REDACTED]") {
		t.Fatalf("expected [PII_NUM_REDACTED] in sanitized string: %s", redacted)
	}
	if !strings.Contains(redacted, "[PII_EMAIL_REDACTED]") {
		t.Fatalf("expected [PII_EMAIL_REDACTED] in sanitized string: %s", redacted)
	}

	// 2. Preservación estricta de teléfonos oficiales verificados de BuyaCAR
	officialMsg := "Comuníquese a la línea de ventas 3151212223 o al 3022866969 o a garantías 3160502717."
	sanitizedOfficial, officialCount := guard.RedactPII(officialMsg)
	if officialCount != 0 {
		t.Fatalf("expected 0 redactions for official numbers, got %d: %s", officialCount, sanitizedOfficial)
	}
	if !strings.Contains(sanitizedOfficial, "3151212223") || !strings.Contains(sanitizedOfficial, "3022866969") || !strings.Contains(sanitizedOfficial, "3160502717") {
		t.Fatalf("official phone numbers must not be redacted: %s", sanitizedOfficial)
	}

	// 3. Verificación de ticket en escalate.Queue con contexto sanitizado
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	escQ := escalate.NewQueue()
	ticket := escQ.Enqueue("whatsapp", "conv-pii-01", "manual_review", customerMsg, now)

	if strings.Contains(ticket.RedactedContext, "1049654321") || strings.Contains(ticket.RedactedContext, "3209876543") || strings.Contains(ticket.RedactedContext, "juan_perez@gmail.com") {
		t.Fatalf("PII leaked into escalated ticket context: %s", ticket.RedactedContext)
	}
}

// 7. Concurrency test: parallel queries to Pipeline with RAGEngine.
func TestRAGGolden_PipelineConcurrencyWithRAG(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	ragEng := loadIndexedGoldenRAG(t)

	validReply := "En BuyaCAR Tunja ofrecemos vehículos certificados, garantía de 20000 km y crédito con bancos aliados. WhatsApp 3151212223. https://buyacar.com.co"
	aiProv := newGoldenMockAIProvider(makeAIResponseJSON(validReply, "complex_query", false, ""))
	pipe, waStub, sess, crmMgr, _ := setupGoldenPipeline(t, aiProv, ragEng, now)

	const workerCount = 20
	var wg sync.WaitGroup
	errCh := make(chan error, workerCount)

	queries := []string{
		"¿Qué camionetas SUV tienen en Tunja?",
		"¿Cuál es la política de garantía de 20000 km?",
		"¿Cuáles son los requisitos de crédito para independientes?",
		"¿Dónde queda la sede de BuyaCAR en Tunja y qué horarios tienen?",
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cid := fmt.Sprintf("conv-concurrent-%03d", idx)
			qText := queries[idx%len(queries)]

			conv := channel.Conversation{
				ID:            cid,
				ChannelID:     "whatsapp",
				CustomerAlias: fmt.Sprintf("Cliente_%03d", idx),
				UpdatedAt:     now.Add(time.Duration(idx) * time.Second),
			}
			waStub.SeedThread(conv, []channel.Message{
				{ID: fmt.Sprintf("msg-%d", idx), Sender: "customer", Text: qText, Timestamp: now},
			})

			outcome, err := pipe.ProcessConversation(ctx, waStub, sess, conv, now)
			if err != nil {
				errCh <- fmt.Errorf("worker %d failed: %w", idx, err)
				return
			}
			if outcome.Escalated {
				errCh <- fmt.Errorf("worker %d unexpectedly escalated: %s", idx, outcome.EscalationReason)
				return
			}
			if outcome.SendStatus != channel.SendSimulated {
				errCh <- fmt.Errorf("worker %d expected SendSimulated, got %s", idx, outcome.SendStatus)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrency error: %v", err)
	}

	// Verificar que cada worker asentó su registro en CRM
	tasks := crmMgr.ListTasks(ctx)
	if len(tasks) < workerCount {
		t.Fatalf("expected at least %d CRM tasks settled, got %d", workerCount, len(tasks))
	}
}

// 8. M0 Marketplace Course-Correction Golden Test: exact Hilux 2022 match, zero vehicle substitution on missing brand/model, and exact top-1 ranking for Boyacá hours/location and warranty coverage.
func TestRAGGolden_M0_MarketplaceCourseCorrection(t *testing.T) {
	ctx := context.Background()
	ragEng := loadIndexedGoldenRAG(t)

	// 1. Toyota Hilux 2022 query must return Hilux 2022 ($185.000.000) as #1 and never Renault Logan 2022
	hiluxRes, err := ragEng.Retrieve(ctx, rag.Query{
		Text:      "¿Tienen disponible una Toyota Hilux 2022 y qué precio tiene?",
		TopK:      2,
		MaxTokens: 250,
	})
	if err != nil {
		t.Fatalf("Hilux retrieval failed: %v", err)
	}
	if len(hiluxRes) == 0 {
		t.Fatalf("expected Toyota Hilux 2022 chunk, got 0")
	}
	if !strings.Contains(hiluxRes[0].Chunk.Title, "Hilux") || !strings.Contains(hiluxRes[0].Chunk.Content, "185000000") {
		t.Fatalf("expected top-1 chunk to be Toyota Hilux 2022 with 185000000, got title=%q", hiluxRes[0].Chunk.Title)
	}
	for _, r := range hiluxRes {
		if strings.Contains(r.Chunk.Title, "Logan") {
			t.Fatalf("regression: Hilux query returned Renault Logan chunk: %q", r.Chunk.Title)
		}
	}

	// 2. Non-existent vehicle (Ferrari Testarossa 2022) must return 0 chunks (never substitute another 2022 car)
	missingRes, err := ragEng.Retrieve(ctx, rag.Query{
		Text:      "¿Tienen disponible un Ferrari Testarossa 2022?",
		TopK:      2,
		MaxTokens: 250,
	})
	if err != nil {
		t.Fatalf("missing vehicle retrieval failed: %v", err)
	}
	if len(missingRes) != 0 {
		t.Fatalf("expected 0 chunks for non-existent Ferrari Testarossa 2022, got %d (%q)", len(missingRes), missingRes[0].Chunk.Title)
	}

	// 3. Boyacá location and hours query must rank "Horarios de Atención y Ubicación de Sedes" #1
	locRes, err := ragEng.Retrieve(ctx, rag.Query{
		Text:      "¿Dónde quedan ubicados y cuál es el horario de atención en Boyacá?",
		TopK:      2,
		MaxTokens: 250,
	})
	if err != nil || len(locRes) == 0 {
		t.Fatalf("location/hours retrieval failed: err=%v len=%d", err, len(locRes))
	}
	if !strings.Contains(locRes[0].Chunk.Title, "Horarios de Atención y Ubicación") {
		t.Fatalf("expected top-1 chunk to be 'Horarios de Atención y Ubicación de Sedes', got %q", locRes[0].Chunk.Title)
	}

	// 4. Used vehicle motor and gearbox warranty query must rank "Cobertura de Garantía Mecánica Certificada" #1
	warrRes, err := ragEng.Retrieve(ctx, rag.Query{
		Text:      "¿Qué garantía tienen los vehículos usados en motor y caja?",
		TopK:      2,
		MaxTokens: 250,
	})
	if err != nil || len(warrRes) == 0 {
		t.Fatalf("warranty retrieval failed: err=%v len=%d", err, len(warrRes))
	}
	if !strings.Contains(warrRes[0].Chunk.Title, "Cobertura de Garantía Mecánica Certificada") {
		t.Fatalf("expected top-1 chunk to be 'Cobertura de Garantía Mecánica Certificada', got %q", warrRes[0].Chunk.Title)
	}
}
