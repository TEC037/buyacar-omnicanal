package crm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"buyacar.com.co/omnicanal/internal/triage"
)

func TestMemoryManager_RecordAndGetByConv(t *testing.T) {
	ctx := context.Background()
	mgr := NewMemoryManager()
	now := time.Now().UTC()

	convID := "conv-test-1"
	task1 := Task{
		ID:          "task-1",
		ConvID:      convID,
		ChannelID:   "whatsapp",
		Type:        TaskScheduleTestDrive,
		Description: "Prueba de ruta Toyota Hilux",
		VehicleID:   "v-hilux-2022",
		CreatedAt:   now,
	}
	task2 := Task{
		ID:          "task-2",
		ConvID:      convID,
		ChannelID:   "whatsapp",
		Type:        TaskCommercialFollowup,
		Description: "Seguimiento cliente",
		VehicleID:   "v-hilux-2022",
		CreatedAt:   now.Add(1 * time.Minute),
	}

	if err := mgr.RecordTask(ctx, task1); err != nil {
		t.Fatalf("unexpected record error: %v", err)
	}
	if err := mgr.RecordTask(ctx, task2); err != nil {
		t.Fatalf("unexpected record error: %v", err)
	}

	tasks := mgr.GetTasksByConv(convID)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ID != "task-1" || tasks[1].ID != "task-2" {
		t.Fatalf("expected tasks in order, got %+v", tasks)
	}
	if tasks[0].Status != TaskStatusPending {
		t.Fatalf("expected default pending status, got %s", tasks[0].Status)
	}

	emptyTasks := mgr.GetTasksByConv("non-existent")
	if len(emptyTasks) != 0 {
		t.Fatalf("expected 0 tasks for non-existent conv, got %d", len(emptyTasks))
	}
}

func TestMemoryManager_ValidationAndDefaults(t *testing.T) {
	ctx := context.Background()
	mgr := NewMemoryManager()

	// Missing ConvID should fail
	err := mgr.RecordTask(ctx, Task{})
	if err != ErrEmptyConvID {
		t.Fatalf("expected ErrEmptyConvID, got %v", err)
	}

	// Automatic ID, Status, Type, and CreatedAt assignment
	task := Task{ConvID: "conv-auto"}
	if err := mgr.RecordTask(ctx, task); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	recorded := mgr.GetTasksByConv("conv-auto")
	if len(recorded) != 1 {
		t.Fatalf("expected 1 task, got %d", len(recorded))
	}
	r := recorded[0]
	if r.ID == "" {
		t.Fatal("expected auto-generated task ID")
	}
	if r.Status != TaskStatusPending {
		t.Fatalf("expected status pending, got %s", r.Status)
	}
	if r.Type != TaskGeneralInquiry {
		t.Fatalf("expected type general_inquiry, got %s", r.Type)
	}
	if r.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt")
	}
}

func TestMemoryManager_GetTaskAndListTasks(t *testing.T) {
	ctx := context.Background()
	mgr := NewMemoryManager()

	task := Task{
		ID:          "task-lookup-1",
		ConvID:      "conv-lookup",
		ChannelID:   "email_pilot",
		Type:        TaskCreditApplication,
		Description: "Consulta de crédito",
	}
	_ = mgr.RecordTask(ctx, task)

	found, err := mgr.GetTask(ctx, "task-lookup-1")
	if err != nil {
		t.Fatalf("expected to find task, got error: %v", err)
	}
	if found.Description != "Consulta de crédito" {
		t.Fatalf("unexpected description: %s", found.Description)
	}

	_, errNotFound := mgr.GetTask(ctx, "does-not-exist")
	if errNotFound != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound, got %v", errNotFound)
	}

	all := mgr.ListTasks(ctx)
	if len(all) != 1 {
		t.Fatalf("expected 1 task in ListTasks, got %d", len(all))
	}
}

func TestMemoryManager_UpdateTaskStatus(t *testing.T) {
	ctx := context.Background()
	mgr := NewMemoryManager()

	task := Task{
		ID:     "task-status-1",
		ConvID: "conv-status",
	}
	_ = mgr.RecordTask(ctx, task)

	err := mgr.UpdateTaskStatus(ctx, "task-status-1", TaskStatusCompleted)
	if err != nil {
		t.Fatalf("unexpected error updating status: %v", err)
	}

	updated, _ := mgr.GetTask(ctx, "task-status-1")
	if updated.Status != TaskStatusCompleted {
		t.Fatalf("expected status completed, got %s", updated.Status)
	}
	if updated.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero UpdatedAt")
	}

	errInvalid := mgr.UpdateTaskStatus(ctx, "non-existing", TaskStatusCompleted)
	if errInvalid != ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound, got %v", errInvalid)
	}
}

func TestNormalizeTaskType(t *testing.T) {
	cases := []struct {
		input    string
		expected TaskType
	}{
		{"agendar_prueba_ruta", TaskScheduleTestDrive},
		{"schedule_test_drive", TaskScheduleTestDrive},
		{"visita_taller", TaskScheduleTestDrive},
		{"solicitud_credito", TaskCreditApplication},
		{"credit_application", TaskCreditApplication},
		{"estudio_financiacion", TaskCreditApplication},
		{"seguimiento_comercial", TaskCommercialFollowup},
		{"commercial_followup", TaskCommercialFollowup},
		{"contacto_asesor", TaskCommercialFollowup},
		{"consulta_general", TaskGeneralInquiry},
		{"otro_desconocido", TaskGeneralInquiry},
	}

	for _, tc := range cases {
		got := NormalizeTaskType(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizeTaskType(%q) = %s; want %s", tc.input, got, tc.expected)
		}
	}
}

func TestInferTaskFromTriage(t *testing.T) {
	tType1, _ := InferTaskFromTriage(triage.IntentVisit, true)
	if tType1 != TaskScheduleTestDrive {
		t.Errorf("expected TaskScheduleTestDrive, got %s", tType1)
	}

	tType2, _ := InferTaskFromTriage(triage.IntentCreditStudy, false)
	if tType2 != TaskCreditApplication {
		t.Errorf("expected TaskCreditApplication, got %s", tType2)
	}

	tType3, _ := InferTaskFromTriage(triage.IntentPrice, true)
	if tType3 != TaskCommercialFollowup {
		t.Errorf("expected TaskCommercialFollowup, got %s", tType3)
	}

	tType4, _ := InferTaskFromTriage(triage.IntentGreeting, false)
	if tType4 != TaskGeneralInquiry {
		t.Errorf("expected TaskGeneralInquiry, got %s", tType4)
	}
}

func TestMemoryManager_ConcurrentAccess(t *testing.T) {
	mgr := NewMemoryManager()
	ctx := context.Background()
	concurrency := 50
	var wg sync.WaitGroup

	// Writers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			convID := fmt.Sprintf("conv-conc-%d", idx%5)
			task := Task{
				ID:        fmt.Sprintf("task-%d", idx),
				ConvID:    convID,
				ChannelID: "whatsapp",
				Type:      TaskCommercialFollowup,
			}
			_ = mgr.RecordTask(ctx, task)
		}(i)
	}

	// Readers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			convID := fmt.Sprintf("conv-conc-%d", idx%5)
			_ = mgr.GetTasksByConv(convID)
			_ = mgr.ListTasks(ctx)
		}(i)
	}

	wg.Wait()

	all := mgr.ListTasks(ctx)
	if len(all) != concurrency {
		t.Fatalf("expected %d total recorded tasks, got %d", concurrency, len(all))
	}
}

func TestMemoryManager_ContextCancellation(t *testing.T) {
	mgr := NewMemoryManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := mgr.RecordTask(ctx, Task{ConvID: "c1"})
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	_, errGet := mgr.GetTask(ctx, "t1")
	if errGet != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", errGet)
	}
}
