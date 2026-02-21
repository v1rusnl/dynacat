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
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var torrentingWidgetTemplate = mustParseTemplate("torrenting.html", "widget-base.html")

var errTorrentUnauthorized = errors.New("qbittorrent: unauthorized")

type TorrentingHostConfig struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	client   *http.Client
}

type torrentingWidget struct {
	widgetBase    `yaml:",inline"`
	Hosts         []TorrentingHostConfig `yaml:"hosts"`
	HideCompleted bool                   `yaml:"hide-completed"`
	HideInactive  bool                   `yaml:"hide-inactive"`
	HideBar       bool                   `yaml:"hide-bar"`
	WrapText      bool                   `yaml:"wrap-text"`
	CollapseAfter int                    `yaml:"collapse-after"`

	mu       sync.RWMutex
	Torrents []torrentInfo
}

type torrentInfo struct {
	Name          string
	State         string
	Progress      float64
	Downloaded    int64
	Size          int64
	ETA           int64
	IsCompleted   bool
	IsActive      bool
	Icon          string
	FmtProgress   string
	FmtETA        string
	ShortName     string
	ProgressWidth string
}

type qbTorrentJSON struct {
	Name       string  `json:"name"`
	State      string  `json:"state"`
	Progress   float64 `json:"progress"`
	Downloaded int64   `json:"downloaded"`
	Size       int64   `json:"size"`
	ETA        int64   `json:"eta"`
}

func (widget *torrentingWidget) initialize() error {
	widget.withTitle("Torrents")

	if widget.UpdateInterval == nil {
		interval := updateIntervalField(30 * time.Second)
		widget.UpdateInterval = &interval
	}

	widget.withCacheDuration(time.Duration(*widget.UpdateInterval))

	if widget.CollapseAfter == 0 {
		widget.CollapseAfter = 3
	}

	if len(widget.Hosts) == 0 {
		return fmt.Errorf("at least one host must be specified")
	}

	for i := range widget.Hosts {
		host := &widget.Hosts[i]
		if host.URL == "" {
			return fmt.Errorf("host URL is required")
		}
		if host.Username == "" {
			return fmt.Errorf("host username is required")
		}
		if host.Password == "" {
			return fmt.Errorf("host password is required")
		}

		jar, err := cookiejar.New(nil)
		if err != nil {
			return fmt.Errorf("failed to create cookie jar: %w", err)
		}
		host.client = &http.Client{
			Jar:     jar,
			Timeout: 10 * time.Second,
		}
	}

	return nil
}

func (widget *torrentingWidget) update(ctx context.Context) {
	type fetchResult struct {
		torrents []torrentInfo
		err      error
		url      string
	}

	results := make(chan fetchResult, len(widget.Hosts))
	var wg sync.WaitGroup

	for i := range widget.Hosts {
		host := &widget.Hosts[i]
		wg.Add(1)
		go func(h *TorrentingHostConfig) {
			defer wg.Done()
			torrents, err := widget.fetchFromHost(ctx, h)
			results <- fetchResult{torrents: torrents, err: err, url: h.URL}
		}(host)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var allTorrents []torrentInfo
	successCount := 0
	errorCount := 0

	for result := range results {
		if result.err != nil {
			errorCount++
			slog.Error("failed to fetch torrents from qBittorrent", "url", result.url, "error", result.err)
			continue
		}
		successCount++
		allTorrents = append(allTorrents, result.torrents...)
	}

	sort.SliceStable(allTorrents, func(i, j int) bool {
		if p, q := torrentDownloadPriority(allTorrents[i]), torrentDownloadPriority(allTorrents[j]); p != q {
			return p < q
		}
		if allTorrents[i].IsCompleted != allTorrents[j].IsCompleted {
			return allTorrents[j].IsCompleted
		}
		return strings.ToLower(allTorrents[i].Name) < strings.ToLower(allTorrents[j].Name)
	})

	widget.mu.Lock()
	widget.Torrents = allTorrents
	widget.mu.Unlock()

	var err error
	if successCount == 0 {
		err = errNoContent
	} else if errorCount > 0 {
		err = errPartialContent
	}

	if !widget.canContinueUpdateAfterHandlingErr(err) {
		return
	}
}

func (widget *torrentingWidget) login(ctx context.Context, host *TorrentingHostConfig) error {
	loginURL := strings.TrimRight(host.URL, "/") + "/api/v2/auth/login"

	form := url.Values{
		"username": {host.Username},
		"password": {host.Password},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := host.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if strings.TrimSpace(string(body)) != "Ok." {
		return fmt.Errorf("login failed: %s", strings.TrimSpace(string(body)))
	}

	return nil
}

func (widget *torrentingWidget) fetchFromHost(ctx context.Context, host *TorrentingHostConfig) ([]torrentInfo, error) {
	torrents, err := widget.fetchTorrentsOnce(ctx, host)
	if errors.Is(err, errTorrentUnauthorized) {
		slog.Info("qBittorrent session expired, re-logging in", "url", host.URL)
		if loginErr := widget.login(ctx, host); loginErr != nil {
			slog.Error("qBittorrent re-login failed", "url", host.URL, "error", loginErr)
			return nil, loginErr
		}
		torrents, err = widget.fetchTorrentsOnce(ctx, host)
	}
	return torrents, err
}

func (widget *torrentingWidget) fetchTorrentsOnce(ctx context.Context, host *TorrentingHostConfig) ([]torrentInfo, error) {
	apiURL := strings.TrimRight(host.URL, "/") + "/api/v2/torrents/info"

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := host.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errTorrentUnauthorized
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, host.URL)
	}

	var raw []qbTorrentJSON
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode torrents JSON: %w", err)
	}

	torrents := make([]torrentInfo, 0, len(raw))
	for _, t := range raw {
		torrents = append(torrents, computeTorrentInfo(t))
	}
	return torrents, nil
}

func computeTorrentInfo(t qbTorrentJSON) torrentInfo {
	info := torrentInfo{
		Name:       t.Name,
		State:      t.State,
		Progress:   t.Progress,
		Downloaded: t.Downloaded,
		Size:       t.Size,
		ETA:        t.ETA,
	}

	info.IsCompleted = t.Progress >= 1.0

	switch t.State {
	case "downloading", "forcedDL", "uploading", "forcedUP":
		info.IsActive = true
	}

	switch {
	case info.IsCompleted:
		info.Icon = "✔"
	case t.State == "downloading" || t.State == "forcedDL":
		info.Icon = "↓"
	case t.State == "uploading" || t.State == "forcedUP":
		info.Icon = "↑"
	case t.State == "error" || t.State == "missingFiles":
		info.Icon = "!"
	case t.State == "checkingDL" || t.State == "checkingUP" || t.State == "allocating":
		info.Icon = "…"
	case t.State == "checkingResumeData":
		info.Icon = "⟳"
	default:
		info.Icon = "❚❚"
	}

	if t.Size >= 1_073_741_824 {
		info.FmtProgress = fmt.Sprintf("%.2f GB / %.2f GB",
			float64(t.Downloaded)/1_073_741_824,
			float64(t.Size)/1_073_741_824,
		)
	} else {
		info.FmtProgress = fmt.Sprintf("%.2f MB / %.2f MB",
			float64(t.Downloaded)/1_048_576,
			float64(t.Size)/1_048_576,
		)
	}

	if t.ETA < 0 || t.ETA >= 8_640_000 {
		info.FmtETA = "∞"
	} else if t.ETA == 0 {
		info.FmtETA = "0m"
	} else {
		h := t.ETA / 3600
		m := (t.ETA % 3600) / 60
		s := t.ETA % 60
		if h > 0 {
			info.FmtETA = fmt.Sprintf("%dh %dm", h, m)
		} else if m > 0 {
			info.FmtETA = fmt.Sprintf("%dm", m)
		} else {
			info.FmtETA = fmt.Sprintf("%ds", s)
		}
	}

	runes := []rune(t.Name)
	if len(runes) > 40 {
		info.ShortName = string(runes[:40]) + "..."
	} else {
		info.ShortName = t.Name
	}

	info.ProgressWidth = fmt.Sprintf("%.1f%%", t.Progress*100)

	return info
}

func torrentDownloadPriority(info torrentInfo) int {
	switch info.State {
	case "downloading", "forcedDL":
		return 0
	case "uploading", "forcedUP":
		return 1
	default:
		if info.IsCompleted {
			return 3
		}
		return 2
	}
}

func (widget *torrentingWidget) Render() template.HTML {
	widget.mu.RLock()
	defer widget.mu.RUnlock()
	return widget.renderTemplate(widget, torrentingWidgetTemplate)
}
