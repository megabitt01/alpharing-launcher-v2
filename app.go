package main

import (
	"context"
	"crypto/sha256"
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

// BackgroundVideoURLs returns the background video's sources, in preferred
// order, served over a real loopback HTTP URL rather than the wails://
// custom scheme (see videoserver.go for why).
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

	gameStartTimeout    = 5 * time.Minute
	processPollInterval = 1 * time.Second
)

var workshopItems = []uint64{3686670451, 3730810482}

func (a *App) log(message string) {
	fmt.Println(message)
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "log", message)
	}
}

func configPath() string {
	exe, err := os.Executable()
	if err != nil {
		return configFileName
	}
	return filepath.Join(filepath.Dir(exe), configFileName)
}

func defaultGamePath() string {
	if runtime.GOOS == "windows" {
		return `C:\Program Files (x86)\Steam\steamapps\common\Halo The Master Chief Collection`
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local/share/Steam", gameSubpath)
}

func readConfigPath() (string, error) {
	path := configPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(fmt.Sprintf("path = %q\n", defaultGamePath())), 0644); err != nil {
			return "", err
		}
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(contents), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "path" {
			value = strings.Trim(strings.TrimSpace(value), `"`)
			if value != "" {
				return filepath.Clean(value), nil
			}
		}
	}
	return "", fmt.Errorf("config file does not contain a path")
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

func findGamePath() (string, error) {
	configured, err := readConfigPath()
	if err != nil {
		return "", err
	}
	for _, root := range steamRoots(configured) {
		libraries := append([]string{root}, libraryFolders(root)...)
		for _, library := range libraries {
			candidate := filepath.Join(library, gameSubpath)
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				return candidate, nil
			}
		}
	}
	if info, statErr := os.Stat(configured); statErr == nil && info.IsDir() {
		return configured, nil
	}
	return "", fmt.Errorf("please specify game location in config file")
}

func steamExecutable(gamePath string) string {
	if runtime.GOOS != "windows" {
		return "steam"
	}
	for _, root := range steamRoots(gamePath) {
		candidate := filepath.Join(root, "steam.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "steam.exe"
}

func workshopPath(gamePath string, item uint64) string {
	steamapps := filepath.Dir(filepath.Dir(gamePath))
	return filepath.Join(steamapps, "workshop", "content", strconv.Itoa(workshopAppID), strconv.FormatUint(item, 10))
}

func (a *App) ensureWorkshopItems(gamePath string) error {
	for _, item := range workshopItems {
		if _, err := os.Stat(workshopPath(gamePath, item)); err == nil {
			continue
		}
		a.log(fmt.Sprintf("Subscribing to Workshop item %d...", item))
		steam := steamExecutable(gamePath)
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

func latestAsset() ([]byte, string, error) {
	client := &http.Client{Timeout: 2 * time.Minute}
	request, err := http.NewRequest(http.MethodGet, releaseURL, nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("User-Agent", "AlphaRing-Launcher")
	response, err := client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("GitHub returned %s", response.Status)
	}
	var latest release
	if err := json.NewDecoder(response.Body).Decode(&latest); err != nil {
		return nil, "", err
	}
	for _, asset := range latest.Assets {
		if asset.Name != modDLLName {
			continue
		}
		assetRequest, err := http.NewRequest(http.MethodGet, asset.URL, nil)
		if err != nil {
			return nil, "", err
		}
		assetRequest.Header.Set("User-Agent", "AlphaRing-Launcher")
		assetResponse, err := client.Do(assetRequest)
		if err != nil {
			return nil, "", err
		}
		defer assetResponse.Body.Close()
		bytes, err := io.ReadAll(assetResponse.Body)
		return bytes, latest.TagName, err
	}
	return nil, "", fmt.Errorf("WTSAPI32.dll was not found in the latest release")
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
	bytes, _, err := latestAsset()
	if err != nil {
		return err
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
	command := exec.Command(steamExecutable(gamePath))
	if runtime.GOOS == "windows" && !vanilla {
		command.Args = append(command.Args, "steam://launch/976730/option2")
	} else {
		command.Args = append(command.Args, "-applaunch", "976730")
		if !vanilla {
			command.Args = append(command.Args, "-eac")
		}
	}
	if runtime.GOOS == "linux" && !vanilla {
		command.Env = append(os.Environ(), "WINEDLLOVERRIDES=WTSAPI32=n,b")
	}
	if err := command.Start(); err != nil {
		return err
	}
	a.waitForGameExit(gamePath)
	return nil
}

// waitForGameExit blocks until the game process appears under gamePath and
// then disappears again, so Play() only returns once MCC has actually
// closed. If the game never starts within gameStartTimeout, it gives up
// and returns immediately rather than blocking forever.
func (a *App) waitForGameExit(gamePath string) {
	deadline := time.Now().Add(gameStartTimeout)
	started := false
	for time.Now().Before(deadline) {
		if processRunningUnder(gamePath) {
			started = true
			break
		}
		time.Sleep(processPollInterval)
	}
	if !started {
		return
	}
	for processRunningUnder(gamePath) {
		time.Sleep(processPollInterval)
	}
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
		latest, _, err := latestAsset()
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
	gamePath, err := findGamePath()
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
	_, tag, err := latestAsset()
	return tag, err
}

func (a *App) OpenInstallDir() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	directory := filepath.Dir(exe)
	if runtime.GOOS == "windows" {
		return exec.Command("explorer.exe", directory).Start()
	}
	return exec.Command("xdg-open", directory).Start()
}
