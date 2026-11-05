package rag

import (
	"math"
	"strings"
	"sync"
	"unicode"
)

// Default BM25 hyperparameters.
const (
	DefaultBM25K1 = 1.2
	DefaultBM25B  = 0.75
)

// spanishStopWords contains common Spanish stop words in lowercase accent-stripped form.
var spanishStopWords = map[string]struct{}{
	"a": {}, "al": {}, "algo": {}, "algunas": {}, "algunos": {}, "ante": {}, "antes": {},
	"como": {}, "con": {}, "contra": {}, "cual": {}, "cuando": {}, "de": {}, "del": {},
	"desde": {}, "donde": {}, "durante": {}, "e": {}, "el": {}, "ella": {}, "ellos": {},
	"en": {}, "entre": {}, "era": {}, "eres": {}, "es": {}, "esa": {}, "ese": {}, "eso": {}, "esta": {},
	"estaba": {}, "estamos": {}, "estar": {}, "estas": {}, "este": {}, "estos": {},
	"fue": {}, "fui": {}, "haber": {}, "habia": {}, "hasta": {}, "hay": {}, "la": {}, "las": {}, "le": {},
	"les": {}, "lo": {}, "los": {}, "mas": {}, "me": {}, "mi": {}, "mis": {}, "mucho": {},
	"muchos": {}, "muy": {}, "nada": {}, "ni": {}, "no": {}, "nos": {}, "nosotros": {},
	"o": {}, "otra": {}, "otras": {}, "otro": {}, "otros": {}, "para": {}, "pero": {},
	"poco": {}, "por": {}, "porque": {}, "que": {}, "quien": {}, "quienes": {},
	"se": {}, "sea": {}, "ser": {}, "si": {}, "sido": {}, "sin": {}, "sobre": {}, "son": {}, "su": {},
	"sus": {}, "tambien": {}, "tanto": {}, "te": {}, "ti": {}, "tiene": {}, "tienen": {}, "todo": {}, "todos": {},
	"tu": {}, "tus": {}, "un": {}, "una": {}, "uno": {}, "unos": {}, "ya": {}, "yo": {},
}

// stripAccents maps accented Spanish characters to ASCII equivalents.
func stripAccents(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case 'á', 'à', 'ä', 'â':
			b.WriteRune('a')
		case 'é', 'è', 'ë', 'ê':
			b.WriteRune('e')
		case 'í', 'ì', 'ï', 'î':
			b.WriteRune('i')
		case 'ó', 'ò', 'ö', 'ô':
			b.WriteRune('o')
		case 'ú', 'ù', 'ü', 'û':
			b.WriteRune('u')
		case 'Á', 'À', 'Ä', 'Â':
			b.WriteRune('a')
		case 'É', 'È', 'Ë', 'Ê':
			b.WriteRune('e')
		case 'Í', 'Ì', 'Ï', 'Î':
			b.WriteRune('i')
		case 'Ó', 'Ò', 'Ö', 'Ô':
			b.WriteRune('o')
		case 'Ú', 'Ù', 'Ü', 'Û':
			b.WriteRune('u')
		case 'ñ', 'Ñ':
			b.WriteRune('ñ')
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// Tokenize converts text into clean lexical tokens for Spanish NLP:
// lowercased, accent-stripped, punctuation stripped, and stop-words removed.
func Tokenize(text string) []string {
	normalized := stripAccents(text)
	var tokens []string
	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			w := current.String()
			current.Reset()
			// Keep tokens with length >= 2, or length == 1 if it is a digit
			if len(w) >= 2 || (len(w) == 1 && unicode.IsDigit(rune(w[0]))) {
				if _, isStop := spanishStopWords[w]; !isStop {
					tokens = append(tokens, w)
				}
			}
		}
	}

	for _, r := range normalized {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// stemSpanish normalizes common Spanish morphological variations (plurals and domain roots)
// so queries like "ubicados"/"horario"/"usados" match "ubicación"/"horarios"/"usado".
func stemSpanish(tok string) string {
	switch {
	case strings.HasPrefix(tok, "ubic"):
		return "ubic"
	case strings.HasPrefix(tok, "horari"):
		return "horari"
	case strings.HasPrefix(tok, "garanti"):
		return "garanti"
	case strings.HasPrefix(tok, "emplead"):
		return "emplead"
	case strings.HasPrefix(tok, "independient"):
		return "independient"
	case strings.HasPrefix(tok, "usad"):
		return "usad"
	case strings.HasPrefix(tok, "vehicul"):
		return "vehicul"
	case strings.HasPrefix(tok, "requisit"):
		return "requisit"
	case strings.HasPrefix(tok, "credit"):
		return "credit"
	case strings.HasPrefix(tok, "financ"):
		return "financ"
	}
	if len(tok) > 4 {
		if strings.HasSuffix(tok, "ciones") {
			return strings.TrimSuffix(tok, "es")
		}
		if strings.HasSuffix(tok, "s") {
			return strings.TrimSuffix(tok, "s")
		}
	}
	return tok
}

// docRecord stores token frequencies for an indexed chunk.
type docRecord struct {
	id     string
	length int
	tf     map[string]int
}

// DocScore represents BM25 scoring result.
type DocScore struct {
	ID    string
	Score float64
}

// BM25Index implements BM25 retrieval across in-memory documents.
type BM25Index struct {
	k1          float64
	b           float64
	mu          sync.RWMutex
	docs        map[string]*docRecord
	df          map[string]int
	totalTokens int
	avgdl       float64
}

// NewBM25Index initializes a new BM25 index with default parameters.
func NewBM25Index() *BM25Index {
	return &BM25Index{
		k1:    DefaultBM25K1,
		b:     DefaultBM25B,
		docs:  make(map[string]*docRecord),
		df:    make(map[string]int),
		avgdl: 0,
	}
}

// AddDocument indexes tokens for a document chunk.
func (idx *BM25Index) AddDocument(id string, text string) {
	tokens := Tokenize(text)
	tf := make(map[string]int, len(tokens)*2)
	for _, tok := range tokens {
		tf[tok]++
		if stem := stemSpanish(tok); stem != "" && stem != tok {
			tf[stem]++
		}
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// If replacing existing doc, subtract old frequencies first
	if old, exists := idx.docs[id]; exists {
		idx.totalTokens -= old.length
		for tok := range old.tf {
			idx.df[tok]--
			if idx.df[tok] <= 0 {
				delete(idx.df, tok)
			}
		}
	}

	doc := &docRecord{
		id:     id,
		length: len(tokens),
		tf:     tf,
	}
	idx.docs[id] = doc
	idx.totalTokens += doc.length

	for tok := range tf {
		idx.df[tok]++
	}

	if len(idx.docs) > 0 {
		idx.avgdl = float64(idx.totalTokens) / float64(len(idx.docs))
	} else {
		idx.avgdl = 0
	}
}

// Score computes the BM25 score for the query text against all indexed documents.
func (idx *BM25Index) Score(query string) []DocScore {
	qTokens := Tokenize(query)
	if len(qTokens) == 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	numDocs := float64(len(idx.docs))
	if numDocs == 0 {
		return nil
	}

	// Calculate query term frequencies to handle repeated query tokens
	qTF := make(map[string]int, len(qTokens)*2)
	for _, t := range qTokens {
		qTF[t]++
		if stem := stemSpanish(t); stem != "" && stem != t {
			qTF[stem]++
		}
	}

	scores := make([]DocScore, 0, len(idx.docs))

	for id, doc := range idx.docs {
		var docScore float64
		docLen := float64(doc.length)

		for term := range qTF {
			docTermFreq, present := doc.tf[term]
			if !present || docTermFreq <= 0 {
				continue
			}

			docFreq := float64(idx.df[term])
			// Standard Robertson-Spärck Jones IDF with 0.5 smoothing
			// idf = ln(1 + (N - df + 0.5) / (df + 0.5))
			idf := math.Log(1.0 + (numDocs-docFreq+0.5)/(docFreq+0.5))
			if idf < 0 {
				idf = 0
			}

			// BM25 term weighting
			tfVal := float64(docTermFreq)
			denom := tfVal + idx.k1*(1.0-idx.b+idx.b*(docLen/math.Max(1.0, idx.avgdl)))
			termScore := idf * ((tfVal * (idx.k1 + 1.0)) / denom)

			docScore += termScore
		}

		if docScore > 0 {
			scores = append(scores, DocScore{
				ID:    id,
				Score: docScore,
			})
		}
	}

	return scores
}
