use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};

use sha2::{Digest, Sha256};
use steamworks::PublishedFileId;
use tauri::{AppHandle, Emitter, Manager};
use tauri_plugin_opener::OpenerExt;

const GAME_SUBPATH: &str = "steamapps/common/Halo The Master Chief Collection";
const CFG_FILE_NAME: &str = "launcher.cfg";
const CFG_PATH_KEY: &str = "path";
const MOD_DLL_SUBPATH: &str = "MCC/Binaries/Win64";
const MOD_DLL_NAME: &str = "WTSAPI32.dll";
const EXTRA_MOD_FILES: &[&str] = &["alpha_ring_menu.bin", "alpha_ring_menu.cfg"];
const RELEASES_API_URL: &str = "https://api.github.com/repos/megabitt01/AlphaRing/releases/latest";
const WORKSHOP_APP_ID: u32 = 976730;
const WORKSHOP_ITEM_IDS: &[u64] = &[3686670451, 3730810482];
const WORKSHOP_SUBSCRIBE_TIMEOUT: Duration = Duration::from_secs(30);
const WORKSHOP_DOWNLOAD_TIMEOUT: Duration = Duration::from_secs(600);
const WORKSHOP_STALL_TIMEOUT: Duration = Duration::from_secs(30);

#[cfg(target_os = "windows")]
static STEAM_API_DLL_BYTES: &[u8] = include_bytes!(concat!(env!("OUT_DIR"), "/steam_api64.dll"));

#[cfg(target_os = "windows")]
fn ensure_steam_api_dll_extracted() {
    let dest = match std::env::current_exe() {
        Ok(exe) => exe.with_file_name("steam_api64.dll"),
        Err(_) => return,
    };

    let up_to_date = fs::read(&dest)
        .map(|existing| existing == STEAM_API_DLL_BYTES)
        .unwrap_or(false);

    if !up_to_date {
        let _ = fs::write(&dest, STEAM_API_DLL_BYTES);
    }
}

struct AppPaths {
    game_path: PathBuf,
    backup_path: PathBuf,
}

static APP_PATHS: OnceLock<Mutex<Option<AppPaths>>> = OnceLock::new();

fn app_paths_lock() -> &'static Mutex<Option<AppPaths>> {
    APP_PATHS.get_or_init(|| Mutex::new(None))
}

fn set_app_paths(game_path: PathBuf) {
    let backup_path = game_path.join("alpha_ring");
    *app_paths_lock().lock().unwrap() = Some(AppPaths {
        game_path,
        backup_path,
    });
}

fn get_app_paths() -> Result<(PathBuf, PathBuf), String> {
    app_paths_lock()
        .lock()
        .unwrap()
        .as_ref()
        .map(|paths| (paths.game_path.clone(), paths.backup_path.clone()))
        .ok_or_else(|| "Game path has not been detected yet; run checkOs first".to_string())
}

fn emit_log(app: &AppHandle, message: impl Into<String>) {
    let message = message.into();
    println!("{message}");
    let _ = app.emit("log", message);
}

// The cfg's "path" value is the full game install directory
// (<steam root>/steamapps/common/<GAME_SUBPATH>), so the Steam root is
// recovered by walking up past that known suffix.
fn steam_root_candidates() -> Vec<PathBuf> {
    let subpath_depth = Path::new(GAME_SUBPATH).components().count();

    read_cfg_path()
        .and_then(|game_path| game_path.ancestors().nth(subpath_depth).map(PathBuf::from))
        .into_iter()
        .collect()
}

fn parse_library_folders(vdf_path: &Path) -> Vec<PathBuf> {
    let Ok(contents) = fs::read_to_string(vdf_path) else {
        return Vec::new();
    };

    contents
        .lines()
        .filter(|line| line.trim_start().starts_with("\"path\""))
        .filter_map(|line| line.split('"').nth(3))
        .map(PathBuf::from)
        .collect()
}

#[cfg(target_os = "windows")]
fn default_cfg_path_value() -> PathBuf {
    PathBuf::from(
        r"C:\Program Files (x86)\Steam\steamapps\common\Halo The Master Chief Collection",
    )
}

#[cfg(target_os = "linux")]
fn default_cfg_path_value() -> PathBuf {
    let home = std::env::var_os("HOME")
        .map(PathBuf::from)
        .unwrap_or_default();
    home.join(".local/share/Steam/steamapps/common/Halo The Master Chief Collection")
}

// On Linux, an AppImage runs from a temporary squashfs mount, so
// `current_exe` doesn't point next to the actual .AppImage file — but the
// AppImage runtime sets `APPIMAGE` to that file's real location.
#[cfg(target_os = "linux")]
fn cfg_dir() -> Result<PathBuf, String> {
    if let Some(appimage) = std::env::var_os("APPIMAGE") {
        if let Some(parent) = PathBuf::from(appimage).parent() {
            return Ok(parent.to_path_buf());
        }
    }

    std::env::current_exe()
        .map_err(|e| e.to_string())?
        .parent()
        .map(|p| p.to_path_buf())
        .ok_or_else(|| "Could not determine executable directory".to_string())
}

#[cfg(target_os = "windows")]
fn cfg_dir() -> Result<PathBuf, String> {
    std::env::current_exe()
        .map_err(|e| e.to_string())?
        .parent()
        .map(|p| p.to_path_buf())
        .ok_or_else(|| "Could not determine executable directory".to_string())
}

fn cfg_file_path() -> Result<PathBuf, String> {
    Ok(cfg_dir()?.join(CFG_FILE_NAME))
}

// Writes the cfg file with its default value the first time the launcher
// runs. Left untouched afterwards so a user's custom path sticks around.
fn ensure_cfg_file() -> Result<PathBuf, String> {
    let path = cfg_file_path()?;

    if !path.exists() {
        let contents = format!(
            "{} = \"{}\"\n",
            CFG_PATH_KEY,
            default_cfg_path_value().display()
        );
        fs::write(&path, contents).map_err(|e| e.to_string())?;
    }

    Ok(path)
}

fn read_cfg_path() -> Option<PathBuf> {
    let path = ensure_cfg_file().ok()?;
    let contents = fs::read_to_string(&path).ok()?;

    contents.lines().find_map(|line| {
        let (key, value) = line.split_once('=')?;
        if key.trim() != CFG_PATH_KEY {
            return None;
        }

        let value = value.trim();
        let value = value
            .strip_prefix('"')
            .and_then(|v| v.strip_suffix('"'))
            .unwrap_or(value);

        if value.is_empty() {
            None
        } else {
            Some(PathBuf::from(value))
        }
    })
}

fn find_game_path() -> Option<PathBuf> {
    for steam_root in steam_root_candidates() {
        let steamapps = steam_root.join("steamapps");
        if !steamapps.is_dir() {
            continue;
        }

        let mut libraries = vec![steam_root];
        libraries.extend(parse_library_folders(&steamapps.join("libraryfolders.vdf")));

        for library in libraries {
            let candidate = library.join(GAME_SUBPATH);
            if candidate.is_dir() {
                return Some(candidate);
            }
        }
    }

    read_cfg_path().filter(|path| path.is_dir())
}

fn workshop_item_path(steamapps_dir: &Path, item_id: u64) -> PathBuf {
    steamapps_dir
        .join("workshop/content")
        .join(WORKSHOP_APP_ID.to_string())
        .join(item_id.to_string())
}

fn ensure_workshop_items(app: &AppHandle, steamapps_dir: &Path) -> Result<(), String> {
    let missing: Vec<u64> = WORKSHOP_ITEM_IDS
        .iter()
        .copied()
        .filter(|id| !workshop_item_path(steamapps_dir, *id).exists())
        .collect();

    if missing.is_empty() {
        return Ok(());
    }

    emit_log(app, "Subscribing to required Workshop items...");

    let client = steamworks::Client::init_app(WORKSHOP_APP_ID)
        .map_err(|err| format!("Couldn't connect to Steam to fetch Workshop items: {err}"))?;

    let ugc = client.ugc();
    let pending = Arc::new(AtomicUsize::new(missing.len()));
    let subscribe_failed = Arc::new(AtomicUsize::new(0));

    for item_id in &missing {
        let item_id = *item_id;
        let pending = pending.clone();
        let subscribe_failed = subscribe_failed.clone();
        let app = app.clone();

        ugc.subscribe_item(PublishedFileId(item_id), move |result| {
            match result {
                Ok(()) => emit_log(&app, format!("Subscribed to Workshop item {item_id}")),
                Err(err) => {
                    emit_log(
                        &app,
                        format!("Failed to subscribe to Workshop item {item_id}: {err}"),
                    );
                    subscribe_failed.fetch_add(1, Ordering::SeqCst);
                }
            }
            pending.fetch_sub(1, Ordering::SeqCst);
        });
    }

    let start = Instant::now();
    while pending.load(Ordering::SeqCst) > 0 && start.elapsed() < WORKSHOP_SUBSCRIBE_TIMEOUT {
        client.run_callbacks();
        std::thread::sleep(Duration::from_millis(100));
    }

    if pending.load(Ordering::SeqCst) > 0 {
        return Err("Timed out subscribing to Workshop items".to_string());
    }
    if subscribe_failed.load(Ordering::SeqCst) > 0 {
        return Err("Failed to subscribe to one or more Workshop items".to_string());
    }

    for item_id in &missing {
        ugc.download_item(PublishedFileId(*item_id), true);
    }

    emit_log(app, "Downloading Workshop items...");

    let mut remaining: Vec<u64> = missing.clone();
    let download_start = Instant::now();
    let mut last_log = Instant::now() - Duration::from_secs(3);
    let mut last_progress = Instant::now();
    let mut last_bytes: std::collections::HashMap<u64, u64> = std::collections::HashMap::new();

    while !remaining.is_empty() && download_start.elapsed() < WORKSHOP_DOWNLOAD_TIMEOUT {
        client.run_callbacks();

        remaining.retain(|item_id| {
            let state = ugc.item_state(PublishedFileId(*item_id));
            let installed = state.contains(steamworks::ItemState::INSTALLED)
                && !state.contains(steamworks::ItemState::DOWNLOADING)
                && !state.contains(steamworks::ItemState::DOWNLOAD_PENDING);

            if installed {
                emit_log(app, format!("Workshop item {item_id} installed"));
            }

            !installed
        });

        let mut any_progress = false;
        for item_id in &remaining {
            if let Some((current, _total)) = ugc.item_download_info(PublishedFileId(*item_id)) {
                if last_bytes.insert(*item_id, current) != Some(current) {
                    any_progress = true;
                }
            }
        }
        if any_progress {
            last_progress = Instant::now();
        }

        if !remaining.is_empty() && last_progress.elapsed() >= WORKSHOP_STALL_TIMEOUT {
            return Err("Workshop item download stalled".to_string());
        }

        if !remaining.is_empty() && last_log.elapsed() >= Duration::from_secs(3) {
            for item_id in &remaining {
                if let Some((current, total)) = ugc.item_download_info(PublishedFileId(*item_id))
                {
                    emit_log(
                        app,
                        format!("Downloading Workshop item {item_id}: {current}/{total} bytes"),
                    );
                }
            }
            last_log = Instant::now();
        }

        std::thread::sleep(Duration::from_millis(200));
    }

    if !remaining.is_empty() {
        return Err(format!(
            "Workshop items did not finish downloading before timing out: {remaining:?}"
        ));
    }

    Ok(())
}

fn check_os(app: &AppHandle) -> Result<(), String> {
    if cfg!(target_os = "windows") {
        emit_log(app, "Running Windows version");
    } else if cfg!(target_os = "linux") {
        emit_log(app, "Running Linux version");
    }

    emit_log(app, "Checking MCC installation...");

    let game_path = find_game_path()
        .ok_or_else(|| "Please specify game location in config file.".to_string())?;

    emit_log(app, format!("Found game installation at {}", game_path.display()));

    if let Some(steamapps_dir) = game_path.parent().and_then(Path::parent) {
        if let Err(err) = ensure_workshop_items(app, steamapps_dir) {
            emit_log(app, format!("Closing: {err}"));
            app.exit(1);
            return Err(err);
        }
    }

    set_app_paths(game_path);
    Ok(())
}

fn sha256_hex(bytes: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    format!("{:x}", hasher.finalize())
}

fn sha256_of_file(path: &Path) -> Result<String, String> {
    let bytes = fs::read(path).map_err(|e| e.to_string())?;
    Ok(sha256_hex(&bytes))
}

fn move_extra_mod_files(src_dir: &Path, dest_dir: &Path) -> Result<(), String> {
    for file_name in EXTRA_MOD_FILES {
        let src = src_dir.join(file_name);
        if !src.exists() {
            continue;
        }

        fs::create_dir_all(dest_dir).map_err(|e| e.to_string())?;
        fs::rename(&src, dest_dir.join(file_name)).map_err(|e| e.to_string())?;
    }

    Ok(())
}

async fn fetch_latest_release(client: &reqwest::Client) -> Result<serde_json::Value, String> {
    client
        .get(RELEASES_API_URL)
        .header("User-Agent", "AlphaRing-Launcher")
        .send()
        .await
        .map_err(|e| e.to_string())?
        .error_for_status()
        .map_err(|e| e.to_string())?
        .json()
        .await
        .map_err(|e| e.to_string())
}

async fn fetch_latest_release_asset(client: &reqwest::Client) -> Result<Vec<u8>, String> {
    let release = fetch_latest_release(client).await?;

    let assets = release["assets"]
        .as_array()
        .ok_or("Release contains no assets")?;

    let asset = assets
        .iter()
        .find(|asset| asset["name"].as_str() == Some(MOD_DLL_NAME))
        .ok_or("WTSAPI32.dll was not found in the latest release")?;

    let download_url = asset["browser_download_url"]
        .as_str()
        .ok_or("Release asset has no download URL")?;

    let bytes = client
        .get(download_url)
        .header("User-Agent", "AlphaRing-Launcher")
        .send()
        .await
        .map_err(|e| e.to_string())?
        .error_for_status()
        .map_err(|e| e.to_string())?
        .bytes()
        .await
        .map_err(|e| e.to_string())?;

    Ok(bytes.to_vec())
}

async fn install_mod(app: &AppHandle, target_dir: &Path) -> Result<String, String> {
    if !target_dir.exists() {
        return Err(format!(
            "Directory does not exist: {}",
            target_dir.display()
        ));
    }

    if !target_dir.is_dir() {
        return Err(format!("Path is not a directory: {}", target_dir.display()));
    }

    emit_log(app, "Downloading the latest AlphaRing release...");

    let client = reqwest::Client::new();
    let bytes = fetch_latest_release_asset(&client).await?;
    let destination = target_dir.join(MOD_DLL_NAME);

    fs::write(&destination, bytes).map_err(|e| e.to_string())?;

    emit_log(app, format!("Installed mod to {}", destination.display()));

    Ok(destination.to_string_lossy().to_string())
}

async fn check_mod(app: &AppHandle, vanilla_mode: bool) -> Result<(), String> {
    let (game_path, backup_path) = get_app_paths()?;
    let mod_dir = game_path.join(MOD_DLL_SUBPATH);
    let mod_dll = mod_dir.join(MOD_DLL_NAME);
    let backup_dll = backup_path.join(MOD_DLL_NAME);

    if vanilla_mode {
        if mod_dll.exists() {
            emit_log(app, "Moving mod files aside for vanilla play...");
            fs::create_dir_all(&backup_path).map_err(|e| e.to_string())?;
            fs::rename(&mod_dll, &backup_dll).map_err(|e| e.to_string())?;
        }
        move_extra_mod_files(&mod_dir, &backup_path)?;
        return launch(app, vanilla_mode);
    }

    let mut mod_up_to_date = false;

    if mod_dll.exists() {
        emit_log(app, "Checking installed mod version...");
        let client = reqwest::Client::new();
        let latest_bytes = fetch_latest_release_asset(&client).await?;

        mod_up_to_date = sha256_of_file(&mod_dll)? == sha256_hex(&latest_bytes);
        if mod_up_to_date {
            emit_log(app, "Mod is up to date.");
        }
    }

    if !mod_up_to_date {
        if backup_dll.exists() {
            emit_log(app, "Restoring cached mod files...");
            fs::rename(&backup_dll, &mod_dll).map_err(|e| e.to_string())?;
        } else {
            install_mod(app, &mod_dir).await?;
        }
    }

    move_extra_mod_files(&backup_path, &mod_dir)?;
    launch(app, vanilla_mode)
}

#[cfg(target_os = "windows")]
fn steam_command() -> Result<Command, String> {
    steam_root_candidates()
        .into_iter()
        .map(|root| root.join("steam.exe"))
        .find(|exe| exe.is_file())
        .map(Command::new)
        .ok_or_else(|| "Couldn't find Steam (steam.exe); is Steam installed?".to_string())
}

#[cfg(target_os = "linux")]
fn steam_command() -> Result<Command, String> {
    Ok(Command::new("steam"))
}

fn launch(app: &AppHandle, vanilla_mode: bool) -> Result<(), String> {
    emit_log(app, "Launching MCC...");

    let mut command = steam_command()?;

    if !vanilla_mode && cfg!(target_os = "windows") {
        command.arg("steam://launch/976730/option2");
    } else {
        command.args(["-applaunch", "976730"]);

        if !vanilla_mode {
            if cfg!(target_os = "linux") {
                command.env("WINEDLLOVERRIDES", "WTSAPI32=n,b");
            }
            command.arg("-eac");
        }
    }

    command.spawn().map_err(|e| e.to_string())?;

    Ok(())
}

#[tauri::command]
async fn latest_mod_version() -> Result<String, String> {
    let client = reqwest::Client::new();
    let release = fetch_latest_release(&client).await?;

    release["tag_name"]
        .as_str()
        .map(|tag| tag.to_string())
        .ok_or_else(|| "Release has no tag name".to_string())
}

#[tauri::command]
async fn play(app: AppHandle, vanilla_mode: bool) -> Result<(), String> {
    if let Err(err) = check_os(&app) {
        emit_log(&app, format!("Error: {err}"));
        return Err(err);
    }

    if let Err(err) = check_mod(&app, vanilla_mode).await {
        emit_log(&app, format!("Error: {err}"));
        return Err(err);
    }

    Ok(())
}

#[tauri::command]
fn open_install_dir(app: AppHandle) -> Result<(), String> {
    let exe = std::env::current_exe().map_err(|e| e.to_string())?;
    let dir = exe
        .parent()
        .ok_or_else(|| "Couldn't determine install directory".to_string())?;

    app.opener()
        .open_path(dir.to_string_lossy().to_string(), None::<&str>)
        .map_err(|e| e.to_string())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    #[cfg(target_os = "windows")]
    ensure_steam_api_dll_extracted();

    tauri::Builder::default()
        .setup(|app| {
            if let Err(err) = ensure_cfg_file() {
                eprintln!("Failed to create config file: {err}");
            }

            let fullscreen = std::env::args().any(|arg| arg == "-fullscreen");

            if fullscreen {
                let window = app.get_webview_window("main").unwrap();
                window.set_fullscreen(true)?;
            }

            Ok(())
        })
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![play, latest_mod_version, open_install_dir])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
