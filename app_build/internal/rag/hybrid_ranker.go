package rag

import (
	"math"
	"sort"
)

// FusionStrategy defines the ranking fusion algorithm.
type FusionStrategy string

const (
	// FusionRRF uses Reciprocal Rank Fusion (standard k=60).
	FusionRRF FusionStrategy = "rrf"
	// FusionWeighted uses min-max normalized weighted linear combination.
	FusionWeighted FusionStrategy = "weighted"
)

// HybridRanker fuses lexical (BM25) and dense vector (Cosine) scores.
type HybridRanker struct {
	strategy     FusionStrategy
	rrfConstant  int
	bm25Weight   float64
	vectorWeight float64
}

// HybridRankerOption configures HybridRanker.
type HybridRankerOption func(*HybridRanker)

// WithFusionStrategy sets RRF or Weighted fusion.
func WithFusionStrategy(s FusionStrategy) HybridRankerOption {
	return func(hr *HybridRanker) {
		if s == FusionRRF || s == FusionWeighted {
			hr.strategy = s
		}
	}
}

// WithRRFConstant configures the RRF k constant (default 60).
func WithRRFConstant(k int) HybridRankerOption {
	return func(hr *HybridRanker) {
		if k > 0 {
			hr.rrfConstant = k
		}
	}
}

// WithWeights sets relative weighting for BM25 and Vector.
func WithWeights(bm25Weight, vectorWeight float64) HybridRankerOption {
	return func(hr *HybridRanker) {
		if bm25Weight >= 0 && vectorWeight >= 0 && (bm25Weight+vectorWeight) > 0 {
			total := bm25Weight + vectorWeight
			hr.bm25Weight = bm25Weight / total
			hr.vectorWeight = vectorWeight / total
		}
	}
}

// NewHybridRanker creates a new ranker with default RRF (k=60) and equal weights.
func NewHybridRanker(opts ...HybridRankerOption) *HybridRanker {
	hr := &HybridRanker{
		strategy:     FusionRRF,
		rrfConstant:  60,
		bm25Weight:   0.5,
		vectorWeight: 0.5,
	}
	for _, opt := range opts {
		opt(hr)
	}
	return hr
}

// Fuse merges BM25 and vector scores into a sorted list of DocScores.
func (hr *HybridRanker) Fuse(bm25Scores []DocScore, vectorScores []DocScore) []DocScore {
	if len(bm25Scores) == 0 && len(vectorScores) == 0 {
		return nil
	}

	if hr.strategy == FusionWeighted {
		return hr.fuseWeighted(bm25Scores, vectorScores)
	}
	return hr.fuseRRF(bm25Scores, vectorScores)
}

// fuseRRF combines rankings using Reciprocal Rank Fusion:
// Score(d) = w_bm25 / (k + rank_bm25(d)) + w_vec / (k + rank_vec(d))
func (hr *HybridRanker) fuseRRF(bm25Scores []DocScore, vectorScores []DocScore) []DocScore {
	// Sort BM25 descending
	sortedBM25 := make([]DocScore, len(bm25Scores))
	copy(sortedBM25, bm25Scores)
	sort.Slice(sortedBM25, func(i, j int) bool {
		if sortedBM25[i].Score == sortedBM25[j].Score {
			return sortedBM25[i].ID < sortedBM25[j].ID
		}
		return sortedBM25[i].Score > sortedBM25[j].Score
	})

	// Sort Vector descending
	sortedVec := make([]DocScore, len(vectorScores))
	copy(sortedVec, vectorScores)
	sort.Slice(sortedVec, func(i, j int) bool {
		if sortedVec[i].Score == sortedVec[j].Score {
			return sortedVec[i].ID < sortedVec[j].ID
		}
		return sortedVec[i].Score > sortedVec[j].Score
	})

	combined := make(map[string]float64)
	k := float64(hr.rrfConstant)

	for rank, item := range sortedBM25 {
		// 1-based rank
		combined[item.ID] += hr.bm25Weight * (1.0 / (k + float64(rank+1)))
	}

	for rank, item := range sortedVec {
		combined[item.ID] += hr.vectorWeight * (1.0 / (k + float64(rank+1)))
	}

	results := make([]DocScore, 0, len(combined))
	for id, score := range combined {
		results = append(results, DocScore{
			ID:    id,
			Score: score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID < results[j].ID
		}
		return results[i].Score > results[j].Score
	})

	return results
}

// fuseWeighted normalizes BM25 and Vector scores into [0, 1] and combines them.
func (hr *HybridRanker) fuseWeighted(bm25Scores []DocScore, vectorScores []DocScore) []DocScore {
	normBM25 := make(map[string]float64)
	if len(bm25Scores) > 0 {
		var minBM25, maxBM25 float64
		minBM25 = bm25Scores[0].Score
		maxBM25 = bm25Scores[0].Score
		for _, s := range bm25Scores {
			if s.Score < minBM25 {
				minBM25 = s.Score
			}
			if s.Score > maxBM25 {
				maxBM25 = s.Score
			}
		}
		rangeBM25 := maxBM25 - minBM25
		for _, s := range bm25Scores {
			if rangeBM25 > 1e-9 {
				normBM25[s.ID] = (s.Score - minBM25) / rangeBM25
			} else {
				normBM25[s.ID] = 1.0
			}
		}
	}

	normVec := make(map[string]float64)
	for _, s := range vectorScores {
		// Cosine similarity in [-1, 1], clamp negative to 0
		val := math.Max(0.0, s.Score)
		normVec[s.ID] = val
	}

	allIDs := make(map[string]struct{})
	for id := range normBM25 {
		allIDs[id] = struct{}{}
	}
	for id := range normVec {
		allIDs[id] = struct{}{}
	}

	results := make([]DocScore, 0, len(allIDs))
	for id := range allIDs {
		sBM25 := normBM25[id]
		sVec := normVec[id]
		finalScore := (hr.bm25Weight * sBM25) + (hr.vectorWeight * sVec)
		results = append(results, DocScore{
			ID:    id,
			Score: finalScore,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID < results[j].ID
		}
		return results[i].Score > results[j].Score
	})

	return results
}
