package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	workshopSubscribeTimeout = 30 * time.Second
	workshopStallTimeout     = 30 * time.Second
	workshopDownloadTimeout  = 10 * time.Minute
	workshopLogInterval      = 3 * time.Second
	workshopPollInterval     = 200 * time.Millisecond
)

// Steamworks constants (see steamclientpublic.h / isteamugc.h / isteamremotestorage.h).
const (
	eResultOK = 1 // k_EResultOK

	itemStateInstalled       = 4  // k_EItemStateInstalled
	itemStateDownloading     = 16 // k_EItemStateDownloading
	itemStateDownloadPending = 32 // k_EItemStateDownloadPending

	// RemoteStorageSubscribePublishedFileResult_t::k_iCallback =
	// k_iSteamRemoteStorageCallbacks(1300) + 13. Verify against the actual
	// SDK version's isteamremotestorage.h if the bundled libraries are
	// ever updated.
	remoteStorageSubscribePublishedFileCallback = 1313
)

// remoteStorageSubscribeResult mirrors the native
// RemoteStorageSubscribePublishedFileResult_t layout:
//
//	struct RemoteStorageSubscribePublishedFileResult_t {
//	    EResult m_eResult;                    // int32
//	    PublishedFileId_t m_nPublishedFileId; // uint64, 8-byte aligned
//	};
type remoteStorageSubscribeResult struct {
	result          int32
	_               int32 // padding to align publishedFileID to 8 bytes
	publishedFileID uint64
}

// steamworksClient holds the purego-bound Steamworks flat-C-API function
// pointers and interface handles needed to subscribe to and download
// Workshop items. Created fresh per ensureWorkshopItems call; not safe for
// concurrent use since Steamworks requires all calls to come from one
// thread.
type steamworksClient struct {
	pipe  int32
	ugc   uintptr
	utils uintptr

	manualDispatchRunFrame func(pipe int32)
	subscribeItem          func(ugc uintptr, item uint64) uint64
	downloadItem           func(ugc uintptr, item uint64, highPriority bool) bool
	getItemState           func(ugc uintptr, item uint64) uint32
	getItemDownloadInfo    func(ugc uintptr, item uint64, downloaded, total *uint64) bool
	isAPICallCompleted     func(utils uintptr, call uint64, failed *bool) bool
	getAPICallResult       func(utils uintptr, call uint64, result *remoteStorageSubscribeResult, size int32, expectedCallback int32, failed *bool) bool
	shutdownFn             func()
}

// ensureSteamAPILibExtracted writes the embedded Steamworks redistributable
// to configDir() (creating it if necessary), skipping the write if the file
// already matches, and returns its absolute path.
func ensureSteamAPILibExtracted() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, steamAPILibFileName)
	if existing, err := os.ReadFile(dest); err == nil && bytes.Equal(existing, steamAPILibBytes) {
		return dest, nil
	}
	if err := os.WriteFile(dest, steamAPILibBytes, 0644); err != nil {
		return "", err
	}
	return dest, nil
}

func nullTerminated(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// newSteamworksClient extracts and loads the native Steamworks library,
// initializes the Steam API (piggybacking on the user's ownership of
// workshopAppID), and binds the handful of flat-C-API functions needed to
// subscribe to and download Workshop items.
func newSteamworksClient() (*steamworksClient, error) {
	libPath, err := ensureSteamAPILibExtracted()
	if err != nil {
		return nil, fmt.Errorf("could not extract Steamworks library: %w", err)
	}
	handle, err := openLibrary(libPath)
	if err != nil {
		return nil, fmt.Errorf("could not load %s: %w", steamAPILibFileName, err)
	}

	bind := func(fptr any, name string) error {
		addr, err := librarySymbol(handle, name)
		if err != nil || addr == 0 {
			return fmt.Errorf("Steamworks library is missing expected export %s: %w", name, err)
		}
		purego.RegisterFunc(fptr, addr)
		return nil
	}

	var (
		initFlat           func(errMsg *[1024]byte) int32
		manualDispatchInit func()
		getHSteamPipe      func() int32
		steamUGCAccessor   func() uintptr
		steamUtilsAccessor func() uintptr
	)
	sw := &steamworksClient{}

	for _, b := range []struct {
		fptr any
		name string
	}{
		{&initFlat, "SteamAPI_InitFlat"},
		{&sw.shutdownFn, "SteamAPI_Shutdown"},
		{&manualDispatchInit, "SteamAPI_ManualDispatch_Init"},
		{&getHSteamPipe, "SteamAPI_GetHSteamPipe"},
		{&sw.manualDispatchRunFrame, "SteamAPI_ManualDispatch_RunFrame"},
		{&steamUGCAccessor, "SteamAPI_SteamUGC_v021"},
		{&steamUtilsAccessor, "SteamAPI_SteamUtils_v011"},
		{&sw.subscribeItem, "SteamAPI_ISteamUGC_SubscribeItem"},
		{&sw.downloadItem, "SteamAPI_ISteamUGC_DownloadItem"},
		{&sw.getItemState, "SteamAPI_ISteamUGC_GetItemState"},
		{&sw.getItemDownloadInfo, "SteamAPI_ISteamUGC_GetItemDownloadInfo"},
		{&sw.isAPICallCompleted, "SteamAPI_ISteamUtils_IsAPICallCompleted"},
		{&sw.getAPICallResult, "SteamAPI_ISteamUtils_GetAPICallResult"},
	} {
		if err := bind(b.fptr, b.name); err != nil {
			return nil, err
		}
	}

	// Piggyback on the user's ownership of the actual game rather than
	// writing a steam_appid.txt into an arbitrary/unwritable working
	// directory; unset afterwards so it doesn't leak into the later
	// steam://launch/... shell-out in launch().
	os.Setenv("SteamAppId", strconv.Itoa(workshopAppID))
	defer os.Unsetenv("SteamAppId")

	var errMsg [1024]byte
	if result := initFlat(&errMsg); result != 0 { // k_ESteamAPIInitResult_OK == 0
		return nil, fmt.Errorf("Steam API failed to initialize: %s", nullTerminated(errMsg[:]))
	}

	manualDispatchInit()
	sw.pipe = getHSteamPipe()
	sw.ugc = steamUGCAccessor()
	if sw.ugc == 0 {
		sw.shutdownFn()
		return nil, fmt.Errorf("Steam did not return a Workshop interface")
	}
	sw.utils = steamUtilsAccessor()
	if sw.utils == 0 {
		sw.shutdownFn()
		return nil, fmt.Errorf("Steam did not return a Utils interface")
	}
	return sw, nil
}

func (sw *steamworksClient) shutdown() {
	if sw.shutdownFn != nil {
		sw.shutdownFn()
	}
}

// subscribeAndDownload subscribes to each item in missing via the Steamworks
// UGC API, then downloads and waits for all of them to finish installing,
// logging progress via a.log in the same style as the rest of app.go.
func (sw *steamworksClient) subscribeAndDownload(a *App, gamePath string, missing []uint64) error {
	a.log("Subscribing to required Workshop items...")

	pending := make(map[uint64]uint64, len(missing)) // item -> SteamAPICall_t handle
	for _, item := range missing {
		a.log(fmt.Sprintf("Subscribing to Workshop item %d...", item))
		call := sw.subscribeItem(sw.ugc, item)
		if call == 0 {
			return fmt.Errorf("Steam rejected the subscribe request for Workshop item %d", item)
		}
		pending[item] = call
	}

	deadline := time.Now().Add(workshopSubscribeTimeout)
	for len(pending) > 0 {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out subscribing to Workshop items")
		}
		sw.manualDispatchRunFrame(sw.pipe)
		for item, call := range pending {
			var ipcFailed bool
			if !sw.isAPICallCompleted(sw.utils, call, &ipcFailed) {
				continue
			}
			if ipcFailed {
				return fmt.Errorf("subscribe request for Workshop item %d failed to reach Steam", item)
			}
			var res remoteStorageSubscribeResult
			var resultFailed bool
			ok := sw.getAPICallResult(sw.utils, call, &res, int32(unsafe.Sizeof(res)), remoteStorageSubscribePublishedFileCallback, &resultFailed)
			if !ok || resultFailed {
				return fmt.Errorf("could not read subscribe result for Workshop item %d", item)
			}
			if res.result != eResultOK {
				return fmt.Errorf("Steam rejected subscribing to Workshop item %d (result %d)", item, res.result)
			}
			a.log(fmt.Sprintf("Subscribed to Workshop item %d", item))
			delete(pending, item)
		}
		if len(pending) > 0 {
			time.Sleep(workshopPollInterval)
		}
	}

	a.log("Downloading Workshop items...")
	for _, item := range missing {
		if !sw.downloadItem(sw.ugc, item, true) {
			return fmt.Errorf("Steam refused to start downloading Workshop item %d", item)
		}
	}

	remaining := make(map[uint64]bool, len(missing))
	for _, item := range missing {
		remaining[item] = true
	}
	lastBytes := make(map[uint64]uint64, len(missing))
	downloadDeadline := time.Now().Add(workshopDownloadTimeout)
	lastProgress := time.Now()
	lastLog := time.Now().Add(-workshopLogInterval)

	for len(remaining) > 0 {
		if time.Now().After(downloadDeadline) {
			return fmt.Errorf("Workshop items did not finish downloading before timing out")
		}
		sw.manualDispatchRunFrame(sw.pipe)

		for item := range remaining {
			state := sw.getItemState(sw.ugc, item)
			installed := state&itemStateInstalled != 0 &&
				state&(itemStateDownloading|itemStateDownloadPending) == 0
			if installed {
				a.log(fmt.Sprintf("Workshop item %d installed", item))
				delete(remaining, item)
			}
		}
		if len(remaining) == 0 {
			break
		}

		progressed := false
		for item := range remaining {
			var current, total uint64
			if sw.getItemDownloadInfo(sw.ugc, item, &current, &total) && lastBytes[item] != current {
				lastBytes[item] = current
				progressed = true
			}
		}
		if progressed {
			lastProgress = time.Now()
		} else if time.Since(lastProgress) >= workshopStallTimeout {
			return fmt.Errorf("Workshop item download stalled")
		}

		if time.Since(lastLog) >= workshopLogInterval {
			for item := range remaining {
				var current, total uint64
				if sw.getItemDownloadInfo(sw.ugc, item, &current, &total) {
					a.log(fmt.Sprintf("Downloading Workshop item %d: %d/%d bytes", item, current, total))
				}
			}
			lastLog = time.Now()
		}

		time.Sleep(workshopPollInterval)
	}

	// Final sanity check against the same on-disk location the fast-path
	// pre-check in ensureWorkshopItems uses.
	for _, item := range missing {
		if _, err := os.Stat(workshopPath(gamePath, item)); err != nil {
			return fmt.Errorf("Workshop item %d did not finish downloading", item)
		}
	}
	return nil
}
