package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autoresume/models"
)

func TestCleanDataEscapesLatexSpecials(t *testing.T) {
	cases := map[string]string{
		"R&D":        `R\&D`,
		"100% done":  `100\% done`,
		"cost $5":    `cost \$5`,
		"tag #1":     `tag \#1`,
		"snake_case": `snake\_case`,
		"a{b}":       `a\{b\}`,
		"~approx":    `\textasciitilde{}approx`,
		"x^2":        `x\textasciicircum{}2`,
		`back\slash`: `back\textbackslash{}slash`,
	}
	for in, want := range cases {
		if got := cleanData(in); got != want {
			t.Errorf("cleanData(%q) = %q, want %q", in, got, want)
		}
	}
}

// resume.cls loads article with no inputenc, so anything above U+007F
// reaching pdflatex is a build failure or a wrong glyph.
func TestCleanDataFoldsToASCII(t *testing.T) {
	in := "dates — scattered – across “portals” with ‘quotes’ and 5★ …"
	got := cleanData(in)
	for _, r := range got {
		if r > 127 {
			t.Fatalf("cleanData left non-ASCII %q (U+%04X) in %q", r, r, got)
		}
	}
	if !strings.Contains(got, "---") {
		t.Errorf("em dash should fold to ---, got %q", got)
	}
}

func TestBulletBoldsQuotedSpans(t *testing.T) {
	got := bullet(`shipped "194" exams`)
	if want := `shipped \textbf{194} exams`; got != want {
		t.Errorf("bullet() = %q, want %q", got, want)
	}
}

// The bug that motivates this: examdb is a private repo. Linking it to
// github.com renders fine for the owner and 404s for every reader, so an
// explicit url must always beat the API-supplied one.
func TestExplicitURLBeatsGithubURL(t *testing.T) {
	cfg := &Config{Projects: []Project{
		{Name: "ExamDB", Repo: "examdb", URL: "https://examdb.org"},
		{Name: "TYPE", Repo: "type"},
	}}
	idx := map[string]repoStats{
		"examdb": {url: "https://github.com/Pranjal-SB/examdb", stars: 0},
		"type":   {url: "https://github.com/Pranjal-SB/type", stars: 1},
	}

	got := renderProjects(cfg, idx)
	if strings.Contains(got, "github.com/Pranjal-SB/examdb") {
		t.Error("private repo linked to github.com; explicit url was ignored")
	}
	if !strings.Contains(got, "https://examdb.org") {
		t.Error("explicit url missing from output")
	}
	// No override, so TYPE should still fall back to the GitHub url.
	if !strings.Contains(got, "github.com/Pranjal-SB/type") {
		t.Error("project without an explicit url lost its GitHub fallback")
	}
}

func TestStarsOnlyWhenAskedAndNonZero(t *testing.T) {
	idx := map[string]repoStats{"a": {stars: 7}, "b": {stars: 0}}

	withStars := renderProjects(&Config{Projects: []Project{
		{Name: "A", Repo: "a", URL: "u", ShowStars: true},
	}}, idx)
	if !strings.Contains(withStars, `7\(\star\)`) {
		t.Errorf("expected star count, got %q", withStars)
	}

	zero := renderProjects(&Config{Projects: []Project{
		{Name: "B", Repo: "b", URL: "u", ShowStars: true},
	}}, idx)
	if strings.Contains(zero, `\star`) {
		t.Error("rendered a 0-star badge")
	}

	off := renderProjects(&Config{Projects: []Project{
		{Name: "A", Repo: "a", URL: "u", ShowStars: false},
	}}, idx)
	if strings.Contains(off, `\star`) {
		t.Error("rendered stars with show_stars false")
	}
}

// An empty grade previously rendered as a dangling "CGPA:" label.
func TestEmptyGradeOmitsLabel(t *testing.T) {
	got := renderEducation(&Config{Education: []Education{
		{School: "S", Degree: "D", Start: "2025", End: "2030"},
	}})
	if strings.Contains(got, "CGPA") {
		t.Errorf("empty grade should omit the label, got %q", got)
	}

	withGrade := renderEducation(&Config{Education: []Education{
		{School: "S", Degree: "D", Grade: "9.1"},
	}})
	if !strings.Contains(withGrade, "CGPA: 9.1") {
		t.Errorf("grade missing, got %q", withGrade)
	}
}

func TestSkillsOrderIsStable(t *testing.T) {
	cfg := &Config{
		Skills:      map[string]string{"Languages": "Go", "Backend": "FastAPI", "Frontend": "React"},
		SkillsOrder: []string{"Languages", "Backend", "Frontend"},
	}
	// Go map iteration is randomised, so a single pass proves nothing.
	first := renderSkills(cfg)
	for i := 0; i < 50; i++ {
		if got := renderSkills(cfg); got != first {
			t.Fatalf("skills order unstable on pass %d:\n%q\nvs\n%q", i, got, first)
		}
	}
	if !strings.HasPrefix(first, "Languages") {
		t.Errorf("skills_order ignored, got %q", first)
	}
}

// A resume shipping the literal text "<PHONE>" is worse than a failed build.
func TestRenderRejectsUnresolvedPlaceholder(t *testing.T) {
	dir := t.TempDir()
	tmpl := filepath.Join(dir, "t.tex")
	if err := os.WriteFile(tmpl, []byte(`\name{<NAME>} <TOTALLY_UNKNOWN>`), 0644); err != nil {
		t.Fatal(err)
	}

	err := Render(tmpl, filepath.Join(dir, "out.tex"), &Config{Name: "X"}, nil)
	if err == nil {
		t.Fatal("expected an error for the unresolved placeholder")
	}
	if !strings.Contains(err.Error(), "TOTALLY_UNKNOWN") {
		t.Errorf("error should name the placeholder, got %v", err)
	}
}

func TestConfigValidation(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "c.yaml")
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if _, err := LoadConfig(write("name: X\n")); err == nil {
		t.Error("missing email should fail (GitHub profile email is often private)")
	}
	if _, err := LoadConfig(write("name: X\nemail: a@b.c\nprojects:\n  - name: P\n")); err == nil {
		t.Error("project with neither url nor repo should fail")
	}
	if _, err := LoadConfig(write("name: X\nemail: a@b.c\nskills_order: [Nope]\n")); err == nil {
		t.Error("skills_order naming an absent key should fail")
	}
	// A typo in a key must not silently render an empty section.
	if _, err := LoadConfig(write("name: X\nemail: a@b.c\nexperiance: []\n")); err == nil {
		t.Error("unknown field should fail")
	}
	if _, err := LoadConfig(write("name: X\nemail: a@b.c\n")); err != nil {
		t.Errorf("minimal valid config rejected: %v", err)
	}
}

// YAML is authoritative; a scraped profile must never overwrite it.
func TestMergeLinkedinNeverOverwritesYAML(t *testing.T) {
	cfg := &Config{
		Experience: []Job{{Title: "Associate", Company: "Next Tech Lab"}},
	}
	li := &models.LinkedinProfile{
		Position: []models.Experience{{Title: "Scraped", CompanyName: "Wrong"}},
	}
	MergeLinkedin(cfg, li)

	if len(cfg.Experience) != 1 || cfg.Experience[0].Title != "Associate" {
		t.Errorf("LinkedIn overwrote hand-written experience: %+v", cfg.Experience)
	}
}

// Upstream filtered positions by /intern/, which drops "Associate" and every
// other non-intern title without warning.
func TestMergeLinkedinKeepsNonInternTitles(t *testing.T) {
	cfg := &Config{}
	li := &models.LinkedinProfile{
		Position: []models.Experience{{Title: "Associate", CompanyName: "Next Tech Lab"}},
	}
	MergeLinkedin(cfg, li)

	if len(cfg.Experience) != 1 {
		t.Fatalf("expected the Associate role to survive, got %+v", cfg.Experience)
	}
	if cfg.Experience[0].End != "Present" {
		t.Errorf("open-ended role should render as Present, got %q", cfg.Experience[0].End)
	}
}

func TestMergeLinkedinNilIsSafe(t *testing.T) {
	cfg := &Config{}
	if added := MergeLinkedin(cfg, nil); added != nil {
		t.Errorf("nil profile should be a no-op, got %v", added)
	}
}
