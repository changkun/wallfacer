//go:build desktop

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"changkun.de/x/wallfacer/assets/icons"
	"changkun.de/x/wallfacer/internal/logger"

	"fyne.io/systray"
)

const (
	// trayPollInterval is the interval between health and config polls.
	trayPollInterval = 5 * time.Second
	// trayStatsPollInterval is the interval between stats polls.
	trayStatsPollInterval = 30 * time.Second
)

// TrayManager manages the system tray icon and menu for the desktop app.
type TrayManager struct {
	showWindow func()
	quit       func()
	serverURL  string
	apiKey     string
	httpClient *http.Client
	stopLoop   func() // returned by systray.RunWithExternalLoop
	done       chan struct{}

	// Menu items updated by poll.
	mInProgress *systray.MenuItem
	mWaiting    *systray.MenuItem
	mBacklog    *systray.MenuItem
	mCost       *systray.MenuItem
	mUptime     *systray.MenuItem

	// Automation toggle menu items.
	mAutopilot  *systray.MenuItem
	mAutotest   *systray.MenuItem
	mAutosubmit *systray.MenuItem
	mAutosync   *systray.MenuItem

	// Last known icon state to avoid redundant SetIcon calls.
	lastIconState string

	// Cost values updated by stats poll.
	todayCost float64
	totalCost float64
	costValid bool // true after at least one successful stats poll
}

// NewTrayManager creates a TrayManager. showWindow is called when the user
// clicks "Open Dashboard"; quit is called when the user clicks "Quit".
// serverURL is the base URL of the running HTTP server (e.g., "http://localhost:8080").
// apiKey is the optional WALLFACER_SERVER_API_KEY for authentication.
func NewTrayManager(showWindow, quit func(), serverURL, apiKey string) *TrayManager {
	return &TrayManager{
		showWindow: showWindow,
		quit:       quit,
		serverURL:  serverURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 3 * time.Second},
		done:       make(chan struct{}),
	}
}

// Start initializes the system tray icon and menu, then starts the poll loop.
// It must be called from the main goroutine on macOS.
func (tm *TrayManager) Start() {
	start, end := systray.RunWithExternalLoop(tm.onReady, func() {
		logger.Main.Info("system tray exited")
	})
	tm.stopLoop = end
	start()
}

// Stop tears down the system tray and stops the poll loop.
func (tm *TrayManager) Stop() {
	close(tm.done)
	systray.Quit()
	if tm.stopLoop != nil {
		tm.stopLoop()
	}
}

// onReady is called by systray when the tray is ready to receive menu items.
func (tm *TrayManager) onReady() {
	systray.SetTemplateIcon(icons.Tray, icons.Tray)
	systray.SetTooltip("Wallfacer")

	mOpen := systray.AddMenuItem("Open Dashboard", "Show the Wallfacer window")
	systray.AddSeparator()

	tm.mInProgress = systray.AddMenuItem("● 0 In Progress", "")
	tm.mInProgress.Disable()
	tm.mWaiting = systray.AddMenuItem("  0 Waiting", "")
	tm.mWaiting.Disable()
	tm.mBacklog = systray.AddMenuItem("  0 Backlog", "")
	tm.mBacklog.Disable()
	systray.AddSeparator()

	tm.mAutopilot = systray.AddMenuItemCheckbox("Autopilot", "Toggle autopilot", false)
	tm.mAutotest = systray.AddMenuItemCheckbox("Auto-test", "Toggle auto-test", false)
	tm.mAutosubmit = systray.AddMenuItemCheckbox("Auto-submit", "Toggle auto-submit", false)
	tm.mAutosync = systray.AddMenuItemCheckbox("Auto-sync", "Toggle auto-sync", false)
	systray.AddSeparator()

	tm.mCost = systray.AddMenuItem("Today: — · Total: —", "")
	tm.mCost.Disable()
	tm.mUptime = systray.AddMenuItem("Uptime: —", "")
	tm.mUptime.Disable()
	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Quit Wallfacer")

	// Click handler goroutine.
	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				tm.showWindow()
			case <-tm.mAutopilot.ClickedCh:
				tm.handleToggle("autopilot", tm.mAutopilot)
			case <-tm.mAutotest.ClickedCh:
				tm.handleToggle("autotest", tm.mAutotest)
			case <-tm.mAutosubmit.ClickedCh:
				tm.handleToggle("autosubmit", tm.mAutosubmit)
			case <-tm.mAutosync.ClickedCh:
				tm.handleToggle("autosync", tm.mAutosync)
			case <-mQuit.ClickedCh:
				tm.quit()
				return
			case <-tm.done:
				return
			}
		}
	}()

	// Poll loop goroutine.
	go func() {
		tm.poll()       // initial health poll
		tm.pollConfig() // initial config poll
		tm.pollStats()  // initial stats poll
		healthTicker := time.NewTicker(trayPollInterval)
		statsTicker := time.NewTicker(trayStatsPollInterval)
		defer healthTicker.Stop()
		defer statsTicker.Stop()
		for {
			select {
			case <-healthTicker.C:
				tm.poll()
				tm.pollConfig()
			case <-statsTicker.C:
				tm.pollStats()
			case <-tm.done:
				return
			}
		}
	}()
}

// healthData is the subset of the /api/debug/health response we care about.
type healthData struct {
	TasksByStatus map[string]int `json:"tasks_by_status"`
	UptimeSeconds float64        `json:"uptime_seconds"`
}

// poll fetches the health endpoint and updates the tray state.
func (tm *TrayManager) poll() {
	data, err := tm.fetchHealth()
	if err != nil {
		logger.Main.Debug("tray health poll failed", "error", err)
		return
	}

	// Update icon.
	state := iconState(data.TasksByStatus)
	if state != tm.lastIconState {
		switch state {
		case "active":
			systray.SetIcon(icons.TrayActive)
		case "attention":
			systray.SetIcon(icons.TrayAttention)
		default:
			systray.SetTemplateIcon(icons.Tray, icons.Tray)
		}
		tm.lastIconState = state
	}

	// Update tooltip.
	costStr := ""
	if tm.costValid {
		costStr = formatCostShort(tm.todayCost)
	}
	systray.SetTooltip(formatTooltip(data.TasksByStatus, data.UptimeSeconds, costStr))

	// Update menu labels.
	inProgress := data.TasksByStatus["in_progress"] + data.TasksByStatus["committing"]
	waiting := data.TasksByStatus["waiting"] + data.TasksByStatus["failed"]
	backlog := data.TasksByStatus["backlog"]

	if inProgress > 0 {
		tm.mInProgress.SetTitle(fmt.Sprintf("● %d In Progress", inProgress))
	} else {
		tm.mInProgress.SetTitle("  0 In Progress")
	}
	tm.mWaiting.SetTitle(fmt.Sprintf("  %d Waiting", waiting))
	tm.mBacklog.SetTitle(fmt.Sprintf("  %d Backlog", backlog))
	tm.mUptime.SetTitle(fmt.Sprintf("Uptime: %s", formatDuration(data.UptimeSeconds)))
}

// fetchHealth performs an HTTP GET to the health endpoint.
func (tm *TrayManager) fetchHealth() (*healthData, error) {
	req, err := http.NewRequest("GET", tm.serverURL+"/api/debug/health", nil)
	if err != nil {
		return nil, err
	}
	if tm.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+tm.apiKey)
	}
	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health returned %d", resp.StatusCode)
	}
	var data healthData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

// iconState returns the icon variant name based on task status counts.
func iconState(tasksByStatus map[string]int) string {
	if tasksByStatus["waiting"] > 0 || tasksByStatus["failed"] > 0 {
		return "attention"
	}
	if tasksByStatus["in_progress"] > 0 || tasksByStatus["committing"] > 0 {
		return "active"
	}
	return "idle"
}

// formatTooltip builds a tooltip string from task status, uptime, and optional cost.
// todayCost is the formatted cost string (e.g., "$3.42"); pass "" if unavailable.
func formatTooltip(tasksByStatus map[string]int, uptimeSeconds float64, todayCost string) string {
	running := tasksByStatus["in_progress"] + tasksByStatus["committing"]
	waiting := tasksByStatus["waiting"]
	if running == 0 && waiting == 0 {
		return "Wallfacer — Idle"
	}
	tooltip := fmt.Sprintf("Wallfacer — %d running · %d waiting", running, waiting)
	if todayCost != "" {
		tooltip += " · " + todayCost + " today"
	}
	return tooltip
}

// formatDuration formats seconds into a human-readable duration like "2h 15m".
func formatDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// configData is the subset of the /api/config response we care about.
type configData struct {
	Autopilot  bool `json:"autopilot"`
	Autotest   bool `json:"autotest"`
	Autosubmit bool `json:"autosubmit"`
	Autosync   bool `json:"autosync"`
}

// pollConfig fetches the config endpoint and updates the toggle check states.
func (tm *TrayManager) pollConfig() {
	cfg, err := tm.fetchConfig()
	if err != nil {
		logger.Main.Debug("tray config poll failed", "error", err)
		return
	}
	setChecked(tm.mAutopilot, cfg.Autopilot)
	setChecked(tm.mAutotest, cfg.Autotest)
	setChecked(tm.mAutosubmit, cfg.Autosubmit)
	setChecked(tm.mAutosync, cfg.Autosync)
}

// setChecked sets a menu item's checked state.
func setChecked(item *systray.MenuItem, checked bool) {
	if checked {
		item.Check()
	} else {
		item.Uncheck()
	}
}

// fetchConfig performs an HTTP GET to the config endpoint.
func (tm *TrayManager) fetchConfig() (*configData, error) {
	req, err := http.NewRequest("GET", tm.serverURL+"/api/config", nil)
	if err != nil {
		return nil, err
	}
	if tm.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+tm.apiKey)
	}
	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("config returned %d", resp.StatusCode)
	}
	var data configData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

// handleToggle sends a PUT /api/config to invert the given toggle.
// On success, the menu item's check state is updated. On failure, the
// state is left unchanged.
func (tm *TrayManager) handleToggle(field string, item *systray.MenuItem) {
	newValue := !item.Checked()
	if err := tm.toggleConfig(field, newValue); err != nil {
		logger.Main.Error("tray toggle failed", "field", field, "error", err)
		return
	}
	setChecked(item, newValue)
}

// statsData is the subset of the /api/stats response we care about.
type statsData struct {
	TotalCostUSD float64 `json:"total_cost_usd"`
	DailyUsage   []struct {
		Date    string  `json:"date"`
		CostUSD float64 `json:"cost_usd"`
	} `json:"daily_usage"`
}

// pollStats fetches the stats endpoint and updates cost display.
func (tm *TrayManager) pollStats() {
	data, err := tm.fetchStats()
	if err != nil {
		logger.Main.Debug("tray stats poll failed", "error", err)
		if !tm.costValid {
			tm.mCost.SetTitle("Today: — · Total: —")
		}
		return
	}

	tm.totalCost = data.TotalCostUSD
	tm.todayCost = extractTodayCost(data)
	tm.costValid = true

	tm.mCost.SetTitle(fmt.Sprintf("Today: %s · Total: %s",
		formatCostShort(tm.todayCost), formatCostShort(tm.totalCost)))
}

// fetchStats performs an HTTP GET to the stats endpoint.
func (tm *TrayManager) fetchStats() (*statsData, error) {
	req, err := http.NewRequest("GET", tm.serverURL+"/api/stats", nil)
	if err != nil {
		return nil, err
	}
	if tm.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+tm.apiKey)
	}
	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stats returned %d", resp.StatusCode)
	}
	var data statsData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

// extractTodayCost finds today's cost from the daily_usage array.
func extractTodayCost(data *statsData) float64 {
	today := time.Now().Format("2006-01-02")
	for _, d := range data.DailyUsage {
		if d.Date == today {
			return d.CostUSD
		}
	}
	return 0
}

// formatCostShort formats a USD cost as "$X.XX" with 2 decimal places.
func formatCostShort(usd float64) string {
	return fmt.Sprintf("$%.2f", usd)
}

// toggleConfig sends a PUT /api/config with a single toggled field.
func (tm *TrayManager) toggleConfig(field string, value bool) error {
	body := fmt.Sprintf(`{%q: %t}`, field, value)
	req, err := http.NewRequest("PUT", tm.serverURL+"/api/config", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if tm.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+tm.apiKey)
	}
	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("config PUT returned %d", resp.StatusCode)
	}
	return nil
}
