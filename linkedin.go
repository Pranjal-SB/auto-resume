package main

import (
	"autoresume/errors"
	"autoresume/models"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
)

const (
	LinkedinApiUrl   = "https://professional-network-data.p.rapidapi.com/get-profile-data-by-url"
	LinkedinDataFile = "linkedin_data.json"
)

var months = [...]string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

func monthAbbr(m int) string {
	if m < 1 || m > 12 {
		return ""
	}
	return months[m]
}

func ym(month, year int) string {
	if year == 0 {
		return ""
	}
	if a := monthAbbr(month); a != "" {
		return fmt.Sprintf("%s %d", a, year)
	}
	return fmt.Sprintf("%d", year)
}

// fetchLinkedin is opt-in. Unlike upstream this never aborts the run: the
// resume is fully renderable from resume.yaml alone, so a missing key or a
// dead scraper endpoint degrades to "no extra data" rather than no resume.
func fetchLinkedin(profileURL string) (*models.LinkedinProfile, error) {
	var data models.LinkedinProfile

	if cacheEnabled() && fileExists(LinkedinDataFile) {
		raw, err := os.ReadFile(LinkedinDataFile)
		if err != nil {
			return nil, errors.FileOperationError{Message: fmt.Sprintf("Failed to read LinkedIn cache: %v", err)}
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, errors.DataParsingError{Message: fmt.Sprintf("Failed to parse LinkedIn cache: %v", err)}
		}
		fmt.Println("linkedin: using cached response")
		return &data, nil
	}

	key := os.Getenv("LINKEDIN_API_KEY")
	if key == "" {
		return nil, nil // not configured; YAML covers everything
	}
	if profileURL == "" {
		return nil, errors.ApiError{Message: "LINKEDIN_API_KEY is set but links.linkedin is empty in resume.yaml"}
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(fmt.Sprintf("%s?url=https://%s", LinkedinApiUrl, strings.TrimPrefix(profileURL, "https://")))
	req.Header.SetMethod("GET")
	req.Header.Set("x-rapidapi-key", key)
	req.Header.Set("x-rapidapi-host", "professional-network-data.p.rapidapi.com")

	client := &fasthttp.Client{ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	if err := client.Do(req, resp); err != nil {
		return nil, errors.ApiError{Message: fmt.Sprintf("LinkedIn request failed: %v", err)}
	}
	if resp.StatusCode() != fasthttp.StatusOK {
		return nil, errors.ApiError{Message: fmt.Sprintf("LinkedIn returned %d", resp.StatusCode())}
	}
	if err := json.Unmarshal(resp.Body(), &data); err != nil {
		return nil, errors.DataParsingError{Message: fmt.Sprintf("Failed to parse LinkedIn response: %v", err)}
	}

	if cacheEnabled() {
		if cache, err := json.MarshalIndent(data, "", "  "); err == nil {
			_ = os.WriteFile(LinkedinDataFile, cache, 0644)
		}
	}
	return &data, nil
}

// MergeLinkedin fills only the fields resume.yaml left empty. YAML always
// wins, so nothing you hand-wrote is ever overwritten by scraped data.
//
// Upstream filtered positions to titles matching /intern/, which silently
// drops every other job title. This takes them all and lets YAML override.
func MergeLinkedin(cfg *Config, li *models.LinkedinProfile) []string {
	if li == nil {
		return nil
	}
	var added []string

	if len(cfg.Experience) == 0 {
		for _, p := range li.Position {
			end := ym(p.End.Month, p.End.Year)
			if end == "" {
				end = "Present"
			}
			cfg.Experience = append(cfg.Experience, Job{
				Title:    p.Title,
				Company:  p.CompanyName,
				Location: p.Location,
				Start:    ym(p.Start.Month, p.Start.Year),
				End:      end,
			})
		}
		if len(li.Position) > 0 {
			added = append(added, fmt.Sprintf("%d position(s)", len(li.Position)))
		}
	}

	if len(cfg.Education) == 0 {
		for _, e := range li.Educations {
			degree := e.Degree
			// LinkedIn stores "Master of Technology - MTech (Integrated) ..."
			// so the useful half is after the separator when there is one.
			if parts := strings.SplitN(e.Degree, " - ", 2); len(parts) == 2 {
				degree = parts[1]
			}
			cfg.Education = append(cfg.Education, Education{
				School: strings.SplitN(e.SchoolName, " (", 2)[0],
				Degree: degree,
				Field:  e.FieldOfStudy,
				Start:  ym(0, e.Start.Year),
				End:    ym(0, e.End.Year),
				Grade:  e.Grade,
				URL:    e.Url,
			})
		}
		if len(li.Educations) > 0 {
			added = append(added, fmt.Sprintf("%d education entr(ies)", len(li.Educations)))
		}
	}

	if len(cfg.Certifications) == 0 {
		for _, c := range li.Certifications {
			cfg.Certifications = append(cfg.Certifications, c.Name)
		}
		if len(li.Certifications) > 0 {
			added = append(added, fmt.Sprintf("%d certification(s)", len(li.Certifications)))
		}
	}

	if len(cfg.Languages) == 0 {
		for _, l := range li.Languages {
			cfg.Languages = append(cfg.Languages,
				fmt.Sprintf("%s (%s)", l.Name, strings.ReplaceAll(l.Proficiency, "_", " ")))
		}
		if len(li.Languages) > 0 {
			added = append(added, fmt.Sprintf("%d language(s)", len(li.Languages)))
		}
	}

	return added
}
