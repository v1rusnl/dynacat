package dynacat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const dynawidgetsDefaultRepo = "main"
const dynawidgetsAssetsDir = "/app/assets/dynawidgets"

type dynawidgetsWidget struct {
	widgetBase        `yaml:",inline"`
	Widget            string                       `yaml:"widget"`
	Repo              string                       `yaml:"repo"`
	*CustomAPIRequest `yaml:",inline"`
	Subrequests       map[string]*CustomAPIRequest  `yaml:"subrequests"`
	Options           customAPIOptions              `yaml:"options"`
	Frameless         bool                          `yaml:"frameless"`
	compiledTemplate  *template.Template            `yaml:"-"`
	CompiledHTML      template.HTML                 `yaml:"-"`
}

type dynawidgetsListEntry struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Slug        string `json:"slug"`
	Template    string `json:"template"`
}

type dynawidgetsRequired struct {
	URL string `yaml:"url"`
}

func (widget *dynawidgetsWidget) initialize() error {
	widget.withTitle("Dynawidgets").withCacheDuration(1 * time.Minute)

	if widget.Widget == "" {
		return errors.New("widget (slug) is required")
	}

	slug := strings.ToLower(widget.Widget)
	repo := widget.Repo
	if repo == "" {
		repo = dynawidgetsDefaultRepo
	}
	templateContent, title, required, err := dynawidgetsResolveTemplate(slug, repo)
	if err != nil {
		return fmt.Errorf("resolving dynawidget template: %w", err)
	}

	if widget.Title == "" && title != "" {
		widget.Title = title
	}

	// Apply required defaults if user hasn't specified them
	if required != nil && required.URL != "" {
		if widget.CustomAPIRequest == nil {
			widget.CustomAPIRequest = &CustomAPIRequest{}
		}
		if widget.CustomAPIRequest.URL == "" {
			widget.CustomAPIRequest.URL = required.URL
		}
	}

	compiledTemplate, err := template.New("").Funcs(customAPITemplateFuncs).Parse(templateContent)
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}
	widget.compiledTemplate = compiledTemplate

	if widget.CustomAPIRequest != nil {
		if err := widget.CustomAPIRequest.initialize(); err != nil {
			return fmt.Errorf("initializing primary request: %v", err)
		}
	}

	for key := range widget.Subrequests {
		if err := widget.Subrequests[key].initialize(); err != nil {
			return fmt.Errorf("initializing subrequest %q: %v", key, err)
		}
	}

	if widget.UpdateInterval == nil {
		interval := updateIntervalField(10 * time.Second)
		widget.UpdateInterval = &interval
	}

	if *widget.UpdateInterval <= 0 {
		return errors.New("update-interval must be greater than 0")
	}

	return nil
}

func (widget *dynawidgetsWidget) update(ctx context.Context) {
	compiledHTML, err := fetchAndRenderCustomAPIRequest(
		widget.CustomAPIRequest, widget.Subrequests, widget.Options, widget.compiledTemplate,
	)
	if !widget.canContinueUpdateAfterHandlingErr(err) {
		return
	}

	widget.CompiledHTML = compiledHTML
}

func (widget *dynawidgetsWidget) Render() template.HTML {
	return widget.renderTemplate(widget, customAPIWidgetTemplate)
}

// dynawidgetsParseTemplate splits a template file into the template content
// and the required section. The required section starts with "required: |"
// on its own line at the bottom of the file.
func dynawidgetsParseTemplate(raw string) (templateContent string, required *dynawidgetsRequired) {
	const separator = "required: |"

	idx := strings.LastIndex(raw, separator)
	if idx == -1 {
		return raw, nil
	}

	templateContent = strings.TrimRight(raw[:idx], "\n\r ")
	requiredRaw := strings.TrimSpace(raw[idx+len(separator):])

	if requiredRaw == "" {
		return templateContent, nil
	}

	required = &dynawidgetsRequired{}
	if err := yaml.Unmarshal([]byte(requiredRaw), required); err != nil {
		slog.Error("Failed to parse dynawidget required section", "error", err)
		return templateContent, nil
	}

	return templateContent, required
}

// dynawidgetsResolveTemplate checks for a cached template on disk, or fetches
// it from the dynawidgets repository. Returns the template content, the
// widget title (empty if loaded from cache), and parsed required config.
func dynawidgetsResolveTemplate(slug string, repo string) (templateContent string, title string, required *dynawidgetsRequired, err error) {
	templatePath := filepath.Join(dynawidgetsAssetsDir, slug+".txt")

	// Check if template already exists on disk
	if data, readErr := os.ReadFile(templatePath); readErr == nil {
		slog.Info("Using cached dynawidget template", "slug", slug, "path", templatePath)
		content, req := dynawidgetsParseTemplate(string(data))
		return content, "", req, nil
	}

	// Fetch the list index for the first letter of the slug
	firstLetter := string(slug[0])
	baseURL := fmt.Sprintf("https://raw.githubusercontent.com/Panonim/dynawidgets/refs/heads/%s", repo)
	listURL := fmt.Sprintf("%s/database/list-%s.json", baseURL, firstLetter)

	slog.Info("Fetching dynawidgets list", "url", listURL)

	resp, err := defaultHTTPClient.Get(listURL)
	if err != nil {
		return "", "", nil, fmt.Errorf("fetching widget list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", nil, fmt.Errorf("fetching widget list: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	var entries []dynawidgetsListEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return "", "", nil, fmt.Errorf("decoding widget list: %w", err)
	}

	// Find the matching slug
	var entry *dynawidgetsListEntry
	for i := range entries {
		if entries[i].Slug == slug {
			entry = &entries[i]
			break
		}
	}

	if entry == nil {
		return "", "", nil, fmt.Errorf("widget %q not found in dynawidgets list", slug)
	}

	// Fetch the template content
	slog.Info("Fetching dynawidget template", "slug", slug, "url", entry.Template)

	templateResp, err := defaultHTTPClient.Get(entry.Template)
	if err != nil {
		return "", "", nil, fmt.Errorf("fetching template: %w", err)
	}
	defer templateResp.Body.Close()

	if templateResp.StatusCode != http.StatusOK {
		return "", "", nil, fmt.Errorf("fetching template: %d %s", templateResp.StatusCode, http.StatusText(templateResp.StatusCode))
	}

	bodyBytes, err := io.ReadAll(templateResp.Body)
	if err != nil {
		return "", "", nil, fmt.Errorf("reading template body: %w", err)
	}

	rawContent := string(bodyBytes)

	// Save to disk for future use
	if err := os.MkdirAll(dynawidgetsAssetsDir, 0755); err != nil {
		slog.Error("Failed to create dynawidgets assets directory", "error", err)
	} else if err := os.WriteFile(templatePath, bodyBytes, 0644); err != nil {
		slog.Error("Failed to cache dynawidget template", "error", err, "path", templatePath)
	} else {
		slog.Info("Cached dynawidget template", "slug", slug, "path", templatePath)
	}

	templateContent, required = dynawidgetsParseTemplate(rawContent)
	return templateContent, entry.Title, required, nil
}
