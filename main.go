package main

import (
	"autoresume/errors"
	"autoresume/models"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/valyala/fasthttp"
)

const (
	GithubApiUrl   = "https://api.github.com/graphql"
	ConfigFile     = "resume.yaml"
	TemplateFile   = "misc/template.tex"
	OutputFile     = "resume.tex"
	GithubDataFile = "github_data.json"
)

// githubQuery pulls the live half of the resume: which repos exist, their
// stars, and the language spread across all of them.
const githubQuery = `
{
  viewer {
    login
    name
    location
    websiteUrl
    repositories(first: 100, ownerAffiliations: OWNER, orderBy: {field: STARGAZERS, direction: DESC}) {
      nodes {
        name
        url
        stargazerCount
        languages(first: 10) { nodes { name } }
      }
    }
  }
}`

// cacheEnabled writes API responses to disk and reuses them on the next run.
// Off by default so CI always fetches fresh; set LOCAL=true while iterating
// on the template to avoid burning API calls.
func cacheEnabled() bool {
	v := os.Getenv("LOCAL")
	return v == "true" || v == "True" || v == "1"
}

func fileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func fetchGithubData() (*models.GithubResponse, error) {
	var data models.GithubResponse

	if cacheEnabled() && fileExists(GithubDataFile) {
		raw, err := os.ReadFile(GithubDataFile)
		if err != nil {
			return nil, errors.FileOperationError{Message: fmt.Sprintf("Failed to read GitHub cache: %v", err)}
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, errors.DataParsingError{Message: fmt.Sprintf("Failed to parse GitHub cache: %v", err)}
		}
		fmt.Println("github: using cached response")
		return &data, nil
	}

	token := os.Getenv("TOKEN")
	if token == "" {
		return nil, errors.ApiError{Message: "TOKEN is not set (needs a GitHub PAT with read:user, plus repo to see private repos)"}
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(GithubApiUrl)
	req.Header.SetMethod("POST")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.SetContentType("application/json")

	body, err := json.Marshal(map[string]string{"query": githubQuery})
	if err != nil {
		return nil, errors.DataParsingError{Message: fmt.Sprintf("Failed to build GitHub query: %v", err)}
	}
	req.SetBody(body)

	client := &fasthttp.Client{ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	if err := client.Do(req, resp); err != nil {
		return nil, errors.ApiError{Message: fmt.Sprintf("GitHub request failed: %v", err)}
	}
	if resp.StatusCode() != fasthttp.StatusOK {
		return nil, errors.ApiError{Message: fmt.Sprintf("GitHub returned %d: %s", resp.StatusCode(), resp.Body())}
	}

	var parsed struct {
		Data   models.GithubResponse `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, errors.DataParsingError{Message: fmt.Sprintf("Failed to parse GitHub response: %v", err)}
	}
	// GraphQL reports scope problems in a 200 body, so this has to be
	// checked explicitly or a missing scope looks like an empty profile.
	if len(parsed.Errors) > 0 {
		msgs := make([]string, len(parsed.Errors))
		for i, e := range parsed.Errors {
			msgs[i] = e.Message
		}
		return nil, errors.ApiError{Message: fmt.Sprintf("GitHub GraphQL errors: %v", msgs)}
	}

	data = parsed.Data

	if cacheEnabled() {
		if cache, err := json.MarshalIndent(data, "", "  "); err == nil {
			_ = os.WriteFile(GithubDataFile, cache, 0644)
		}
	}
	return &data, nil
}

func main() {
	_ = godotenv.Load()

	cfg, err := LoadConfig(ConfigFile)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	gh, err := fetchGithubData()
	if err != nil {
		log.Fatalf("github: %v", err)
	}

	fmt.Printf("github: %s, %d repos visible\n",
		gh.Viewer.Login, len(gh.Viewer.Repositories.Nodes))

	// Optional. A LinkedIn failure is reported and ignored — resume.yaml is
	// authoritative, so a dead scraper must not cost you a resume.
	if li, err := fetchLinkedin(cfg.Links.LinkedIn); err != nil {
		fmt.Printf("warn: linkedin skipped: %v\n", err)
	} else if li == nil {
		fmt.Println("linkedin: not configured, using resume.yaml only")
	} else if added := MergeLinkedin(cfg, li); len(added) > 0 {
		fmt.Printf("linkedin: filled empty sections with %s\n", strings.Join(added, ", "))
	} else {
		fmt.Println("linkedin: fetched, but resume.yaml already covers every section")
	}

	// Warn on YAML projects whose repo never came back — usually a typo or a
	// token missing the repo scope. Not fatal: a project may have no repo.
	idx := indexRepos(gh)
	for _, p := range cfg.Projects {
		if p.Repo == "" {
			continue
		}
		if _, ok := idx[strings.ToLower(p.Repo)]; !ok {
			fmt.Printf("warn: project %q references repo %q which the token cannot see\n", p.Name, p.Repo)
		}
	}

	if err := Render(TemplateFile, OutputFile, cfg, gh); err != nil {
		log.Fatalf("render: %v", err)
	}

	// Variant resumes: same content, projects filtered to a tag.
	for outFile, v := range cfg.Variants {
		sub := *cfg
		sub.Projects = filterByTag(cfg.Projects, v.Tag)
		sub.Contributions = filterByTag(cfg.Contributions, v.Tag)
		if len(sub.Projects) == 0 {
			fmt.Printf("warn: variant %q (tag %q) has no tagged projects, skipping\n", outFile, v.Tag)
			continue
		}
		if err := Render(TemplateFile, outFile, &sub, gh); err != nil {
			log.Fatalf("render %s: %v", outFile, err)
		}
	}
}

// filterByTag keeps the projects tagged with tag. Untagged projects belong to
// the default resume only, so they are excluded from a focused variant.
func filterByTag(items []Project, tag string) []Project {
	var out []Project
	for _, p := range items {
		for _, t := range p.Tags {
			if t == tag {
				out = append(out, p)
				break
			}
		}
	}
	return out
}
