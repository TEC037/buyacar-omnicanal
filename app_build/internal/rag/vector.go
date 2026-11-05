package rag

import (
	"math"
)

// DotProduct calculates the scalar dot product between two float32 slices in pure Go.
func DotProduct(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var sum float32
	for i := 0; i < len(a); i++ {
		sum += a[i] * b[i]
	}
	return sum
}

// Norm computes the Euclidean L2 norm of a vector.
func Norm(v []float32) float32 {
	if len(v) == 0 {
		return 0
	}
	var sum float64
	for _, val := range v {
		sum += float64(val) * float64(val)
	}
	return float32(math.Sqrt(sum))
}

// Normalize produces a unit-length L2-normalized copy of vector v.
// If the vector is zero-length or has zero norm, it returns a zero-filled vector.
func Normalize(v []float32) []float32 {
	if len(v) == 0 {
		return nil
	}
	out := make([]float32, len(v))
	copy(out, v)
	NormalizeInPlace(out)
	return out
}

// NormalizeInPlace normalizes slice v in-place to unit L2 norm.
func NormalizeInPlace(v []float32) {
	n := Norm(v)
	if n <= 1e-9 {
		for i := range v {
			v[i] = 0
		}
		return
	}
	inv := 1.0 / n
	for i := range v {
		v[i] *= inv
	}
}

// CosineSimilarity computes the cosine of the angle between two vectors.
// Returns a value clamped to [-1.0, 1.0], or 0.0 if either vector has zero magnitude or lengths mismatch.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0.0
	}

	var dot, normA, normB float64
	for i := 0; i < len(a); i++ {
		va := float64(a[i])
		vb := float64(b[i])
		dot += va * vb
		normA += va * va
		normB += vb * vb
	}

	if normA <= 1e-12 || normB <= 1e-12 {
		return 0.0
	}

	sim := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if sim > 1.0 {
		return 1.0
	}
	if sim < -1.0 {
		return -1.0
	}
	return sim
}
