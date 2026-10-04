package docs

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Index is an in-memory BM25-style index over the pages.
type Index struct {
	docs   []Doc
	tf     []map[string]float64 // weighted term frequency per page
	length []float64
	avgLen float64
	df     map[string]int
	lower  []string // lowercased title+description+body for phrase matching
}

// Result is one search hit.
type Result struct {
	Doc     Doc
	Score   float64
	Snippet string
}

// NewIndex indexes docs.
func NewIndex(docs []Doc) *Index {
	ix := &Index{docs: docs, df: map[string]int{}}
	var total float64
	for _, d := range docs {
		tf := map[string]float64{}
		add := func(text string, weight float64) {
			for _, t := range tokenize(text) {
				tf[t] += weight
			}
		}
		add(d.Title, 8)
		add(strings.ReplaceAll(d.Path, "-", " "), 5)
		add(d.Description, 3)
		var bodyLen float64
		inFence := false
		for _, line := range strings.Split(d.Body, "\n") {
			if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
				inFence = !inFence
			}
			w := 1.0
			if !inFence && strings.HasPrefix(line, "#") {
				w = 4
			}
			toks := tokenize(line)
			bodyLen += float64(len(toks))
			for _, t := range toks {
				tf[t] += w
			}
		}
		ix.tf = append(ix.tf, tf)
		ix.length = append(ix.length, bodyLen+1)
		total += bodyLen + 1
		for t := range tf {
			ix.df[t]++
		}
		ix.lower = append(ix.lower, strings.ToLower(d.Title+"\n"+d.Description+"\n"+d.Body))
	}
	if len(docs) > 0 {
		ix.avgLen = total / float64(len(docs))
	}
	return ix
}

// Docs returns every indexed page.
func (ix *Index) Docs() []Doc { return ix.docs }

// Lookup finds a page by site path. It accepts /docs/x, docs/x, x, a trailing
// .md or .mdx, and a full goaisdk.com URL.
func (ix *Index) Lookup(p string) (Doc, bool) {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, SiteURL)
	if i := strings.IndexAny(p, "#?"); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimSuffix(strings.TrimSuffix(p, ".mdx"), ".md")
	p = "/" + strings.Trim(p, "/")
	if !strings.HasPrefix(p, "/docs") {
		p = "/docs" + p
	}
	for _, d := range ix.docs {
		if d.Path == p {
			return d, true
		}
	}
	return Doc{}, false
}

// Search returns up to limit pages ranked by relevance. typ filters by page
// type when non-empty.
func (ix *Index) Search(query, typ string, limit int) []Result {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	phrase := strings.ToLower(strings.Join(strings.Fields(query), " "))
	n := float64(len(ix.docs))
	var res []Result
	for i, d := range ix.docs {
		if typ != "" && d.Type != typ {
			continue
		}
		const k1, b = 1.2, 0.75
		var score float64
		matched := 0
		for _, t := range terms {
			f := ix.tf[i][t]
			if f == 0 {
				continue
			}
			matched++
			idf := math.Log(1 + (n-float64(ix.df[t])+0.5)/(float64(ix.df[t])+0.5))
			norm := f * (k1 + 1) / (f + k1*(1-b+b*ix.length[i]/ix.avgLen))
			score += idf * norm
		}
		if matched == 0 {
			continue
		}
		// Reward pages that match every term and pages containing the phrase.
		score *= 1 + 0.5*float64(matched)/float64(len(terms))
		if len(terms) > 1 {
			if strings.Contains(strings.ToLower(d.Title), phrase) {
				score *= 2
			} else if strings.Contains(ix.lower[i], phrase) {
				score *= 1.3
			}
		}
		// Migration notes mention many features in passing; prefer the pages
		// that teach them.
		switch d.Type {
		case "migration":
			score *= 0.6
		case "provider":
			score *= 0.85
		}
		res = append(res, Result{Doc: d, Score: score})
	}
	sort.SliceStable(res, func(a, b int) bool { return res[a].Score > res[b].Score })
	if len(res) > limit {
		res = res[:limit]
	}
	for i := range res {
		res[i].Snippet = snippet(res[i].Doc, terms)
	}
	return res
}

func snippet(d Doc, terms []string) string {
	set := map[string]bool{}
	for _, t := range terms {
		set[t] = true
	}
	best, bestN := "", 0
	for _, line := range strings.Split(d.Body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		n := 0
		seen := map[string]bool{}
		for _, t := range tokenize(line) {
			if set[t] && !seen[t] {
				seen[t] = true
				n++
			}
		}
		if n > bestN {
			best, bestN = line, n
		}
	}
	if best == "" {
		best = d.Description
	}
	if len(best) > 240 {
		best = best[:237] + "..."
	}
	return best
}

// tokenize lowercases text, splits it on non-alphanumerics, splits camelCase
// identifiers into parts (keeping the whole identifier too), and folds simple
// plurals.
func tokenize(s string) []string {
	var out []string
	var word []rune
	flush := func() {
		if len(word) == 0 {
			return
		}
		w := string(word)
		word = word[:0]
		parts := splitCamel(w)
		if len(parts) > 1 {
			out = append(out, stem(strings.ToLower(w)))
		}
		for _, p := range parts {
			out = append(out, stem(strings.ToLower(p)))
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func splitCamel(w string) []string {
	rs := []rune(w)
	var parts []string
	start := 0
	for i := 1; i < len(rs); i++ {
		if unicode.IsUpper(rs[i]) && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1]) ||
			(i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(rs[i-1]))) {
			parts = append(parts, string(rs[start:i]))
			start = i
		}
	}
	return append(parts, string(rs[start:]))
}

func stem(w string) string {
	if len(w) > 4 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
		return strings.TrimSuffix(w, "s")
	}
	return w
}
