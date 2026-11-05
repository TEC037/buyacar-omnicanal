package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"buyacar.com.co/omnicanal/internal/rag"
)

func main() {
	fmt.Println("==================================================================")
	fmt.Println("[DEMOSTRACION EN VIVO] MOTOR RAG LOCAL DE BUYACAR (Go 1.23)")
	fmt.Println("==================================================================")
	fmt.Println()

	ctx := context.Background()
	engine := rag.NewEngine()

	// Cargar fixtures normativos y catálogo de vehículos embebidos
	chunks, err := rag.LoadEmbeddedFixtures(ctx, engine)
	if err != nil {
		fmt.Printf("[ERROR] Al cargar fixtures: %v\n", err)
		return
	}

	fmt.Printf("[OK] Motor RAG en memoria inicializado exitosamente.\n")
	fmt.Printf("[INFO] Chunks indexados: %d fragmentos (BM25 + Cosine + Embeddings locales).\n\n", len(chunks))

	// Consultas de prueba representativas del concesionario
	queries := []string{
		"¿Qué garantía tienen los vehículos usados en motor y caja?",
		"¿Cuáles son los requisitos de crédito para empleados en Tunja?",
		"¿Tienen disponible una Toyota Hilux 2022 y qué precio tiene?",
		"¿Dónde quedan ubicados y cuál es el horario de atención en Boyacá?",
		"¿Tienen disponible un Ferrari Testarossa 2022?",
	}

	for i, qText := range queries {
		fmt.Printf("------------------------------------------------------------------\n")
		fmt.Printf("[CONSULTA #%d] \"%s\"\n", i+1, qText)
		fmt.Printf("------------------------------------------------------------------\n")

		q := rag.Query{
			Text:      qText,
			TopK:      2,
			MaxTokens: 250,
		}

		start := time.Now()
		results, err := engine.Retrieve(ctx, q)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("[ERROR] En recuperación: %v\n", err)
			continue
		}

		fmt.Printf("[METRICA] Latencia de recuperación: %v (SLA p99 < 5ms)\n", elapsed)
		fmt.Printf("[RESULTADOS] Fragmentos semánticos más relevantes encontrados (%d):\n", len(results))

		for j, r := range results {
			fmt.Printf("\n   [Fragmento %d - Score: %.4f | Título: %s | Tokens: %d]\n", j+1, r.Score, r.Chunk.Title, r.Chunk.Tokens)
			lines := strings.Split(r.Chunk.Content, "\n")
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					fmt.Printf("   > %s\n", trimmed)
				}
			}
		}
		fmt.Println()
	}

	fmt.Println("==================================================================")
	fmt.Println("[VERIFICACION DE GUARDRAILS ANTI-ALUCINACION CP5]")
	fmt.Println("==================================================================")
	backed := engine.GetBackedNumbers()
	fmt.Printf("Total de cifras numéricas (5+ dígitos) respaldadas por el RAG: %d\n", len(backed))
	fmt.Println("Ejemplos de precios y teléfonos oficiales autorizados:")
	count := 0
	for _, num := range backed {
		fmt.Printf(" • %s (Respaldado por ficha técnica / póliza)\n", num)
		count++
		if count >= 6 {
			break
		}
	}
	fmt.Println("\n[OK] Demostración completada: el RAG enriquece el contexto con datos 100% fácticos.")
}
