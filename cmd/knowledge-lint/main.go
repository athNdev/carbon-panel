// Command knowledge-lint validates the repository knowledge base.
//
// The knowledge base is a directory of Markdown concepts with YAML
// frontmatter. This command is deliberately a linter, not a database engine:
// git owns history and merging, and the files stay readable without tooling.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

const knowledgeDir = "knowledge"

var (
	allowedTypes = []string{
		"Overview",
		"Architecture",
		"Invariant",
		"Workflow",
		"Runbook",
		"Decision",
		"Domain Term",
		"Failure Mode",
	}
	allowedStatuses = []string{"draft", "stable", "deprecated"}
	linkPattern     = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
)

var forbiddenPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\b(ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)},
	{"Stripe secret", regexp.MustCompile(`\b(sk_live|sk_test|whsec)_[A-Za-z0-9]{8,}`)},
	{"Windows home path", regexp.MustCompile(`[A-Za-z]:\\Users\\`)},
	{"Unix home path", regexp.MustCompile(`(^|[\s(])/(home|Users)/[A-Za-z0-9._-]+/`)},
	{"private network URL", regexp.MustCompile(`https?://(10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[01])\.)`)},
	{"workstation workspace path", regexp.MustCompile(`C:\\research_learning_new`)},
	{"known homelab host", regexp.MustCompile(`\b(prox3|pveZ3|carbon\.local)\b`)},
}

type source struct {
	Title    string `yaml:"title"`
	Resource string `yaml:"resource"`
}

type verification struct {
	By string `yaml:"by"`
	At string `yaml:"at"`
}

type meta struct {
	Type        string        `yaml:"type"`
	Title       string        `yaml:"title"`
	Description string        `yaml:"description"`
	Status      string        `yaml:"status"`
	Governance  string        `yaml:"governance"`
	Owners      []string      `yaml:"owners"`
	CodeRefs    []string      `yaml:"code_refs"`
	Sources     []source      `yaml:"sources"`
	Verified    *verification `yaml:"verified"`
	ReviewAfter string        `yaml:"review_after"`
}

type concept struct {
	ID   string
	Rel  string
	Abs  string
	Meta meta
	Body string
}

type report struct {
	Root           string   `json:"root"`
	Concepts       int      `json:"concepts"`
	IndexesWritten int      `json:"indexes_written"`
	QueriesChecked int      `json:"queries_checked"`
	QueriesPassed  int      `json:"queries_passed"`
	QueriesTop1    int      `json:"queries_top1"`
	TrustHuman     int      `json:"trust_human_reviewed"`
	TrustMachine   int      `json:"trust_machine_confirmed"`
	Errors         []string `json:"errors"`
	Warnings       []string `json:"warnings"`
}

type queryCase struct {
	Query  string `yaml:"query"`
	Expect string `yaml:"expect"`
}

type queryResult struct {
	Query  string
	Expect string
	Rank   int
	Top    []string
}

type dirInfo struct {
	rel      string
	concepts []*concept
	subdirs  []string
}

func main() {
	root := flag.String("root", ".", "repository root")
	fix := flag.Bool("fix", false, "rewrite generated index files")
	asJSON := flag.Bool("json", false, "emit a machine-readable report")
	forPath := flag.String("for-path", "", "list concepts governing a source path")
	flag.Parse()

	if *forPath != "" {
		os.Exit(runForPath(*root, *forPath))
	}

	r := lint(*root, *fix)
	if *asJSON {
		out, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println(string(out))
	} else {
		printReport(r)
	}
	if len(r.Errors) > 0 {
		os.Exit(1)
	}
}

func lint(root string, fix bool) report {
	r := report{Root: root, Errors: []string{}, Warnings: []string{}}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("resolve root: %v", err))
		return r
	}
	kroot := filepath.Join(absRoot, knowledgeDir)
	if info, err := os.Stat(kroot); err != nil || !info.IsDir() {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: knowledge directory not found", knowledgeDir))
		return r
	}

	concepts, loadErrors := loadConcepts(absRoot, kroot)
	r.Errors = append(r.Errors, loadErrors...)
	r.Concepts = len(concepts)

	for _, c := range concepts {
		errs, warns := c.validate(absRoot)
		r.Errors = append(r.Errors, errs...)
		r.Warnings = append(r.Warnings, warns...)
		r.Errors = append(r.Errors, scanForbidden(c)...)
		if c.Meta.Verified != nil {
			if strings.HasPrefix(c.Meta.Verified.By, "human:") {
				r.TrustHuman++
			} else {
				r.TrustMachine++
			}
		}
	}
	r.Errors = append(r.Errors, checkLinks(absRoot, concepts)...)
	r.Errors = append(r.Errors, checkIndexes(absRoot, kroot, concepts, fix, &r)...)
	r.Warnings = append(r.Warnings, duplicateTitles(concepts)...)

	queries, queryErr := loadQueries(kroot)
	if queryErr != nil {
		r.Errors = append(r.Errors, queryErr.Error())
	} else if len(queries) > 0 {
		known := map[string]bool{}
		for _, c := range concepts {
			known[c.ID] = true
		}
		valid := make([]queryCase, 0, len(queries))
		for _, query := range queries {
			if !known[query.Expect] {
				r.Errors = append(r.Errors, fmt.Sprintf("query %q: expected concept %s does not exist", query.Query, query.Expect))
				continue
			}
			valid = append(valid, query)
		}
		results := runQueries(concepts, valid)
		r.QueriesChecked = len(results)
		for _, result := range results {
			if result.Rank == 1 {
				r.QueriesTop1++
			}
			if result.Rank > 0 && result.Rank <= 3 {
				r.QueriesPassed++
				continue
			}
			r.Errors = append(r.Errors, fmt.Sprintf("query %q: expected %s in top 3, got %v", result.Query, result.Expect, result.Top))
		}
	}

	sort.Strings(r.Errors)
	sort.Strings(r.Warnings)
	return r
}

func loadConcepts(absRoot, kroot string) ([]*concept, []string) {
	var concepts []*concept
	var errs []string

	err := filepath.WalkDir(kroot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != kroot && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".md" {
			return nil
		}
		base := filepath.Base(p)
		if base == "index.md" || base == "README.md" {
			return nil
		}
		c, parseErr := parseConcept(absRoot, kroot, p)
		if parseErr != nil {
			rel, _ := filepath.Rel(kroot, p)
			errs = append(errs, fmt.Sprintf("%s: %v", filepath.ToSlash(rel), parseErr))
			return nil
		}
		concepts = append(concepts, c)
		return nil
	})
	if err != nil {
		errs = append(errs, fmt.Sprintf("walk %s: %v", kroot, err))
	}
	return concepts, errs
}

func parseConcept(absRoot, kroot, file string) (*concept, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	s := strings.TrimPrefix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\ufeff")
	lines := strings.Split(s, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("missing YAML frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("unterminated YAML frontmatter")
	}
	var m meta
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &m); err != nil {
		return nil, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	rel, err := filepath.Rel(kroot, file)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	return &concept{
		ID:   strings.TrimSuffix(rel, ".md"),
		Rel:  rel,
		Abs:  file,
		Meta: m,
		Body: strings.Join(lines[end+1:], "\n"),
	}, nil
}

func (c *concept) validate(root string) (errs, warns []string) {
	add := func(format string, args ...interface{}) {
		errs = append(errs, c.ID+": "+fmt.Sprintf(format, args...))
	}
	warn := func(format string, args ...interface{}) {
		warns = append(warns, c.ID+": "+fmt.Sprintf(format, args...))
	}

	if strings.TrimSpace(c.Meta.Type) == "" {
		add("type is required")
	} else if !contains(allowedTypes, c.Meta.Type) {
		add("unknown type %q", c.Meta.Type)
	}
	if strings.TrimSpace(c.Meta.Title) == "" {
		add("title is required")
	} else if len([]rune(c.Meta.Title)) > 120 {
		add("title is longer than 120 characters")
	}
	if strings.TrimSpace(c.Meta.Description) == "" {
		add("description is required")
	} else if strings.Contains(c.Meta.Description, "\n") {
		add("description must be one line")
	} else if len([]rune(c.Meta.Description)) > 240 {
		add("description is longer than 240 characters")
	}
	if strings.TrimSpace(c.Meta.Status) == "" {
		add("status is required")
	} else if !contains(allowedStatuses, c.Meta.Status) {
		add("unknown status %q", c.Meta.Status)
	}
	if len(c.Meta.Owners) == 0 {
		add("owners is required")
	}
	for _, owner := range c.Meta.Owners {
		if strings.TrimSpace(owner) == "" {
			add("owners must not contain an empty value")
		}
	}

	if c.Meta.Type == "Architecture" || c.Meta.Type == "Invariant" {
		if len(c.Meta.CodeRefs) == 0 {
			add("code_refs is required for %s", c.Meta.Type)
		}
	}
	if c.Meta.Verified != nil {
		if !strings.HasPrefix(c.Meta.Verified.By, "human:") &&
			!strings.HasPrefix(c.Meta.Verified.By, "agent:") &&
			!strings.HasPrefix(c.Meta.Verified.By, "process:") {
			add("verified.by must start with human:, agent: or process:")
		}
		if verifiedAt, err := time.Parse("2006-01-02", c.Meta.Verified.At); err != nil {
			add("verified.at must be YYYY-MM-DD")
		} else if lastCommit, ok := lastCommitDate(root, filepath.Join(knowledgeDir, c.Rel)); ok && isVerificationStale(verifiedAt, lastCommit) {
			warn("verified %s predates the last change to this file (%s); re-verify", c.Meta.Verified.At, lastCommit.Format("2006-01-02"))
		}
	}
	if c.Meta.Type == "Invariant" {
		if c.Meta.Status == "stable" && c.Meta.Verified == nil {
			add("verified is required for a stable Invariant; keep it draft until a reviewer records verified")
		}
		if c.Meta.Status == "draft" && c.Meta.Verified == nil {
			warn("draft Invariant has no verification")
		}
	}
	if c.Meta.Governance != "" && c.Meta.Governance != "constraint" && c.Meta.Governance != "hold" && c.Meta.Governance != "context" {
		add("unknown governance %q", c.Meta.Governance)
	}
	if c.Meta.ReviewAfter != "" {
		if t, err := time.Parse("2006-01-02", c.Meta.ReviewAfter); err != nil {
			add("review_after must be YYYY-MM-DD")
		} else if t.Before(time.Now().UTC()) {
			warn("review_after %s has passed", c.Meta.ReviewAfter)
		}
	}
	for _, ref := range c.Meta.CodeRefs {
		if err := checkRef(root, ref); err != nil {
			add("code_refs %q: %v", ref, err)
		}
	}
	return errs, warns
}

func checkRef(root, ref string) error {
	ref = strings.TrimSpace(strings.ReplaceAll(ref, "\\", "/"))
	if ref == "" {
		return fmt.Errorf("empty path")
	}
	if filepath.IsAbs(ref) || strings.HasPrefix(ref, "/") {
		return fmt.Errorf("must be repository-relative")
	}
	clean := path.Clean(ref)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("escapes the repository")
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	if strings.ContainsAny(clean, "*?[") {
		matches, err := filepath.Glob(full)
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			return fmt.Errorf("no matches")
		}
		return nil
	}
	if _, err := os.Stat(full); err != nil {
		return fmt.Errorf("not found")
	}
	return nil
}

func checkLinks(root string, concepts []*concept) []string {
	var errs []string
	for _, c := range concepts {
		for _, match := range linkPattern.FindAllStringSubmatch(c.Body, -1) {
			target := cleanLinkTarget(match[1])
			if skipLink(target) {
				continue
			}
			abs := resolveLink(root, c, target)
			if abs == "" {
				continue
			}
			if _, err := os.Stat(abs); err != nil {
				errs = append(errs, fmt.Sprintf("%s: link %q does not resolve", c.ID, target))
			}
		}
	}
	return errs
}

func cleanLinkTarget(target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "<")
	target = strings.TrimSuffix(target, ">")
	if i := strings.IndexAny(target, " \t"); i >= 0 {
		target = target[:i]
	}
	if i := strings.Index(target, "#"); i >= 0 {
		target = target[:i]
	}
	if i := strings.Index(target, "?"); i >= 0 {
		target = target[:i]
	}
	return strings.TrimSpace(target)
}

func skipLink(target string) bool {
	return target == "" ||
		strings.HasPrefix(target, "#") ||
		strings.Contains(target, "://") ||
		strings.HasPrefix(target, "mailto:") ||
		strings.HasPrefix(target, "tel:") ||
		strings.HasPrefix(target, "{{")
}

func resolveLink(root string, c *concept, target string) string {
	target = strings.ReplaceAll(target, "\\", "/")
	var abs string
	if strings.HasPrefix(target, "/") {
		abs = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(target, "/")))
	} else {
		abs = filepath.Join(filepath.Dir(c.Abs), filepath.FromSlash(target))
	}
	if strings.HasSuffix(target, "/") {
		abs = filepath.Join(abs, "index.md")
	}
	if _, err := os.Stat(abs); err != nil && filepath.Ext(abs) == "" {
		if _, mdErr := os.Stat(abs + ".md"); mdErr == nil {
			return abs + ".md"
		}
	}
	return abs
}

func scanForbidden(c *concept) []string {
	var text strings.Builder
	text.WriteString(c.Meta.Title)
	text.WriteString("\n")
	text.WriteString(c.Meta.Description)
	text.WriteString("\n")
	text.WriteString(c.Body)
	for _, ref := range c.Meta.CodeRefs {
		text.WriteString("\n")
		text.WriteString(ref)
	}
	for _, s := range c.Meta.Sources {
		text.WriteString("\n")
		text.WriteString(s.Title)
		text.WriteString("\n")
		text.WriteString(s.Resource)
	}

	var errs []string
	for _, pattern := range forbiddenPatterns {
		if pattern.re.MatchString(text.String()) {
			errs = append(errs, fmt.Sprintf("%s: forbidden content: %s", c.ID, pattern.name))
		}
	}
	return errs
}

func checkIndexes(root, kroot string, concepts []*concept, fix bool, r *report) []string {
	dirs := map[string]*dirInfo{}
	ensure := func(rel string) *dirInfo {
		if di, ok := dirs[rel]; ok {
			return di
		}
		di := &dirInfo{rel: rel}
		dirs[rel] = di
		return di
	}

	walkErr := filepath.WalkDir(kroot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(kroot, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		if rel != "" && strings.HasPrefix(path.Base(rel), ".") {
			return fs.SkipDir
		}
		ensure(rel)
		return nil
	})
	if walkErr != nil {
		return []string{fmt.Sprintf("walk %s: %v", kroot, walkErr)}
	}

	for _, c := range concepts {
		dir := path.Dir(c.ID)
		if dir == "." {
			dir = ""
		}
		ensure(dir).concepts = append(ensure(dir).concepts, c)
	}

	for rel := range dirs {
		if rel == "" {
			continue
		}
		parent := path.Dir(rel)
		if parent == "." {
			parent = ""
		}
		ensure(parent).subdirs = append(ensure(parent).subdirs, rel)
	}

	sections := loadSections(kroot, r)
	for rel, di := range dirs {
		if len(di.concepts) == 0 && len(di.subdirs) == 0 {
			continue
		}
		if rel != "" {
			if _, ok := sections[rel]; !ok {
				r.Warnings = append(r.Warnings, fmt.Sprintf("%s: no description in sections.yaml", rel))
			}
		}
	}

	var errs []string
	for rel, di := range dirs {
		if len(di.concepts) == 0 && len(di.subdirs) == 0 {
			continue
		}
		content := renderIndex(rel, di, sections)
		indexPath := filepath.Join(kroot, filepath.FromSlash(rel), "index.md")
		existing, readErr := os.ReadFile(indexPath)
		if readErr == nil && string(existing) == content {
			continue
		}
		if fix {
			if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
				errs = append(errs, fmt.Sprintf("%s: write index: %v", displayDir(rel), err))
				continue
			}
			r.IndexesWritten++
			continue
		}
		errs = append(errs, fmt.Sprintf("%s: index.md is missing or out of date; run make knowledge-index", displayDir(rel)))
	}
	return errs
}

func loadSections(kroot string, r *report) map[string]string {
	b, err := os.ReadFile(filepath.Join(kroot, "sections.yaml"))
	if err != nil {
		r.Warnings = append(r.Warnings, "sections.yaml is missing; directory descriptions will be generic")
		return map[string]string{}
	}
	var sections map[string]string
	if err := yaml.Unmarshal(b, &sections); err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("sections.yaml: %v", err))
		return map[string]string{}
	}
	return sections
}

func renderIndex(rel string, di *dirInfo, sections map[string]string) string {
	var b strings.Builder
	title := "Knowledge base"
	if rel != "" {
		title = heading(path.Base(rel))
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	b.WriteString("> Generated by `go run ./cmd/knowledge-lint -fix`. Do not edit by hand.\n")
	if rel == "" {
		b.WriteString("> Start with the [README](README.md) for the write protocol and conflict rules.\n")
	}
	b.WriteString("\n")

	if len(di.subdirs) > 0 {
		sort.Strings(di.subdirs)
		sectionTitle := "Sections"
		if rel != "" {
			sectionTitle = "Subdirectories"
		}
		fmt.Fprintf(&b, "## %s\n\n", sectionTitle)
		for _, sub := range di.subdirs {
			name := path.Base(sub)
			desc := sections[sub]
			if desc == "" {
				desc = fmt.Sprintf("Concepts for %s.", name)
			}
			fmt.Fprintf(&b, "- [%s/](%s/index.md) — %s\n", name, name, desc)
		}
		b.WriteString("\n")
	}

	if len(di.concepts) > 0 {
		sort.Slice(di.concepts, func(i, j int) bool {
			left := strings.ToLower(di.concepts[i].Meta.Title)
			right := strings.ToLower(di.concepts[j].Meta.Title)
			if left == right {
				return di.concepts[i].Rel < di.concepts[j].Rel
			}
			return left < right
		})
		b.WriteString("## Concepts\n\n")
		for _, c := range di.concepts {
			fmt.Fprintf(&b, "- [%s](%s) — %s\n", c.Meta.Title, path.Base(c.Rel), c.Meta.Description)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), string(byte(10))) + string(byte(10))
}

func (c *concept) governance() string {
	if c.Meta.Governance != "" {
		return c.Meta.Governance
	}
	if c.Meta.Type == "Invariant" {
		return "constraint"
	}
	return "context"
}

func governanceRank(governance string) int {
	switch governance {
	case "constraint":
		return 0
	case "hold":
		return 1
	default:
		return 2
	}
}

type pathMatch struct {
	governance string
	id         string
	title      string
	score      int
}

func conceptsForPath(concepts []*concept, target string) []pathMatch {
	target = strings.Trim(strings.ReplaceAll(target, "\\", "/"), "/")
	var matches []pathMatch
	for _, c := range concepts {
		score := 0
		for _, ref := range c.Meta.CodeRefs {
			ref = strings.Trim(strings.ReplaceAll(ref, "\\", "/"), "/")
			if ref == "" {
				continue
			}
			clean := path.Clean(ref)
			switch {
			case clean == target:
				score += 4
			case strings.HasPrefix(target, clean+"/"):
				score += 3
			default:
				if ok, _ := path.Match(clean, target); ok {
					score += 3
				}
			}
		}
		if score > 0 {
			matches = append(matches, pathMatch{governance: c.governance(), id: c.ID, title: c.Meta.Title, score: score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if left, right := governanceRank(matches[i].governance), governanceRank(matches[j].governance); left != right {
			return left < right
		}
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].id < matches[j].id
	})
	return matches
}

func runForPath(root, target string) int {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	concepts, errs := loadConcepts(absRoot, filepath.Join(absRoot, knowledgeDir))
	for _, err := range errs {
		fmt.Println("ERROR " + err)
	}
	matches := conceptsForPath(concepts, target)
	if len(matches) == 0 {
		fmt.Printf("no concept governs %s\n", target)
	} else {
		fmt.Printf("%d concept(s) govern %s:\n", len(matches), target)
		for _, match := range matches {
			fmt.Printf("  [%-10s] %-42s %s\n", match.governance, match.id, match.title)
		}
	}
	if len(errs) > 0 {
		return 1
	}
	return 0
}

func lastCommitDate(root, gitPath string) (time.Time, bool) {
	out, err := exec.Command("git", "-C", root, "log", "-1", "--format=%cs", "--", filepath.ToSlash(gitPath)).Output()
	if err != nil {
		return time.Time{}, false
	}
	stamp := strings.TrimSpace(string(out))
	if stamp == "" {
		return time.Time{}, false
	}
	when, err := time.Parse("2006-01-02", stamp)
	if err != nil {
		return time.Time{}, false
	}
	return when, true
}

func isVerificationStale(verifiedAt, lastCommit time.Time) bool {
	return lastCommit.After(verifiedAt)
}

func duplicateTitles(concepts []*concept) []string {
	seen := map[string]string{}
	var warnings []string
	for _, c := range concepts {
		key := strings.ToLower(strings.TrimSpace(c.Meta.Title))
		if key == "" {
			continue
		}
		if previous, ok := seen[key]; ok {
			warnings = append(warnings, fmt.Sprintf("%s: duplicate title also used by %s", c.ID, previous))
			continue
		}
		seen[key] = c.ID
	}
	return warnings
}

func loadQueries(kroot string) ([]queryCase, error) {
	b, err := os.ReadFile(filepath.Join(kroot, "queries.yaml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("queries.yaml: %w", err)
	}
	var queries []queryCase
	if err := yaml.Unmarshal(b, &queries); err != nil {
		return nil, fmt.Errorf("queries.yaml: %w", err)
	}
	for i, query := range queries {
		if strings.TrimSpace(query.Query) == "" {
			return nil, fmt.Errorf("queries.yaml: entry %d has no query", i+1)
		}
		if strings.TrimSpace(query.Expect) == "" {
			return nil, fmt.Errorf("queries.yaml: entry %d has no expect", i+1)
		}
	}
	return queries, nil
}

func runQueries(concepts []*concept, queries []queryCase) []queryResult {
	results := make([]queryResult, 0, len(queries))
	for _, query := range queries {
		tokens := tokenize(query.Query)
		type scored struct {
			id    string
			score int
		}
		ranked := make([]scored, 0, len(concepts))
		for _, c := range concepts {
			ranked = append(ranked, scored{id: c.ID, score: scoreConcept(c, tokens)})
		}
		sort.SliceStable(ranked, func(i, j int) bool {
			if ranked[i].score == ranked[j].score {
				return ranked[i].id < ranked[j].id
			}
			return ranked[i].score > ranked[j].score
		})
		top := make([]string, 0, 3)
		rank := 0
		for i, candidate := range ranked {
			if i >= 3 {
				break
			}
			top = append(top, candidate.id)
			if candidate.id == query.Expect && rank == 0 {
				rank = i + 1
			}
		}
		results = append(results, queryResult{Query: query.Query, Expect: query.Expect, Rank: rank, Top: top})
	}
	return results
}

func scoreConcept(c *concept, tokens []string) int {
	title := tokenSet(c.Meta.Title)
	desc := tokenSet(c.Meta.Description)
	id := tokenSet(c.ID)
	body := tokenSet(c.Body)
	refs := tokenSet(strings.Join(c.Meta.CodeRefs, " "))
	score := 0
	for _, token := range tokens {
		if title[token] {
			score += 6
		}
		if id[token] {
			score += 4
		}
		if desc[token] {
			score += 3
		}
		if refs[token] {
			score += 2
		}
		if body[token] {
			score++
		}
	}
	return score
}

var queryStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "can": true, "do": true, "does": true, "for": true,
	"from": true, "how": true, "i": true, "if": true, "in": true, "is": true,
	"it": true, "of": true, "on": true, "or": true, "should": true, "that": true,
	"the": true, "this": true, "to": true, "use": true, "using": true, "what": true,
	"when": true, "where": true, "which": true, "why": true, "with": true,
}

func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		if len(field) < 2 || queryStopwords[field] || seen[field] {
			continue
		}
		seen[field] = true
		out = append(out, field)
	}
	return out
}

func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, token := range tokenize(s) {
		set[token] = true
	}
	return set
}

func displayDir(rel string) string {
	if rel == "" {
		return knowledgeDir
	}
	return knowledgeDir + "/" + rel
}

func heading(s string) string {
	replacer := strings.NewReplacer("-", " ", "_", " ")
	runes := []rune(replacer.Replace(s))
	if len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func printReport(r report) {
	for _, err := range r.Errors {
		fmt.Println("ERROR " + err)
	}
	for _, warn := range r.Warnings {
		fmt.Println("WARN  " + warn)
	}
	if r.IndexesWritten > 0 {
		fmt.Printf("wrote %d index file(s)\n", r.IndexesWritten)
	}
	if r.QueriesChecked > 0 {
		fmt.Printf("queries: %d/%d pass, %d top-1\n", r.QueriesPassed, r.QueriesChecked, r.QueriesTop1)
	}
	if r.TrustHuman+r.TrustMachine > 0 {
		fmt.Printf("trust: %d machine-confirmed, %d human-reviewed\n", r.TrustMachine, r.TrustHuman)
	}
	if len(r.Errors) == 0 {
		fmt.Printf("OK: %d concept(s), indexes current\n", r.Concepts)
	}
}
