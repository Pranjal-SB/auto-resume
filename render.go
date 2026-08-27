package main

import (
	"autoresume/errors"
	"autoresume/models"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// quoted marks "..." spans as bold, matching the upstream convention for
// calling out metrics inside a bullet.
var quoted = regexp.MustCompile(`"([^"]+)"`)

// unicodeFold maps the punctuation that word processors and web pages inject
// onto plain ASCII. resume.cls loads article with no inputenc and no fontenc,
// so a stray em-dash or curly quote either breaks the build or renders as the
// wrong glyph. Folding here means the template never has to care.
var unicodeFold = strings.NewReplacer(
	"—", "---", // em dash
	"–", "--", // en dash
	"‘", "`", // left single quote
	"’", "'", // right single quote / apostrophe
	"“", "``", // left double quote
	"”", "''", // right double quote
	"…", `\ldots{}`, // ellipsis
	" ", " ", // non-breaking space
	"•", "", // bullet
	"×", `\(\times\)`,
	"→", `\(\rightarrow\)`,
	"≤", `\(\leq\)`,
	"≥", `\(\geq\)`,
	"±", `\(\pm\)`,
	"★", `\(\star\)`,
	"✓", "",
)

// cleanData escapes the LaTeX specials that show up in ordinary prose.
// Order matters: the backslash must be escaped before anything that
// introduces one, so folding runs after escaping.
func cleanData(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	r := strings.NewReplacer(
		`\`, `\textbackslash{}`,
		`&`, `\&`,
		`%`, `\%`,
		`$`, `\$`,
		`#`, `\#`,
		`_`, `\_`,
		`{`, `\{`,
		`}`, `\}`,
		`~`, `\textasciitilde{}`,
		`^`, `\textasciicircum{}`,
	)
	return unicodeFold.Replace(r.Replace(s))
}

// assertASCII is the backstop: anything the fold table missed would reach
// pdflatex, and a LaTeX encoding failure in CI is far harder to read than
// this message.
func assertASCII(what, s string) error {
	for i, r := range s {
		if r > 127 {
			return errors.DataParsingError{Message: fmt.Sprintf(
				"%s contains non-ASCII %q (U+%04X) at byte %d; add it to unicodeFold in render.go",
				what, r, r, i)}
		}
	}
	return nil
}

// bullet escapes a bullet then re-applies the "..." bold convention.
func bullet(s string) string {
	out := cleanData(strings.TrimSpace(s))
	return quoted.ReplaceAllString(out, `\textbf{$1}`)
}

func itemize(bullets []string) []string {
	if len(bullets) == 0 {
		return nil
	}
	out := []string{`\begin{itemize}`, `\itemsep -3pt{}`}
	for _, b := range bullets {
		if strings.TrimSpace(b) == "" {
			continue
		}
		out = append(out, fmt.Sprintf(`\item %s`, bullet(b)))
	}
	return append(out, `\end{itemize}`)
}

// repoStats indexes the live GitHub data by repo name so YAML entries can
// pull star counts without a second API call.
type repoStats struct {
	stars int
	url   string
}

func indexRepos(gh *models.GithubResponse) map[string]repoStats {
	idx := make(map[string]repoStats)
	if gh == nil {
		return idx
	}
	for _, r := range gh.Viewer.Repositories.Nodes {
		idx[strings.ToLower(r.Name)] = repoStats{stars: r.StargazerCount, url: r.Url}
	}
	return idx
}

func renderProjects(cfg *Config, idx map[string]repoStats) string {
	var entries []string

	for _, p := range cfg.Projects {
		stats, found := idx[strings.ToLower(p.Repo)]

		// An explicit url always wins. Falling back to the GitHub url is
		// only safe when the repo actually resolved.
		url := p.URL
		if url == "" && found {
			url = stats.url
		}

		heading := fmt.Sprintf(`\textbf{\href{%s}{%s}}`, url, cleanData(p.Name))
		if p.Stack != "" {
			heading += fmt.Sprintf(` \(\mid\) \textbf{%s}`, cleanData(p.Stack))
		}
		if p.ShowStars && found && stats.stars > 0 {
			heading += fmt.Sprintf(` \(\mid\) \textbf{%d\(\star\)}`, stats.stars)
		}

		entry := append([]string{heading}, itemize(p.Bullets)...)
		entries = append(entries, strings.Join(entry, "\n"))
	}

	return strings.Join(entries, "\n\n")
}

func renderExperience(cfg *Config) string {
	var entries []string
	for _, j := range cfg.Experience {
		entry := []string{
			fmt.Sprintf(`\textbf{%s} \hfill %s - %s\\`, cleanData(j.Title), cleanData(j.Start), cleanData(j.End)),
			fmt.Sprintf(`%s \hfill \textit{%s}`, cleanData(j.Company), cleanData(j.Location)),
		}
		entry = append(entry, itemize(j.Bullets)...)
		entries = append(entries, strings.Join(entry, "\n"))
	}
	return strings.Join(entries, "\n\n")
}

func renderEducation(cfg *Config) string {
	var entries []string
	for _, e := range cfg.Education {
		span := e.End
		if e.Start != "" {
			span = fmt.Sprintf("%s - %s", e.Start, e.End)
		}

		head := cleanData(e.School)
		if e.URL != "" {
			head = fmt.Sprintf(`\href{%s}{%s}`, e.URL, head)
		}

		line := fmt.Sprintf(`\textbf{%s} %s`, cleanData(e.Degree), cleanData(e.Field))
		// Only emit the grade when there is one; an empty CGPA renders as a
		// dangling label.
		if e.Grade != "" {
			line += fmt.Sprintf(` \hfill \textit{CGPA: %s}`, cleanData(e.Grade))
		}

		entries = append(entries, strings.Join([]string{
			fmt.Sprintf(`%s \hfill %s\\`, head, cleanData(span)),
			line,
		}, "\n"))
	}
	return strings.Join(entries, "\n\n")
}

func renderSkills(cfg *Config) string {
	keys := cfg.SkillsOrder
	if len(keys) == 0 {
		for k := range cfg.Skills {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}

	var rows []string
	for _, k := range keys {
		rows = append(rows, fmt.Sprintf(`%s & %s`, cleanData(k), cleanData(cfg.Skills[k])))
	}
	return strings.Join(rows, `\\`+"\n")
}

// renderContact joins only the contact fields that are filled. Building the
// line in the template instead would leave a dangling "\\" separator for
// every field left blank.
func renderContact(cfg *Config) string {
	var parts []string

	if cfg.Email != "" {
		parts = append(parts, fmt.Sprintf(`\href{mailto:%s}{%s}`, cfg.Email, cleanData(cfg.Email)))
	}
	if cfg.Phone != "" {
		// tel: wants no spaces; the visible label keeps them.
		tel := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(cfg.Phone)
		parts = append(parts, fmt.Sprintf(`\href{tel:%s}{%s}`, tel, cleanData(cfg.Phone)))
	}
	for _, l := range []string{cfg.Links.LinkedIn, cfg.Links.GitHub, cfg.Links.Site} {
		if l == "" {
			continue
		}
		bare := strings.TrimPrefix(strings.TrimPrefix(l, "https://"), "http://")
		parts = append(parts, fmt.Sprintf(`\href{https://%s}{%s}`, bare, cleanData(bare)))
	}

	return strings.Join(parts, ` \\ `)
}

// githubLanguages is the one genuinely live field: every language across
// every repo the token can see, deduped.
func githubLanguages(gh *models.GithubResponse) string {
	if gh == nil {
		return ""
	}
	seen := make(map[string]bool)
	var langs []string
	for _, r := range gh.Viewer.Repositories.Nodes {
		for _, l := range r.Languages.Nodes {
			if !seen[l.Name] {
				seen[l.Name] = true
				langs = append(langs, l.Name)
			}
		}
	}
	sort.Strings(langs)
	return strings.Join(langs, ", ")
}

func Render(templateFile, outputFile string, cfg *Config, gh *models.GithubResponse) error {
	raw, err := os.ReadFile(templateFile)
	if err != nil {
		return errors.FileOperationError{Message: fmt.Sprintf("Failed to read template %s: %v", templateFile, err)}
	}

	idx := indexRepos(gh)

	repl := map[string]string{
		"<NAME>":           cleanData(cfg.Name),
		"<LOCATION>":       cleanData(cfg.Location),
		"<CONTACT>":        renderContact(cfg),
		"<EXPERIENCES>":    renderExperience(cfg),
		"<REPOSITORIES>":   renderProjects(cfg, idx),
		"<EDUCATION>":      renderEducation(cfg),
		"<SKILLS>":         renderSkills(cfg),
		"<CERTIFICATIONS>": cleanData(strings.Join(cfg.Certifications, ", ")),
		"<SPEAKS>":         cleanData(strings.Join(cfg.Languages, ", ")),
		"<GITHUB_LANGS>":   cleanData(githubLanguages(gh)),
	}

	content := string(raw)
	for k, v := range repl {
		content = strings.ReplaceAll(content, k, v)
	}

	// A surviving placeholder means the template and the renderer have
	// drifted apart; better to fail than to ship "<PHONE>" on a resume.
	if leftover := regexp.MustCompile(`<[A-Z_]+>`).FindAllString(content, -1); len(leftover) > 0 {
		return errors.DataParsingError{
			Message: fmt.Sprintf("template %s has unresolved placeholders: %s",
				templateFile, strings.Join(leftover, ", ")),
		}
	}

	if err := assertASCII(outputFile, content); err != nil {
		return err
	}

	if err := os.WriteFile(outputFile, []byte(content), 0644); err != nil {
		return errors.FileOperationError{Message: fmt.Sprintf("Failed to write %s: %v", outputFile, err)}
	}
	fmt.Printf("rendered %s\n", outputFile)
	return nil
}
