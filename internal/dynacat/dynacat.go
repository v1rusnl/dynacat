package dynacat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	pageTemplate        = mustParseTemplate("page.html", "document.html", "footer.html")
	pageContentTemplate = mustParseTemplate("page-content.html")
	manifestTemplate    = mustParseTemplate("manifest.json")
)

const STATIC_ASSETS_CACHE_DURATION = 24 * time.Hour
const REMOTE_IMAGE_CACHE_DURATION = 7 * 24 * time.Hour

var reservedPageSlugs = []string{"login", "logout"}

type imageProxyInfo struct {
	URL           string
	AllowInsecure bool
}

type application struct {
	Version   string
	CreatedAt time.Time
	Config    config

	parsedManifest []byte

	slugToPage   map[string]*page
	widgetByID   map[uint64]widget
	widgetToPage map[uint64]*page

	RequiresAuth           bool
	authSecretKey          []byte
	usernameHashToUsername map[string]string
	authAttemptsMu         sync.Mutex
	failedAuthAttempts     map[string]*failedAuthAttempt

	todoStorage *todoStorage

	sseMu                sync.RWMutex
	sseClients           map[*sseClient]struct{}
	DynamicUpdateEnabled bool

	imageProxyMu   sync.RWMutex
	imageProxyURLs map[string]imageProxyInfo

	imageCache *imageCache
}

func newApplication(c *config) (*application, error) {
	app := &application{
		Version:        buildVersion,
		CreatedAt:      time.Now(),
		Config:         *c,
		slugToPage:     make(map[string]*page),
		widgetByID:     make(map[uint64]widget),
		widgetToPage:   make(map[uint64]*page),
		sseClients:     make(map[*sseClient]struct{}),
		imageProxyURLs: make(map[string]imageProxyInfo),
	}
	config := &app.Config

	//
	// Init auth
	//

	if len(config.Auth.Users) > 0 {
		secretBytes, err := base64.StdEncoding.DecodeString(config.Auth.SecretKey)
		if err != nil {
			return nil, fmt.Errorf("decoding secret-key: %v", err)
		}

		if len(secretBytes) != AUTH_SECRET_KEY_LENGTH {
			return nil, fmt.Errorf("secret-key must be exactly %d bytes", AUTH_SECRET_KEY_LENGTH)
		}

		app.usernameHashToUsername = make(map[string]string)
		app.failedAuthAttempts = make(map[string]*failedAuthAttempt)
		app.RequiresAuth = true

		for username := range config.Auth.Users {
			user := config.Auth.Users[username]
			usernameHash, err := computeUsernameHash(username, secretBytes)
			if err != nil {
				return nil, fmt.Errorf("computing username hash for user %s: %v", username, err)
			}
			app.usernameHashToUsername[string(usernameHash)] = username

			if user.PasswordHashString != "" {
				user.PasswordHash = []byte(user.PasswordHashString)
				user.PasswordHashString = ""
			} else {
				hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
				if err != nil {
					return nil, fmt.Errorf("hashing password for user %s: %v", username, err)
				}

				user.Password = ""
				user.PasswordHash = hashedPassword
			}
		}

		app.authSecretKey = secretBytes
	}

	//
	// Init themes
	//

	if !config.Theme.DisablePicker {
		themeKeys := make([]string, 0, 2)
		themeProps := make([]*themeProperties, 0, 2)

		defaultDarkTheme, ok := config.Theme.Presets.Get("default-dark")
		if ok && !config.Theme.SameAs(defaultDarkTheme) || !config.Theme.SameAs(&themeProperties{}) {
			themeKeys = append(themeKeys, "default-dark")
			themeProps = append(themeProps, &themeProperties{})
		}

		themeKeys = append(themeKeys, "default-light")
		themeProps = append(themeProps, &themeProperties{
			Light:                    true,
			BackgroundColor:          &hslColorField{240, 13, 95},
			PrimaryColor:             &hslColorField{230, 100, 30},
			NegativeColor:            &hslColorField{0, 70, 50},
			ContrastMultiplier:       1.3,
			TextSaturationMultiplier: 0.5,
		})

		themePresets, err := newOrderedYAMLMap(themeKeys, themeProps)
		if err != nil {
			return nil, fmt.Errorf("creating theme presets: %v", err)
		}
		config.Theme.Presets = *themePresets.Merge(&config.Theme.Presets)

		for key, properties := range config.Theme.Presets.Items() {
			properties.Key = key
			if err := properties.init(); err != nil {
				return nil, fmt.Errorf("initializing preset theme %s: %v", key, err)
			}
		}
	}

	config.Theme.Key = "default"
	if err := config.Theme.init(); err != nil {
		return nil, fmt.Errorf("initializing default theme: %v", err)
	}

	config.Server.BaseURL = strings.TrimRight(config.Server.BaseURL, "/")
	if config.Server.CacheDir == "" {
		config.Server.CacheDir = ".cache"
	}
	cacheDir := config.Server.CacheDir
	if !filepath.IsAbs(cacheDir) {
		absCacheDir, err := filepath.Abs(cacheDir)
		if err != nil {
			return nil, fmt.Errorf("resolving cache-dir: %v", err)
		}
		cacheDir = absCacheDir
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache-dir: %v", err)
	}
	config.Server.CacheDir = cacheDir

	//
	// Init pages
	//

	app.slugToPage[""] = &config.Pages[0]

	dynamicUpdateEnabled := true
	if v := os.Getenv("ENABLE_DYNAMIC_UPDATE"); v == "false" || v == "0" || v == "f" {
		dynamicUpdateEnabled = false
	}

	app.DynamicUpdateEnabled = dynamicUpdateEnabled

	app.imageCache = newImageCache(config.Server.BaseURL, config.Server.CacheDir)

	providers := &widgetProviders{
		assetResolver:        app.StaticAssetPath,
		imageCache:           app.imageCache,
		baseURL:              config.Server.BaseURL,
		DynamicUpdateEnabled: dynamicUpdateEnabled,
		app:                  app,
	}

	for p := range config.Pages {
		page := &config.Pages[p]
		page.PrimaryColumnIndex = -1

		if page.Slug == "" {
			page.Slug = titleToSlug(page.Title)
		}

		if slices.Contains(reservedPageSlugs, page.Slug) {
			return nil, fmt.Errorf("page slug \"%s\" is reserved", page.Slug)
		}

		app.slugToPage[page.Slug] = page

		if page.Width == "default" {
			page.Width = ""
		}

		if page.DesktopNavigationWidth == "" && page.DesktopNavigationWidth != "default" {
			page.DesktopNavigationWidth = page.Width
		}

		registerWidget := func(widget widget) {
			app.widgetByID[widget.GetID()] = widget
			app.widgetToPage[widget.GetID()] = page
			widget.setProviders(providers)
		}

		for i := range page.HeadWidgets {
			registerWidget(page.HeadWidgets[i])
		}

		for c := range page.Columns {
			column := &page.Columns[c]

			if page.PrimaryColumnIndex == -1 && column.Size == "full" {
				page.PrimaryColumnIndex = int8(c)
			}

			for w := range column.Widgets {
				registerWidget(column.Widgets[w])
			}
		}
	}

	config.Theme.CustomCSSFile = app.resolveUserDefinedAssetPath(config.Theme.CustomCSSFile)
	config.Branding.LogoURL = app.resolveUserDefinedAssetPath(config.Branding.LogoURL)

	config.Branding.FaviconURL = ternary(
		config.Branding.FaviconURL == "",
		app.StaticAssetPath("favicon.svg"),
		app.resolveUserDefinedAssetPath(config.Branding.FaviconURL),
	)

	config.Branding.FaviconType = ternary(
		strings.HasSuffix(config.Branding.FaviconURL, ".svg"),
		"image/svg+xml",
		"image/png",
	)

	if config.Branding.AppName == "" {
		config.Branding.AppName = "Dynacat"
	}

	if config.Branding.AppIconURL == "" {
		config.Branding.AppIconURL = app.StaticAssetPath("app-icon.svg")
	}

	if config.Branding.AppBackgroundColor == "" {
		config.Branding.AppBackgroundColor = config.Theme.BackgroundColorAsHex
	}

	manifest, err := executeTemplateToString(manifestTemplate, templateData{App: app})
	if err != nil {
		return nil, fmt.Errorf("parsing manifest.json: %v", err)
	}
	app.parsedManifest = []byte(manifest)

	//
	// Init todo storage
	//

	needsTodoDB := false
	for p := range config.Pages {
		for _, w := range config.Pages[p].HeadWidgets {
			if tw, ok := w.(*todoWidget); ok && tw.Storage == "server" {
				needsTodoDB = true
				break
			}
		}
		if needsTodoDB {
			break
		}
		for c := range config.Pages[p].Columns {
			for _, w := range config.Pages[p].Columns[c].Widgets {
				if tw, ok := w.(*todoWidget); ok && tw.Storage == "server" {
					needsTodoDB = true
					break
				}
			}
			if needsTodoDB {
				break
			}
		}
	}

	if needsTodoDB {
		dbPath := config.Server.DBPath
		if dbPath == "" {
			dbPath = "/app/assets/dynacat.db"
		}
		app.todoStorage = newTodoStorage(dbPath)
	}

	return app, nil
}

func (a *application) sseRegisterClient(c *sseClient) {
	a.sseMu.Lock()
	a.sseClients[c] = struct{}{}
	a.sseMu.Unlock()
}

func (a *application) sseUnregisterClient(c *sseClient) {
	a.sseMu.Lock()
	delete(a.sseClients, c)
	a.sseMu.Unlock()
}

func (a *application) sseBroadcast(msg string) {
	a.sseMu.RLock()
	defer a.sseMu.RUnlock()
	for c := range a.sseClients {
		select {
		case c.ch <- msg:
		default: // client too slow; drop rather than block
		}
	}
}

func (p *page) updateOutdatedWidgets() {
	now := time.Now()

	var wg sync.WaitGroup
	context := context.Background()

	for w := range p.HeadWidgets {
		widget := p.HeadWidgets[w]

		if !widget.requiresUpdate(&now) {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			widget.update(context)
		}()
	}

	for c := range p.Columns {
		for w := range p.Columns[c].Widgets {
			widget := p.Columns[c].Widgets[w]

			if !widget.requiresUpdate(&now) {
				continue
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				widget.update(context)
			}()
		}
	}

	wg.Wait()
}

func (p *page) GetMinUpdateInterval() int64 {
	if !p.DynamicUpdatesEnabled() {
		return 0
	}

	min, found := getMinUpdateIntervalForWidgets(p.HeadWidgets)

	for c := range p.Columns {
		m, f := getMinUpdateIntervalForWidgets(p.Columns[c].Widgets)
		if f {
			if !found || m < min {
				min = m
				found = true
			}
		}
	}

	if !found {
		return 0
	}

	return min.Milliseconds()
}

func getMinUpdateIntervalForWidgets(ws widgets) (time.Duration, bool) {
	min := 1 * time.Second
	found := false

	for _, w := range ws {
		var interval time.Duration
		widgetFound := false

		if cw, ok := w.(*customAPIWidget); ok {
			// Only include custom-api widgets in global polling if they don't have update-interval set
			// Widgets with update-interval will poll independently on the client side
			if cw.UpdateInterval == nil {
				widgetFound = true
				interval = 1 * time.Second
			}
		} else if group, ok := w.(*groupWidget); ok {
			interval, widgetFound = getMinUpdateIntervalForWidgets(group.Widgets)
		} else if sc, ok := w.(*splitColumnWidget); ok {
			interval, widgetFound = getMinUpdateIntervalForWidgets(sc.Widgets)
		}

		if widgetFound {
			if !found || interval < min {
				min = interval
			}
			found = true
		}
	}

	return min, found
}

func (a *application) resolveUserDefinedAssetPath(path string) string {
	if strings.HasPrefix(path, "/assets/") {
		return a.Config.Server.BaseURL + path
	}

	return path
}

type templateRequestData struct {
	Theme *themeProperties
}

type templateData struct {
	App     *application
	Page    *page
	Request templateRequestData
}

func (a *application) populateTemplateRequestData(data *templateRequestData, r *http.Request) {
	theme := &a.Config.Theme.themeProperties

	if !a.Config.Theme.DisablePicker {
		selectedTheme, err := r.Cookie("theme")
		if err == nil {
			preset, exists := a.Config.Theme.Presets.Get(selectedTheme.Value)
			if exists {
				theme = preset
			}
		}
	}

	data.Theme = theme
}

func (a *application) handlePageRequest(w http.ResponseWriter, r *http.Request) {
	page, exists := a.slugToPage[r.PathValue("page")]
	if !exists {
		a.handleNotFound(w, r)
		return
	}

	if a.handleUnauthorizedResponse(w, r, redirectToLogin) {
		return
	}

	data := templateData{
		Page: page,
		App:  a,
	}
	a.populateTemplateRequestData(&data.Request, r)

	var responseBytes bytes.Buffer
	err := pageTemplate.Execute(&responseBytes, data)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	w.Write(responseBytes.Bytes())
}

func (a *application) handlePageContentRequest(w http.ResponseWriter, r *http.Request) {
	page, exists := a.slugToPage[r.PathValue("page")]
	if !exists {
		a.handleNotFound(w, r)
		return
	}

	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}

	pageData := templateData{
		Page: page,
	}

	var err error
	var responseBytes bytes.Buffer
	isCacheBuilding := false

	func() {
		page.mu.Lock()
		defer page.mu.Unlock()

		// Determine cache-build status after widgets have had a chance to queue
		// image fetches to avoid missing the initial "building cache" response.
		page.updateOutdatedWidgets()
		if a.imageCache != nil {
			isCacheBuilding = a.imageCache.IsBuildingCache()
		}
		err = pageContentTemplate.Execute(&responseBytes, pageData)
	}()

	w.Header().Set("X-Dynacat-Cache-Building", strconv.FormatBool(isCacheBuilding))

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	w.Write(responseBytes.Bytes())
}

func (a *application) addressOfRequest(r *http.Request) string {
	remoteAddrWithoutPort := func() string {
		for i := len(r.RemoteAddr) - 1; i >= 0; i-- {
			if r.RemoteAddr[i] == ':' {
				return r.RemoteAddr[:i]
			}
		}

		return r.RemoteAddr
	}

	if !a.Config.Server.Proxied {
		return remoteAddrWithoutPort()
	}

	// This should probably be configurable or look for multiple headers, not just this one
	forwardedFor := r.Header.Get("X-Forwarded-For")
	if forwardedFor == "" {
		return remoteAddrWithoutPort()
	}

	ips := strings.Split(forwardedFor, ",")
	if len(ips) == 0 || ips[0] == "" {
		return remoteAddrWithoutPort()
	}

	return ips[0]
}

func (a *application) handleNotFound(w http.ResponseWriter, _ *http.Request) {
	// TODO: add proper not found page
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Page not found"))
}

func (a *application) handleWidgetContentRequest(w http.ResponseWriter, r *http.Request) {
	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}

	widgetValue := r.PathValue("widget")
	widgetID, err := strconv.ParseUint(widgetValue, 10, 64)
	if err != nil {
		a.handleNotFound(w, r)
		return
	}

	widget, exists := a.widgetByID[widgetID]
	if !exists {
		a.handleNotFound(w, r)
		return
	}

	page, exists := a.widgetToPage[widgetID]
	if !exists {
		a.handleNotFound(w, r)
		return
	}

	page.mu.Lock()
	defer page.mu.Unlock()

	widget.update(context.Background())

	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(widget.Render()))
}

func (a *application) handleWidgetActionRequest(w http.ResponseWriter, r *http.Request) {
	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}

	widgetID, err := strconv.ParseUint(r.PathValue("widget"), 10, 64)
	if err != nil {
		http.Error(w, "invalid widget", http.StatusBadRequest)
		return
	}

	widget, exists := a.widgetByID[widgetID]
	if !exists {
		a.handleNotFound(w, r)
		return
	}

	widget.handleRequest(w, r)
}

func (a *application) StaticAssetPath(asset string) string {
	return a.Config.Server.BaseURL + "/static/" + staticFSHash + "/" + asset
}

func (a *application) VersionedAssetPath(asset string) string {
	return a.Config.Server.BaseURL + asset +
		"?v=" + strconv.FormatInt(a.CreatedAt.Unix(), 10)
}

func (a *application) handleTodoLoad(w http.ResponseWriter, r *http.Request) {
	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}

	listID := r.PathValue("listID")
	tasks, err := a.todoStorage.loadTasks(listID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func (a *application) handleTodoSave(w http.ResponseWriter, r *http.Request) {
	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}

	listID := r.PathValue("listID")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "reading body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var tasks []todoTask
	if err := json.Unmarshal(body, &tasks); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := a.todoStorage.saveTasks(listID, tasks); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *application) server() (func() error, func() error) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", a.handlePageRequest)
	mux.HandleFunc("GET /{page}", a.handlePageRequest)

	mux.HandleFunc("GET /api/pages/{page}/content/{$}", a.handlePageContentRequest)

	if !a.Config.Theme.DisablePicker {
		mux.HandleFunc("POST /api/set-theme/{key}", a.handleThemeChangeRequest)
	}

	mux.HandleFunc("GET /api/widgets/{widget}/content/{$}", a.handleWidgetContentRequest)
	mux.HandleFunc("POST /api/widgets/{widget}/action/{action...}", a.handleWidgetActionRequest)
	mux.HandleFunc("GET /api/sse/updates", a.handleSSEUpdates)
	mux.HandleFunc("GET /api/image-proxy/{hash}", a.handleImageProxyRequest)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if a.RequiresAuth {
		mux.HandleFunc("GET /login", a.handleLoginPageRequest)
		mux.HandleFunc("GET /logout", a.handleLogoutRequest)
		mux.HandleFunc("POST /api/authenticate", a.handleAuthenticationAttempt)
	}

	if a.todoStorage != nil {
		mux.HandleFunc("GET /api/todo/{listID}", a.handleTodoLoad)
		mux.HandleFunc("PUT /api/todo/{listID}", a.handleTodoSave)
	}

	mux.Handle(
		fmt.Sprintf("GET /static/%s/{path...}", staticFSHash),
		http.StripPrefix(
			"/static/"+staticFSHash,
			fileServerWithCache(http.FS(staticFS), STATIC_ASSETS_CACHE_DURATION),
		),
	)

	if a.Config.Server.CacheDir != "" {
		mux.Handle(
			"GET /.cache/{path...}",
			http.StripPrefix(
				"/.cache",
				fileServerWithCache(http.Dir(a.Config.Server.CacheDir), REMOTE_IMAGE_CACHE_DURATION),
			),
		)
	}

	assetCacheControlValue := fmt.Sprintf(
		"public, max-age=%d",
		int(STATIC_ASSETS_CACHE_DURATION.Seconds()),
	)

	mux.HandleFunc(fmt.Sprintf("GET /static/%s/css/bundle.css", staticFSHash), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", assetCacheControlValue)
		w.Header().Add("Content-Type", "text/css; charset=utf-8")
		w.Write(bundledCSSContents)
	})

	mux.HandleFunc("GET /manifest.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", assetCacheControlValue)
		w.Header().Add("Content-Type", "application/json")
		w.Write(a.parsedManifest)
	})

	assetsPath := a.Config.Server.AssetsPath
	if assetsPath == "" {
		assetsPath = "/app/assets"
	}

	absAssetsPath, _ := filepath.Abs(assetsPath)
	assetsFS := fileServerWithCache(http.Dir(assetsPath), 2*time.Hour)
	mux.Handle("/assets/{path...}", http.StripPrefix("/assets/", assetsFS))

	server := http.Server{
		Addr:    fmt.Sprintf("%s:%d", a.Config.Server.Host, a.Config.Server.Port),
		Handler: mux,
	}

	start := func() error {
		log.Printf("Starting server on %s:%d (base-url: \"%s\", assets-path: \"%s\")\n",
			a.Config.Server.Host,
			a.Config.Server.Port,
			a.Config.Server.BaseURL,
			absAssetsPath,
		)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}

		return nil
	}

	ctx, cancelCtx := context.WithCancel(context.Background())
	go a.sseUpdateLoop(ctx)

	stop := func() error {
		cancelCtx()
		return server.Close()
	}

	return start, stop
}
