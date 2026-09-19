package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx             context.Context
	videoServerBase string
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if base, err := startVideoServer(); err == nil {
		a.videoServerBase = base
	} else {
		a.log(fmt.Sprintf("Error starting video server: %s", err))
	}
	for _, argument := range os.Args[1:] {
		if argument == "-fullscreen" {
			wailsruntime.WindowFullscreen(ctx)
			break
		}
	}
}

func (a *App) BackgroundVideoURLs() []string {
	if a.videoServerBase == "" {
		return nil
	}
	return []string{a.videoServerBase + "/bkgVideo.webm", a.videoServerBase + "/bkgVideo.mp4"}
}

const (
	gameSubpath    = "steamapps/common/Halo The Master Chief Collection"
	modDLLSubpath  = "MCC/Binaries/Win64"
	modDLLName     = "WTSAPI32.dll"
	configFileName = "launcher.cfg"
	workshopAppID  = 976730
	releaseURL     = "https://api.github.com/repos/megabitt01/AlphaRing/releases/latest"
)

var workshopItems = []uint64{3686670451, 3730810482}

func (a *App) log(message string) {
	fmt.Println(message)
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "log", message)
	}
}

func configDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", homeErr
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "alpharing"), nil
}

func configPath() string {
	dir, err := configDir()
	if err != nil {
		return configFileName
	}
	return filepath.Join(dir, configFileName)
}

func defaultGamePath() string {
	if runtime.GOOS == "windows" {
		return `C:\Program Files (x86)\Steam\steamapps\common\Halo The Master Chief Collection`
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local/share/Steam", gameSubpath)
}

// config holds the settings persisted in launcher.cfg, keyed by name.
// "gamePath" is the MCC installation folder and "steamPath" is the Steam
// executable; both are auto-detected when possible and otherwise filled in
// by prompting the user with a native file/folder browser (see
// promptDirectory/promptFile).
type config map[string]string

var configKeys = []string{"gamePath", "steamPath"}

func readConfig() (config, error) {
	path := configPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		gamePath := defaultGamePath()
		steamPath, _ := locateSteamExecutable(gamePath)
		if err := writeConfig(config{"gamePath": gamePath, "steamPath": steamPath}); err != nil {
			return nil, err
		}
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := config{}
	for _, line := range strings.Split(string(contents), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if key != "" {
			cfg[key] = value
		}
	}
	return cfg, nil
}

func writeConfig(cfg config) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var builder strings.Builder
	for _, key := range configKeys {
		fmt.Fprintf(&builder, "%s = %q\n", key, cfg[key])
	}
	return os.WriteFile(path, []byte(builder.String()), 0644)
}

func setConfigValue(key, value string) error {
	cfg, err := readConfig()
	if err != nil {
		cfg = config{}
	}
	cfg[key] = value
	return writeConfig(cfg)
}

// nearestExistingDir walks up from path until it finds a directory that
// exists, for use as a dialog's starting directory (Wails errors if given a
// starting directory that doesn't exist). Returns "" if none is found.
func nearestExistingDir(path string) string {
	for path != "" {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return ""
}

func (a *App) promptDirectory(title, hint string) (string, error) {
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: nearestExistingDir(hint),
	})
}

func (a *App) promptFile(title, hint string, filters []wailsruntime.FileFilter) (string, error) {
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: nearestExistingDir(hint),
		Filters:          filters,
	})
}

func steamRoots(gamePath string) []string {
	root := gamePath
	for range strings.Split(gameSubpath, "/") {
		root = filepath.Dir(root)
	}
	return []string{root}
}

func libraryFolders(path string) []string {
	contents, err := os.ReadFile(filepath.Join(path, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return nil
	}
	var libraries []string
	for _, line := range strings.Split(string(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, `"path"`) {
			parts := strings.Split(trimmed, `"`)
			if len(parts) > 3 {
				libraries = append(libraries, strings.ReplaceAll(parts[3], `\\`, `\`))
			}
		}
	}
	return libraries
}

func locateGamePath(configured string) (string, bool) {
	if configured == "" {
		return "", false
	}
	configured = filepath.Clean(configured)
	for _, root := range steamRoots(configured) {
		libraries := append([]string{root}, libraryFolders(root)...)
		for _, library := range libraries {
			candidate := filepath.Join(library, gameSubpath)
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				return candidate, true
			}
		}
	}
	if info, statErr := os.Stat(configured); statErr == nil && info.IsDir() {
		return configured, true
	}
	return "", false
}

func (a *App) findGamePath() (string, error) {
	cfg, err := readConfig()
	if err != nil {
		return "", err
	}
	if path, ok := locateGamePath(cfg["gamePath"]); ok {
		return path, nil
	}
	a.log("Could not locate the MCC installation, please select the game folder...")
	selected, err := a.promptDirectory("Select the Halo: The Master Chief Collection folder", cfg["gamePath"])
	if err != nil {
		return "", fmt.Errorf("could not open folder browser: %w", err)
	}
	if selected == "" {
		return "", fmt.Errorf("game location was not specified")
	}
	selected = filepath.Clean(selected)
	if err := setConfigValue("gamePath", selected); err != nil {
		return "", err
	}
	return selected, nil
}

func locateSteamExecutable(gamePath string) (string, bool) {
	if runtime.GOOS != "windows" {
		if path, err := exec.LookPath("steam"); err == nil {
			return path, true
		}
		return "", false
	}
	for _, root := range steamRoots(gamePath) {
		candidate := filepath.Join(root, "steam.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func (a *App) steamExecutable(gamePath string) (string, error) {
	cfg, err := readConfig()
	if err != nil {
		return "", err
	}
	if configured := cfg["steamPath"]; configured != "" {
		if info, statErr := os.Stat(configured); statErr == nil && !info.IsDir() {
			return configured, nil
		}
	}
	a.log("Could not locate Steam, please select the Steam executable...")
	var filters []wailsruntime.FileFilter
	hint := ""
	if runtime.GOOS == "windows" {
		filters = []wailsruntime.FileFilter{{DisplayName: "Steam executable (steam.exe)", Pattern: "steam.exe"}}
		if roots := steamRoots(gamePath); len(roots) > 0 {
			hint = roots[0]
		}
	}
	selected, err := a.promptFile("Select the Steam executable", hint, filters)
	if err != nil {
		return "", fmt.Errorf("could not open file browser: %w", err)
	}
	if selected == "" {
		return "", fmt.Errorf("steam executable was not specified")
	}
	selected = filepath.Clean(selected)
	if err := setConfigValue("steamPath", selected); err != nil {
		return "", err
	}
	return selected, nil
}

func workshopPath(gamePath string, item uint64) string {
	steamapps := filepath.Dir(filepath.Dir(gamePath))
	return filepath.Join(steamapps, "workshop", "content", strconv.Itoa(workshopAppID), strconv.FormatUint(item, 10))
}

func (a *App) ensureWorkshopItems(gamePath string) error {
	var steam string
	for _, item := range workshopItems {
		if _, err := os.Stat(workshopPath(gamePath, item)); err == nil {
			continue
		}
		if steam == "" {
			resolved, err := a.steamExecutable(gamePath)
			if err != nil {
				return err
			}
			steam = resolved
		}
		a.log(fmt.Sprintf("Subscribing to Workshop item %d...", item))
		if err := exec.Command(steam, "steam://subscribe/"+strconv.FormatUint(item, 10)).Start(); err != nil {
			return fmt.Errorf("could not contact Steam: %w", err)
		}
		deadline := time.Now().Add(10 * time.Minute)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(workshopPath(gamePath, item)); err == nil {
				a.log(fmt.Sprintf("Workshop item %d installed", item))
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if _, err := os.Stat(workshopPath(gamePath, item)); err != nil {
			return fmt.Errorf("Workshop item %d did not finish downloading", item)
		}
	}
	return nil
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func downloadAsset(client *http.Client, url string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "AlphaRing-Launcher")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub returned %s", response.Status)
	}
	return io.ReadAll(response.Body)
}

// latestAsset downloads the latest AlphaRing mod DLL and, if the release also
// publishes a "<dll>.sha256" checksum asset, verifies the download against it.
// verified reports whether that check actually ran, so callers can warn when
// a release doesn't yet publish a checksum to check against.
func latestAsset() (dll []byte, tag string, verified bool, err error) {
	client := &http.Client{Timeout: 2 * time.Minute}
	request, err := http.NewRequest(http.MethodGet, releaseURL, nil)
	if err != nil {
		return nil, "", false, err
	}
	request.Header.Set("User-Agent", "AlphaRing-Launcher")
	response, err := client.Do(request)
	if err != nil {
		return nil, "", false, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", false, fmt.Errorf("GitHub returned %s", response.Status)
	}
	var latest release
	if err := json.NewDecoder(response.Body).Decode(&latest); err != nil {
		return nil, "", false, err
	}

	var dllURL, checksumURL string
	for _, asset := range latest.Assets {
		switch asset.Name {
		case modDLLName:
			dllURL = asset.URL
		case modDLLName + ".sha256":
			checksumURL = asset.URL
		}
	}
	if dllURL == "" {
		return nil, "", false, fmt.Errorf("%s was not found in the latest release", modDLLName)
	}

	dll, err = downloadAsset(client, dllURL)
	if err != nil {
		return nil, "", false, err
	}

	if checksumURL == "" {
		return dll, latest.TagName, false, nil
	}
	checksum, err := downloadAsset(client, checksumURL)
	if err != nil {
		return nil, "", false, fmt.Errorf("could not download checksum for %s: %w", modDLLName, err)
	}
	expected := strings.Fields(string(checksum))
	if len(expected) == 0 {
		return nil, "", false, fmt.Errorf("checksum file for %s was empty", modDLLName)
	}
	actual := sha256.Sum256(dll)
	if !strings.EqualFold(expected[0], hex.EncodeToString(actual[:])) {
		return nil, "", false, fmt.Errorf("checksum mismatch for %s: expected %s, got %x", modDLLName, expected[0], actual)
	}
	return dll, latest.TagName, true, nil
}

func moveModFiles(source, destination string) error {
	for _, name := range []string{modDLLName, "alpha_ring_menu.bin", "alpha_ring_menu.cfg"} {
		from := filepath.Join(source, name)
		if _, err := os.Stat(from); os.IsNotExist(err) {
			continue
		}
		if err := os.MkdirAll(destination, 0755); err != nil {
			return err
		}
		if err := os.Rename(from, filepath.Join(destination, name)); err != nil {
			return err
		}
	}
	return nil
}

func fileHash(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}

func (a *App) installMod(modDir string) error {
	a.log("Downloading the latest AlphaRing release...")
	bytes, _, verified, err := latestAsset()
	if err != nil {
		return err
	}
	if verified {
		a.log("Verified mod checksum")
	} else {
		a.log("Warning: no checksum published for this release, installing unverified")
	}
	path := filepath.Join(modDir, modDLLName)
	if err := os.WriteFile(path, bytes, 0644); err != nil {
		return err
	}
	a.log(fmt.Sprintf("Installed mod to %s", path))
	return nil
}

func (a *App) launch(gamePath string, vanilla bool) error {
	a.log("Launching MCC...")
	steam, err := a.steamExecutable(gamePath)
	if err != nil {
		return err
	}
	command := exec.Command(steam)
	if !vanilla {
		command.Args = append(command.Args, "steam://launch/976730/option2")
	} else {
		command.Args = append(command.Args, "steam://launch/976730/option1")
	}
	if err := command.Start(); err != nil {
		return err
	}
	a.log("Game launched, closing launcher...")
	wailsruntime.Quit(a.ctx)
	return nil
}

func (a *App) checkMod(gamePath string, vanilla bool) error {
	modDir := filepath.Join(gamePath, modDLLSubpath)
	backupDir := filepath.Join(gamePath, "alpha_ring")
	modDLL := filepath.Join(modDir, modDLLName)
	backupDLL := filepath.Join(backupDir, modDLLName)
	if vanilla {
		if _, err := os.Stat(modDLL); err == nil {
			a.log("Moving mod files aside for vanilla play...")
			if err := os.MkdirAll(backupDir, 0755); err != nil {
				return err
			}
			if err := os.Rename(modDLL, backupDLL); err != nil {
				return err
			}
		}
		if err := moveModFiles(modDir, backupDir); err != nil {
			return err
		}
		return a.launch(gamePath, true)
	}
	modUpToDate := false
	if _, err := os.Stat(modDLL); err == nil {
		a.log("Checking installed mod version...")
		latest, _, _, err := latestAsset()
		if err != nil {
			return err
		}
		latestHash := sha256.Sum256(latest)
		installedHash, err := fileHash(modDLL)
		if err != nil {
			return err
		}
		modUpToDate = string(installedHash) == string(latestHash[:])
		if modUpToDate {
			a.log("Mod is up to date.")
		}
	}
	if !modUpToDate {
		if _, err := os.Stat(backupDLL); err == nil {
			a.log("Restoring cached mod files...")
			if err := os.Rename(backupDLL, modDLL); err != nil {
				return err
			}
		} else if err := a.installMod(modDir); err != nil {
			return err
		}
	}
	if err := moveModFiles(backupDir, modDir); err != nil {
		return err
	}
	return a.launch(gamePath, false)
}

func (a *App) Play(vanilla bool) error {
	a.log(fmt.Sprintf("Running %s version", strings.Title(runtime.GOOS)))
	a.log("Checking MCC installation...")
	gamePath, err := a.findGamePath()
	if err != nil {
		return err
	}
	a.log(fmt.Sprintf("Found game installation at %s", gamePath))
	if err := a.ensureWorkshopItems(gamePath); err != nil {
		return err
	}
	return a.checkMod(gamePath, vanilla)
}

func (a *App) LatestModVersion() (string, error) {
	_, tag, _, err := latestAsset()
	return tag, err
}

func (a *App) OpenInstallDir() error {
	directory, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return exec.Command("explorer.exe", directory).Start()
	}
	return exec.Command("xdg-open", directory).Start()
}
