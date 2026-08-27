package main

import (
	"autoresume/errors"
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the hand-edited source of truth for everything the GitHub API
// cannot supply: bullets, dates, contact details, section ordering.
type Config struct {
	Name     string `yaml:"name"`
	Location string `yaml:"location"`
	Email    string `yaml:"email"`
	Phone    string `yaml:"phone"`

	Links struct {
		LinkedIn string `yaml:"linkedin"`
		GitHub   string `yaml:"github"`
		Site     string `yaml:"site"`
	} `yaml:"links"`

	Education  []Education `yaml:"education"`
	Experience []Job       `yaml:"experience"`
	Projects   []Project   `yaml:"projects"`

	// Skills renders as a two-column table; Order fixes the row sequence
	// because Go map iteration is randomised.
	Skills      map[string]string `yaml:"skills"`
	SkillsOrder []string          `yaml:"skills_order"`

	Certifications []string `yaml:"certifications"`
	Languages      []string `yaml:"languages"`
}

type Education struct {
	School string `yaml:"school"`
	Degree string `yaml:"degree"`
	Field  string `yaml:"field"`
	Start  string `yaml:"start"`
	End    string `yaml:"end"`
	Grade  string `yaml:"grade"`
	URL    string `yaml:"url"`
}

type Job struct {
	Title    string   `yaml:"title"`
	Company  string   `yaml:"company"`
	Location string   `yaml:"location"`
	Start    string   `yaml:"start"`
	End      string   `yaml:"end"`
	Bullets  []string `yaml:"bullets"`
}

type Project struct {
	Name string `yaml:"name"`
	// Repo is the GitHub repo name used to look up live star counts and
	// languages. Leave blank for projects that have no repo.
	Repo string `yaml:"repo"`
	// URL is what the resume actually links to. Set it explicitly for
	// private repos — a github.com link to a private repo 404s for every
	// reader, and looks fine to you because you are signed in.
	URL     string   `yaml:"url"`
	Stack   string   `yaml:"stack"`
	Bullets []string `yaml:"bullets"`
	// ShowStars appends the live stargazer count when the repo has any.
	ShowStars bool `yaml:"show_stars"`
}

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.FileOperationError{Message: fmt.Sprintf("Failed to read config %s: %v", path, err)}
	}

	var cfg Config
	// KnownFields makes a typo in resume.yaml an error instead of a
	// silently empty section.
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, errors.DataParsingError{Message: fmt.Sprintf("Failed to parse %s: %v", path, err)}
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Name == "" {
		return errors.DataParsingError{Message: "config: name is required"}
	}
	if c.Email == "" {
		return errors.DataParsingError{Message: "config: email is required (GitHub profile email is often private and renders blank)"}
	}
	for i, p := range c.Projects {
		if p.Name == "" {
			return errors.DataParsingError{Message: fmt.Sprintf("config: projects[%d] has no name", i)}
		}
		if p.URL == "" && p.Repo == "" {
			return errors.DataParsingError{Message: fmt.Sprintf("config: project %q needs a url or a repo", p.Name)}
		}
	}
	for _, k := range c.SkillsOrder {
		if _, ok := c.Skills[k]; !ok {
			return errors.DataParsingError{Message: fmt.Sprintf("config: skills_order names %q which is not in skills", k)}
		}
	}
	return nil
}
