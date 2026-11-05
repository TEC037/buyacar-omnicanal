package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/channel"
	"buyacar.com.co/omnicanal/internal/escalate"
)

func TestDashboardHandler_ServeIndex(t *testing.T) {
	dh := NewDashboardHandler(nil, nil, nil, nil, "SIMULATION", nil)

	// GET /
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	dh.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)
	if !strings.Contains(body, "BuyaCAR Supervisor Console") {
		t.Fatalf("expected body to contain 'BuyaCAR Supervisor Console', got: %s", body)
	}

	// POST / should be 405 Method Not Allowed
	reqPost := httptest.NewRequest(http.MethodPost, "/", nil)
	wPost := httptest.NewRecorder()
	dh.ServeHTTP(wPost, reqPost)
	if wPost.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed on POST /, got: %d", wPost.Result().StatusCode)
	}
}

func TestDashboardHandler_1ClickDraftApprove(t *testing.T) {
	dh := NewDashboardHandler(nil, nil, nil, nil, "DRAFT", nil)

	hookCalled := false
	var hookedID string
	dh.SetOnDraftApprove(func(draftID string) error {
		hookCalled = true
		hookedID = draftID
		return nil
	})

	// 1. Register draft
	draftID := "draft-test-001"
	dh.RegisterDraft(DraftItem{
		DraftID:        draftID,
		ConversationID: "conv-01",
		CustomerAlias:  "Lead_Tunja",
		Subject:        "Consulta Toyota",
		DraftBody:      "Vehiculo disponible por $120M COP",
		Status:         "pending",
		CreatedAt:      time.Now().UTC(),
	})

	if dh.PendingDraftsCount() != 1 {
		t.Fatalf("expected 1 pending draft, got: %d", dh.PendingDraftsCount())
	}

	// 2. Approve via Query Param (?id=...)
	req := httptest.NewRequest(http.MethodPost, "/api/draft/approve?id="+draftID, nil)
	w := httptest.NewRecorder()
	dh.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
	}

	var resData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&resData); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if resData["status"] != "approved" || resData["draft_id"] != draftID {
		t.Fatalf("unexpected approve response: %+v", resData)
	}

	status, ok := dh.GetDraftStatus(draftID)
	if !ok || status != "approved" {
		t.Fatalf("expected draft status approved, got: %s (ok=%v)", status, ok)
	}

	if !hookCalled || hookedID != draftID {
		t.Fatalf("expected hook to be called with %s, got: %v (%s)", draftID, hookCalled, hookedID)
	}

	// 3. Approve via JSON Body
	draftID2 := "draft-test-002"
	dh.RegisterDraft(DraftItem{DraftID: draftID2, Status: "pending"})

	jsonBody := []byte(`{"draft_id":"` + draftID2 + `"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/draft/approve", bytes.NewReader(jsonBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	dh.ServeHTTP(w2, req2)

	if w2.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for JSON body, got: %d", w2.Result().StatusCode)
	}
	status2, _ := dh.GetDraftStatus(draftID2)
	if status2 != "approved" {
		t.Fatalf("expected draft2 status approved, got: %s", status2)
	}

	// 4. Missing ID -> 400 Bad Request
	reqBad := httptest.NewRequest(http.MethodPost, "/api/draft/approve", nil)
	wBad := httptest.NewRecorder()
	dh.ServeHTTP(wBad, reqBad)
	if wBad.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on missing ID, got: %d", wBad.Result().StatusCode)
	}

	// 5. GET method -> 405 Method Not Allowed
	reqGet := httptest.NewRequest(http.MethodGet, "/api/draft/approve?id="+draftID, nil)
	wGet := httptest.NewRecorder()
	dh.ServeHTTP(wGet, reqGet)
	if wGet.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed on GET, got: %d", wGet.Result().StatusCode)
	}
}

func TestDashboardHandler_ChannelUnpause(t *testing.T) {
	escQ := escalate.NewQueue()
	res := channel.NewMemoryResilienceTracker(escQ)
	res.RecordFailure("whatsapp", "err1")
	res.RecordFailure("whatsapp", "err2")
	res.RecordFailure("whatsapp", "err3")

	if !res.IsPaused("whatsapp") {
		t.Fatalf("expected whatsapp to be paused")
	}

	dh := NewDashboardHandler(escQ, res, nil, nil, "SIMULATION", []string{"whatsapp"})

	// 1. Unpause via query param
	req := httptest.NewRequest(http.MethodPost, "/api/channel/unpause?channel=whatsapp", nil)
	w := httptest.NewRecorder()
	dh.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d", resp.StatusCode)
	}

	var resData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&resData); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resData["channel"] != "whatsapp" || resData["status"] != "active" {
		t.Fatalf("unexpected response: %+v", resData)
	}

	if res.IsPaused("whatsapp") {
		t.Fatalf("expected whatsapp to be unpaused after API call")
	}

	// 2. Unpause via JSON Body
	res.RecordFailure("mercadolibre", "err1")
	res.RecordFailure("mercadolibre", "err2")
	res.RecordFailure("mercadolibre", "err3")
	if !res.IsPaused("mercadolibre") {
		t.Fatalf("expected mercadolibre to be paused")
	}

	jsonBody := []byte(`{"channel":"mercadolibre"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/channel/unpause", bytes.NewReader(jsonBody))
	w2 := httptest.NewRecorder()
	dh.ServeHTTP(w2, req2)

	if w2.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for unpause JSON, got: %d", w2.Result().StatusCode)
	}
	if res.IsPaused("mercadolibre") {
		t.Fatalf("expected mercadolibre to be unpaused")
	}

	// 3. Missing channel -> 400
	reqBad := httptest.NewRequest(http.MethodPost, "/api/channel/unpause", nil)
	wBad := httptest.NewRecorder()
	dh.ServeHTTP(wBad, reqBad)
	if wBad.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing channel, got: %d", wBad.Result().StatusCode)
	}

	// 4. GET method -> 405
	reqGet := httptest.NewRequest(http.MethodGet, "/api/channel/unpause?channel=whatsapp", nil)
	wGet := httptest.NewRecorder()
	dh.ServeHTTP(wGet, reqGet)
	if wGet.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 on GET, got: %d", wGet.Result().StatusCode)
	}
}

func TestDashboardHandler_StatusAndAPIs(t *testing.T) {
	escQ := escalate.NewQueue()
	escQ.Enqueue("email", "conv-99", "human_review", "consulta especial", time.Now().UTC())

	dh := NewDashboardHandler(escQ, nil, nil, nil, "DRAFT", []string{"whatsapp", "email"})
	dh.RegisterDraft(DraftItem{DraftID: "d-1", Status: "pending"})

	// /api/status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	wStatus := httptest.NewRecorder()
	dh.ServeHTTP(wStatus, reqStatus)

	if wStatus.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /api/status, got: %d", wStatus.Result().StatusCode)
	}
	var statusData map[string]interface{}
	if err := json.NewDecoder(wStatus.Result().Body).Decode(&statusData); err != nil {
		t.Fatalf("failed to decode /api/status JSON: %v", err)
	}
	if statusData["status"] != "operational" || statusData["operating_mode"] != "DRAFT" {
		t.Fatalf("unexpected status data: %+v", statusData)
	}

	// /api/escalations
	reqEsc := httptest.NewRequest(http.MethodGet, "/api/escalations", nil)
	wEsc := httptest.NewRecorder()
	dh.ServeHTTP(wEsc, reqEsc)
	if wEsc.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /api/escalations, got: %d", wEsc.Result().StatusCode)
	}

	// /api/drafts
	reqDrafts := httptest.NewRequest(http.MethodGet, "/api/drafts", nil)
	wDrafts := httptest.NewRecorder()
	dh.ServeHTTP(wDrafts, reqDrafts)
	if wDrafts.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /api/drafts, got: %d", wDrafts.Result().StatusCode)
	}

	// 404 for unknown path
	req404 := httptest.NewRequest(http.MethodGet, "/api/unknown_route_xyz", nil)
	w404 := httptest.NewRecorder()
	dh.ServeHTTP(w404, req404)
	if w404.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on unknown route, got: %d", w404.Result().StatusCode)
	}
}
