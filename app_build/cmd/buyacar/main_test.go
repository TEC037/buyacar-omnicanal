package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunCLI_Metrics(t *testing.T) {
	ctx := context.Background()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunCLI(ctx, []string{"--metrics"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got: %d, stderr: %s", code, stderr.String())
	}

	var data map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &data); err != nil {
		t.Fatalf("stdout is not valid JSON: %v, raw: %s", err, stdout.String())
	}

	// Verify required keys
	if _, ok := data["fast_path_p99_us"]; !ok {
		t.Errorf("missing fast_path_p99_us in metrics JSON")
	}
	if _, ok := data["cache_hits_total"]; !ok {
		t.Errorf("missing cache_hits_total in metrics JSON")
	}
	if _, ok := data["operating_mode"]; !ok {
		t.Errorf("missing operating_mode in metrics JSON")
	}
	if _, ok := data["pending_escalations"]; !ok {
		t.Errorf("missing pending_escalations in metrics JSON")
	}
}

func TestRunCLI_Inspect(t *testing.T) {
	ctx := context.Background()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunCLI(ctx, []string{"--inspect"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got: %d, stderr: %s", code, stderr.String())
	}

	var data map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &data); err != nil {
		t.Fatalf("stdout is not valid JSON: %v, raw: %s", err, stdout.String())
	}

	if data["status"] != "operational" {
		t.Errorf("expected status 'operational', got: %v", data["status"])
	}
	if _, ok := data["channels"]; !ok {
		t.Errorf("missing channels in inspect JSON")
	}
	if _, ok := data["escalate_queue"]; !ok {
		t.Errorf("missing escalate_queue in inspect JSON")
	}
	if _, ok := data["active_mode"]; !ok {
		t.Errorf("missing active_mode in inspect JSON")
	}
}

func TestRunCLI_ServerFlag(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	port := 18942 // Puerto alto dedicado para pruebas locales
	doneChan := make(chan int, 1)

	go func() {
		code := RunCLI(ctx, []string{"--server", "--port", "18942"}, &stdout, &stderr)
		doneChan <- code
	}()

	// Esperar arranque del servidor
	time.Sleep(100 * time.Millisecond)

	// Verificar respuesta de / y /metrics
	resp, err := http.Get("http://127.0.0.1:18942/")
	if err != nil {
		cancel()
		t.Fatalf("failed to query running server on port %d: %v", port, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from server /, got: %d", resp.StatusCode)
	}

	respM, errM := http.Get("http://127.0.0.1:18942/metrics")
	if errM != nil {
		cancel()
		t.Fatalf("failed to query running server /metrics: %v", errM)
	}
	defer respM.Body.Close()
	bodyM, _ := io.ReadAll(respM.Body)
	if !strings.Contains(string(bodyM), "buyacar_fastpath_latency_p99") {
		t.Fatalf("expected /metrics to contain buyacar_fastpath_latency_p99")
	}

	// Detener el servidor cancelando el contexto
	cancel()

	select {
	case code := <-doneChan:
		if code != 0 {
			t.Fatalf("expected server exit code 0, got: %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for server to exit gracefully")
	}
}

func TestRunCLI_InvalidFlag(t *testing.T) {
	ctx := context.Background()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunCLI(ctx, []string{"--unknown-flag-xyz"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2 on invalid flag, got: %d", code)
	}
}

func TestRunCLI_DefaultBatchExecution(t *testing.T) {
	ctx := context.Background()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := RunCLI(ctx, []string{}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 on default run, got: %d, stderr: %s", code, stderr.String())
	}
}

func TestRunCLI_MarketplaceCopilotEndpoint_M2(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	doneChan := make(chan int, 1)
	go func() {
		doneChan <- RunCLI(ctx, []string{"--server", "--port", "18943"}, &stdout, &stderr)
	}()

	time.Sleep(150 * time.Millisecond)

	// 1. Consulta Fast-Path de Marketplace (Hilux 2022) con PII del cliente que debe redactarse
	reqBody := `{"channel_id":"facebook_marketplace","customer_alias":"Juan_3209876543","message":"Hola, mi cédula es 1049654321, ¿sigue disponible la Toyota Hilux 2022 y qué precio tiene?"}`
	resp, err := http.Post("http://127.0.0.1:18943/api/copilot/draft", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/copilot/draft failed: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", resp.StatusCode, string(raw))
	}

	var copilotOut map[string]interface{}
	if err := json.Unmarshal(raw, &copilotOut); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	reply, _ := copilotOut["proposed_reply"].(string)
	if !strings.Contains(reply, "185000000") || !strings.Contains(reply, "Hilux") {
		t.Fatalf("expected Hilux 185000000 in proposed_reply, got: %s", reply)
	}
	if strings.Contains(string(raw), "1049654321") || strings.Contains(string(raw), "3209876543") {
		t.Fatalf("PII leaked in copilot response: %s", string(raw))
	}

	// 2. Consulta normativa RAG (Garantía motor y caja) -> síntesis autónoma respaldada por RAG
	reqRAG := `{"channel_id":"facebook_marketplace","customer_alias":"Comprador_Boyaca","message":"¿Qué garantía tienen los vehículos usados en motor y caja?"}`
	respRAG, err := http.Post("http://127.0.0.1:18943/api/copilot/draft", "application/json", strings.NewReader(reqRAG))
	if err != nil {
		t.Fatalf("POST /api/copilot/draft RAG failed: %v", err)
	}
	rawRAG, _ := io.ReadAll(respRAG.Body)
	respRAG.Body.Close()
	if !strings.Contains(string(rawRAG), "20000") && !strings.Contains(string(rawRAG), "Garantía") {
		t.Fatalf("expected RAG-backed warranty reply, got: %s", string(rawRAG))
	}

	// 3. Consulta de regateo en Marketplace -> escalamiento obligatorio con respuesta de handoff comercial
	reqEsc := `{"channel_id":"facebook_marketplace","customer_alias":"Comprador_Regateo","message":"¿Cuánto es lo menos por la Toyota Hilux 2022?"}`
	respEsc, err := http.Post("http://127.0.0.1:18943/api/copilot/draft", "application/json", strings.NewReader(reqEsc))
	if err != nil {
		t.Fatalf("POST /api/copilot/draft escalate failed: %v", err)
	}
	rawEsc, _ := io.ReadAll(respEsc.Body)
	respEsc.Body.Close()
	if !strings.Contains(string(rawEsc), `"escalated":true`) || !strings.Contains(string(rawEsc), "315 121 2223") {
		t.Fatalf("expected discount query to escalate with commercial handoff, got: %s", string(rawEsc))
	}

	// 4. Consulta de modelo con múltiples años (Mazda CX-5) -> desambiguación inmediata con precios de ambos años
	reqDisamb := `{"channel_id":"facebook_marketplace","customer_alias":"Comprador_CX5","message":"¿Tienen Mazda CX-5?"}`
	respDisamb, err := http.Post("http://127.0.0.1:18943/api/copilot/draft", "application/json", strings.NewReader(reqDisamb))
	if err != nil {
		t.Fatalf("POST /api/copilot/draft disambiguate failed: %v", err)
	}
	rawDisamb, _ := io.ReadAll(respDisamb.Body)
	respDisamb.Body.Close()
	if !strings.Contains(string(rawDisamb), "118000000") || !strings.Contains(string(rawDisamb), "142000000") {
		t.Fatalf("expected CX-5 disambiguation with both 2021 and 2023 prices, got: %s", string(rawDisamb))
	}

	cancel()
	select {
	case <-doneChan:
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not shut down in time")
	}
}

func TestRunCLI_Phase4_MetaWebhookAndKillSwitch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	doneChan := make(chan int, 1)
	go func() {
		doneChan <- RunCLI(ctx, []string{"--server", "--port", "18944"}, &stdout, &stderr)
	}()

	time.Sleep(150 * time.Millisecond)

	// 1. Verificar webhook de Meta con referral.ad_title de Facebook Marketplace y firma HMAC-SHA256
	nowMs := time.Now().UnixMilli()
	payload := `{"object":"page","entry":[{"id":"buyacar-page-sim","time":` +
		strings.TrimSpace(string( mustJSON(nowMs) )) +
		`,"messaging":[{"sender":{"id":"buyer-boyaca-99"},"recipient":{"id":"buyacar-page-sim"},"timestamp":` +
		strings.TrimSpace(string( mustJSON(nowMs) )) +
		`,"referral":{"ad_title":"Toyota Hilux 2022"},"message":{"mid":"mid.mkt.hilux.01","text":"¿Sigue disponible?"}}]}]}`
	mac := hmac.New(sha256.New, []byte("buyacar-secret-sim"))
	_, _ = mac.Write([]byte(payload))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:18944/webhooks/meta", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("failed to build webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /webhooks/meta failed: %v", err)
	}
	rawW, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /webhooks/meta, got %d: %s", resp.StatusCode, string(rawW))
	}
	if !strings.Contains(string(rawW), `"status":"received"`) {
		t.Fatalf("expected status:received, got: %s", string(rawW))
	}

	// Verificar en /metrics que el evento del webhook con ad_title heredado disparó el Fast-Path autónomamente
	respM, errM := http.Get("http://127.0.0.1:18944/metrics")
	if errM != nil {
		t.Fatalf("GET /metrics failed: %v", errM)
	}
	rawM, _ := io.ReadAll(respM.Body)
	respM.Body.Close()
	if !strings.Contains(string(rawM), `buyacar_channel_messages_total{channel="messenger"} 1`) {
		t.Fatalf("expected webhook to trigger autonomous Fast-Path processing (buyacar_channel_messages_total{channel=\"messenger\"} 1), got:\n%s", string(rawM))
	}

	// 2. Verificar endpoint de Kill Switch en vivo (/api/killswitch)
	respKS, err := http.Post("http://127.0.0.1:18944/api/killswitch", "application/json", strings.NewReader(`{"active":true}`))
	if err != nil {
		t.Fatalf("POST /api/killswitch failed: %v", err)
	}
	rawKS, _ := io.ReadAll(respKS.Body)
	respKS.Body.Close()
	if respKS.StatusCode != http.StatusOK || !strings.Contains(string(rawKS), `"kill_switch_active":true`) {
		t.Fatalf("expected kill_switch_active:true, got %d: %s", respKS.StatusCode, string(rawKS))
	}

	cancel()
	select {
	case <-doneChan:
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not shut down in time")
	}
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestRunCLI_AutopilotSandbox_EndToEndWebhookToMetaSendAPI(t *testing.T) {
	// 1. Levantar servidor local sandbox que emula Meta Graph API (POST /me/messages)
	var mu sync.Mutex
	var sentRecipient string
	var sentText string
	var authHeader string

	mockMetaGraph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/me/messages") {
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Recipient struct {
					ID string `json:"id"`
				} `json:"recipient"`
				Message struct {
					Text       string `json:"text"`
					Attachment struct {
						Type    string `json:"type"`
						Payload struct {
							TemplateType string `json:"template_type"`
							Text         string `json:"text"`
						} `json:"payload"`
					} `json:"attachment"`
				} `json:"message"`
			}
			_ = json.Unmarshal(body, &payload)
			txt := payload.Message.Text
			if txt == "" && payload.Message.Attachment.Payload.Text != "" {
				txt = payload.Message.Attachment.Payload.Text
			}
			mu.Lock()
			sentRecipient = payload.Recipient.ID
			sentText = txt
			authHeader = r.Header.Get("Authorization")
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"recipient_id":"` + payload.Recipient.ID + `","message_id":"m_sandbox_01"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockMetaGraph.Close()

	// 2. Activar AUTOPILOT con doble candado en entorno aislado de prueba
	t.Setenv("BUYACAR_OPERATING_MODE", "AUTOPILOT")
	t.Setenv("BUYACAR_AUTOPILOT_CONFIRMED", "1")
	t.Setenv("META_GRAPH_BASE_URL", mockMetaGraph.URL)
	t.Setenv("META_ACCESS_TOKEN", "sandbox-page-token-xyz")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	doneChan := make(chan int, 1)
	go func() {
		doneChan <- RunCLI(ctx, []string{"--server", "--port", "18945"}, &stdout, &stderr)
	}()

	time.Sleep(150 * time.Millisecond)

	// 3. Enviar webhook firmado de un comprador de Marketplace preguntando "¿Sigue disponible?" sobre el anuncio real "Nissan Kicks 2020"
	nowMs := time.Now().UnixMilli()
	payload := `{"object":"page","entry":[{"id":"buyacar-page-sim","time":` +
		strings.TrimSpace(string(mustJSON(nowMs))) +
		`,"messaging":[{"sender":{"id":"buyer-kicks-2020"},"recipient":{"id":"buyacar-page-sim"},"timestamp":` +
		strings.TrimSpace(string(mustJSON(nowMs))) +
		`,"referral":{"ad_title":"Nissan Kicks 2020"},"message":{"mid":"mid.autopilot.kicks.01","text":"¿Sigue disponible?"}}]}]}`
	mac := hmac.New(sha256.New, []byte("buyacar-secret-sim"))
	_, _ = mac.Write([]byte(payload))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:18945/webhooks/meta", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /webhooks/meta failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /webhooks/meta, got %d", resp.StatusCode)
	}

	// 4. Verificar que AUTOPILOT despachó automáticamente la respuesta verificada a Meta Send API (/me/messages)
	mu.Lock()
	gotRecipient := sentRecipient
	gotText := sentText
	gotAuth := authHeader
	mu.Unlock()

	if gotRecipient != "buyer-kicks-2020" {
		t.Fatalf("expected AUTOPILOT to send reply to recipient buyer-kicks-2020, got %q", gotRecipient)
	}
	if !strings.Contains(gotText, "72000000") || !strings.Contains(gotText, "Kicks") {
		t.Fatalf("expected AUTOPILOT reply to contain real Nissan Kicks 2020 price 72000000, got: %s", gotText)
	}
	if gotAuth != "Bearer sandbox-page-token-xyz" {
		t.Fatalf("expected Authorization Bearer sandbox-page-token-xyz, got %q", gotAuth)
	}

	cancel()
	select {
	case <-doneChan:
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not shut down in time")
	}
}

func TestRunCLI_WebConsole_OneClickAutopilotAndSignedWebhookSimulation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	doneChan := make(chan int, 1)
	go func() {
		doneChan <- RunCLI(ctx, []string{"--server", "--port", "18946"}, &stdout, &stderr)
	}()

	time.Sleep(150 * time.Millisecond)

	// 1. Cambiar a AUTOPILOT en caliente desde la consola web (1 clic) sin variables de entorno
	modeResp, err := http.Post("http://127.0.0.1:18946/api/mode", "application/json", strings.NewReader(`{"mode":"AUTOPILOT"}`))
	if err != nil {
		t.Fatalf("POST /api/mode failed: %v", err)
	}
	modeResp.Body.Close()
	if modeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/mode, got %d", modeResp.StatusCode)
	}

	// 2. Disparar simulación de Webhook firmado con 1 clic desde la consola web
	whResp, err := http.Post("http://127.0.0.1:18946/api/webhook/simulate", "application/json",
		strings.NewReader(`{"message":"¿Sigue disponible?","ad_title":"Toyota Fortuner 2024"}`))
	if err != nil {
		t.Fatalf("POST /api/webhook/simulate failed: %v", err)
	}
	if whResp.StatusCode != http.StatusOK {
		whResp.Body.Close()
		t.Fatalf("expected 200 OK from /api/webhook/simulate, got %d", whResp.StatusCode)
	}

	var result map[string]any
	decodeErr := json.NewDecoder(whResp.Body).Decode(&result)
	whResp.Body.Close()
	if decodeErr != nil {
		t.Fatalf("failed to decode /api/webhook/simulate response: %v", decodeErr)
	}
	if result["operating_mode"] != "AUTOPILOT" {
		t.Fatalf("expected operating_mode AUTOPILOT, got %v", result["operating_mode"])
	}
	if result["send_status"] != "sent_verified" {
		t.Fatalf("expected send_status sent_verified, got %v", result["send_status"])
	}
	reply, _ := result["dispatched_reply"].(string)
	if !strings.Contains(reply, "233000000") || !strings.Contains(reply, "Fortuner") {
		t.Fatalf("expected AUTOPILOT reply for Toyota Fortuner 2024 (233000000), got %q", reply)
	}

	cancel()
	select {
	case <-doneChan:
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not shut down in time")
	}
}

func TestRunCLI_Phase5_N8NBridgeAndHotReloadGeminiAPIKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	doneChan := make(chan int, 1)
	go func() {
		doneChan <- RunCLI(ctx, []string{"--server", "--port", "18947"}, &stdout, &stderr)
	}()

	time.Sleep(150 * time.Millisecond)

	// 1. Configurar GEMINI_API_KEY en caliente desde la consola web (POST /api/gemini/config)
	gemResp, err := http.Post("http://127.0.0.1:18947/api/gemini/config", "application/json",
		strings.NewReader(`{"api_key":"AIzaSyBuyaCARTunjaKey123456"}`))
	if err != nil {
		t.Fatalf("POST /api/gemini/config failed: %v", err)
	}
	var gemCfg map[string]any
	_ = json.NewDecoder(gemResp.Body).Decode(&gemCfg)
	gemResp.Body.Close()
	if gemCfg["configured"] != true || gemCfg["strategy"] != "A4_adaptive" {
		t.Fatalf("expected configured=true and strategy=A4_adaptive, got %v", gemCfg)
	}

	// 2. Probar el puente oficial de n8n (POST /api/n8n/webhook) con consulta sobre Nissan Kicks 2020
	n8nResp, err := http.Post("http://127.0.0.1:18947/api/n8n/webhook", "application/json",
		strings.NewReader(`{"channel_id":"n8n_whatsapp","customer_alias":"cliente-n8n-01","ad_title":"Nissan Kicks 2020","message":"¿Sigue disponible y qué garantía dan?"}`))
	if err != nil {
		t.Fatalf("POST /api/n8n/webhook failed: %v", err)
	}
	var n8nOut map[string]any
	_ = json.NewDecoder(n8nResp.Body).Decode(&n8nOut)
	n8nResp.Body.Close()
	if n8nOut["should_send"] != true {
		t.Fatalf("expected should_send=true from /api/n8n/webhook, got %v", n8nOut)
	}
	replyText, _ := n8nOut["reply_text"].(string)
	if !strings.Contains(replyText, "72000000") || !strings.Contains(replyText, "12 meses") {
		t.Fatalf("expected compound reply with price 72000000 and 12 meses warranty, got %q", replyText)
	}

	// 3. Probar el puente de n8n con regateo -> debe devolver should_send=false y escalated=true
	n8nEsc, err := http.Post("http://127.0.0.1:18947/api/n8n/webhook", "application/json",
		strings.NewReader(`{"channel_id":"n8n_whatsapp","customer_alias":"cliente-n8n-02","ad_title":"Nissan Kicks 2020","message":"¿Cuánto es lo menos?"}`))
	if err != nil {
		t.Fatalf("POST /api/n8n/webhook escalate failed: %v", err)
	}
	var n8nEscOut map[string]any
	_ = json.NewDecoder(n8nEsc.Body).Decode(&n8nEscOut)
	n8nEsc.Body.Close()
	if n8nEscOut["should_send"] != false || n8nEscOut["escalated"] != true {
		t.Fatalf("expected should_send=false and escalated=true on discount query, got %v", n8nEscOut)
	}

	cancel()
	select {
	case <-doneChan:
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not shut down in time")
	}
}


