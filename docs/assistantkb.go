package assistantkb

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed *.md deployment/*.md
var knowledgeFS embed.FS

type knowledgeDocument struct {
	Path       string
	PathLower  string
	Title      string
	TitleLower string
	Sections   []knowledgeSection
}

type knowledgeSection struct {
	Heading      string
	HeadingLower string
	Body         string
	SearchText   string
}

type knowledgeMatch struct {
	Path    string
	Heading string
	Excerpt string
	Score   int
}

var (
	knowledgeDocsOnce sync.Once
	knowledgeDocs     []knowledgeDocument
	knowledgeDocsErr  error

	knowledgeTokenRE   = regexp.MustCompile(`[a-z0-9_./-]+`)
	knowledgeStopwords = map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "by": {},
		"do": {}, "for": {}, "from": {}, "get": {}, "how": {}, "i": {}, "in": {}, "into": {},
		"is": {}, "it": {}, "its": {}, "of": {}, "on": {}, "or": {}, "should": {}, "that": {},
		"the": {}, "their": {}, "them": {}, "this": {}, "to": {}, "use": {}, "what": {}, "when": {},
		"where": {}, "which": {}, "with": {}, "your": {},
	}
	knowledgeShortAllowlist = map[string]struct{}{
		"id": {}, "ui": {}, "mcp": {},
	}
)

func BuildPromptContext(prompt string) string {
	matches := Search(prompt, 3)
	if len(matches) == 0 {
		return ""
	}

	lines := []string{
		"Packaged tunnel-client knowledge base injected from the binary.",
		"These markdown snippets remain available even when the source checkout is not present on disk.",
		"Use them as repository-specific reference before falling back to generic assumptions.",
	}
	for i, match := range matches {
		index := i + 1
		lines = append(lines, fmt.Sprintf("knowledge.match.%d.path=%s", index, match.Path))
		if match.Heading != "" {
			lines = append(lines, fmt.Sprintf("knowledge.match.%d.heading=%s", index, match.Heading))
		}
		lines = append(lines,
			fmt.Sprintf("knowledge.match.%d.excerpt_begin", index),
			match.Excerpt,
			fmt.Sprintf("knowledge.match.%d.excerpt_end", index),
		)
	}
	return strings.Join(lines, "\n")
}

func Search(prompt string, limit int) []knowledgeMatch {
	if limit <= 0 {
		limit = 1
	}
	docs, err := loadKnowledgeDocuments()
	if err != nil || len(docs) == 0 {
		return nil
	}

	terms := tokenizeKnowledgePrompt(prompt)
	if len(terms) == 0 {
		terms = []string{"tunnel-client"}
	}

	matches := make([]knowledgeMatch, 0, len(docs))
	for _, doc := range docs {
		best := knowledgeMatch{Path: doc.Path}
		for _, section := range doc.Sections {
			score := knowledgeScore(doc, section, terms)
			if score <= 0 {
				continue
			}
			if score > best.Score {
				best.Score = score
				best.Heading = section.Heading
				best.Excerpt = excerptKnowledgeSection(section, terms, 1200)
			}
		}
		if best.Score > 0 {
			matches = append(matches, best)
		}
	}

	if len(matches) == 0 {
		return nil
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Path < matches[j].Path
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

func loadKnowledgeDocuments() ([]knowledgeDocument, error) {
	knowledgeDocsOnce.Do(func() {
		paths := make([]string, 0, 16)
		topLevel, err := fs.Glob(knowledgeFS, "*.md")
		if err != nil {
			knowledgeDocsErr = err
			return
		}
		deployment, err := fs.Glob(knowledgeFS, "deployment/*.md")
		if err != nil {
			knowledgeDocsErr = err
			return
		}
		paths = append(paths, topLevel...)
		paths = append(paths, deployment...)
		sort.Strings(paths)

		docs := make([]knowledgeDocument, 0, len(paths))
		for _, path := range paths {
			data, err := knowledgeFS.ReadFile(path)
			if err != nil {
				knowledgeDocsErr = err
				return
			}
			displayPath := "docs/" + path
			sections := splitKnowledgeSections(string(data))
			if len(sections) == 0 {
				continue
			}
			title := sections[0].Heading
			if title == "" {
				title = displayPath
			}
			docs = append(docs, knowledgeDocument{
				Path:       displayPath,
				PathLower:  strings.ToLower(displayPath),
				Title:      title,
				TitleLower: strings.ToLower(title),
				Sections:   sections,
			})
		}
		knowledgeDocs = docs
	})
	return knowledgeDocs, knowledgeDocsErr
}

func splitKnowledgeSections(body string) []knowledgeSection {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	sections := make([]knowledgeSection, 0, 16)

	currentHeading := ""
	currentLines := make([]string, 0, len(lines))
	flush := func() {
		text := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if text == "" {
			currentLines = currentLines[:0]
			return
		}
		sections = append(sections, knowledgeSection{
			Heading:      currentHeading,
			HeadingLower: strings.ToLower(currentHeading),
			Body:         text,
			SearchText:   strings.ToLower(text),
		})
		currentLines = currentLines[:0]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			flush()
			currentHeading = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		}
		currentLines = append(currentLines, line)
	}
	flush()
	return sections
}

func tokenizeKnowledgePrompt(prompt string) []string {
	raw := knowledgeTokenRE.FindAllString(strings.ToLower(prompt), -1)
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	terms := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if _, stop := knowledgeStopwords[token]; stop {
			continue
		}
		if len(token) < 3 {
			if _, ok := knowledgeShortAllowlist[token]; !ok {
				continue
			}
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		terms = append(terms, token)
	}
	return terms
}

func knowledgeScore(doc knowledgeDocument, section knowledgeSection, terms []string) int {
	score := 0
	for _, term := range terms {
		if strings.Contains(doc.PathLower, term) {
			score += 20
		}
		if strings.Contains(doc.TitleLower, term) {
			score += 24
		}
		if strings.Contains(section.HeadingLower, term) {
			score += 28
		}
		if count := strings.Count(section.SearchText, term); count > 0 {
			if count > 6 {
				count = 6
			}
			score += count * 5
		}
	}
	if strings.Contains(section.HeadingLower, "chatgpt") && containsKnowledgeTerm(terms, "chatgpt") {
		score += 12
	}
	if strings.Contains(section.HeadingLower, "troubleshooting") && containsAnyKnowledgeTerm(terms, "debug", "diagnose", "troubleshoot", "healthz", "readyz", "logs", "log") {
		score += 12
	}
	return score
}

func containsKnowledgeTerm(terms []string, target string) bool {
	for _, term := range terms {
		if term == target {
			return true
		}
	}
	return false
}

func containsAnyKnowledgeTerm(terms []string, targets ...string) bool {
	for _, target := range targets {
		if containsKnowledgeTerm(terms, target) {
			return true
		}
	}
	return false
}

func excerptKnowledgeSection(section knowledgeSection, terms []string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 1200
	}
	text := strings.TrimSpace(section.Body)
	if len(text) <= maxChars {
		return text
	}

	lower := strings.ToLower(text)
	best := -1
	for _, term := range terms {
		if idx := strings.Index(lower, term); idx >= 0 && (best == -1 || idx < best) {
			best = idx
		}
	}

	start := 0
	if best > 0 {
		start = best - maxChars/4
		if start < 0 {
			start = 0
		}
	}
	end := start + maxChars
	if end > len(text) {
		end = len(text)
		start = max(0, end-maxChars)
	}
	snippet := strings.TrimSpace(text[start:end])
	if start > 0 {
		snippet = "...\n" + snippet
	}
	if end < len(text) {
		snippet += "\n..."
	}
	return snippet
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
