#!/usr/bin/env bash
#   ./install.sh                 # install system-wide to /usr/local/bin (uses sudo)
#   ./install.sh --user          # install to ~/.local/bin, no sudo/root required
#   ./install.sh --uninstall           # remove the system-wide install
#   ./install.sh --uninstall --user    # remove the ~/.local/bin install
set -euo pipefail

APPID="976730"
VCREDIST_URL="https://aka.ms/vs/17/release/vc_redist.x64.exe"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
COMMAND_NAME="alpharing"
REPO="megabitt01/alpharing-launcher-v2"
RELEASE_ASSET="alpharing-launcher-v2"

DOWNLOADED_TMP_DIR=""
cleanup() {
    [ -n "$DOWNLOADED_TMP_DIR" ] && rm -rf "$DOWNLOADED_TMP_DIR"
}
trap cleanup EXIT

USER_INSTALL=0
UNINSTALL=0
for arg in "$@"; do
    case "$arg" in
        --user) USER_INSTALL=1 ;;
        --uninstall) UNINSTALL=1 ;;
        *)
            echo "Unknown option: $arg" >&2
            echo "Usage: $0 [--user] [--uninstall]" >&2
            exit 1
            ;;
    esac
done

if [ "$USER_INSTALL" -eq 1 ]; then
    INSTALL_DIR="$HOME/.local/bin"
    ICON_DIR="$HOME/.local/share/icons/hicolor/128x128/apps"
    DESKTOP_DIR="$HOME/.local/share/applications"
else
    INSTALL_DIR="/usr/local/bin"
    ICON_DIR="/usr/share/icons/hicolor/128x128/apps"
    DESKTOP_DIR="/usr/share/applications"
fi
DESKTOP_FILE="$DESKTOP_DIR/$COMMAND_NAME.desktop"
ICON_FILE="$ICON_DIR/$COMMAND_NAME.png"

if [ "$USER_INSTALL" -eq 1 ] || [ "$(id -u)" -eq 0 ]; then
    BIN_SUDO=""
else
    BIN_SUDO="sudo"
fi

if [ "$(id -u)" -eq 0 ]; then
    SUDO=""
else
    SUDO="sudo"
fi

if [ "$UNINSTALL" -eq 1 ]; then
    TARGET="$INSTALL_DIR/$COMMAND_NAME"
    if [ -e "$TARGET" ] || [ -L "$TARGET" ]; then
        $BIN_SUDO rm -f "$TARGET"
        echo "Removed $TARGET"
    else
        echo "'$COMMAND_NAME' is not installed at $TARGET (nothing to do)."
        OTHER_DIR="/usr/local/bin"
        [ "$USER_INSTALL" -eq 1 ] || OTHER_DIR="$HOME/.local/bin"
        if [ -e "$OTHER_DIR/$COMMAND_NAME" ]; then
            echo "Note: found an install at $OTHER_DIR/$COMMAND_NAME instead."
            if [ "$USER_INSTALL" -eq 1 ]; then
                echo "Run: $0 --uninstall (without --user) to remove it."
            else
                echo "Run: $0 --uninstall --user to remove it."
            fi
        fi
    fi

    $BIN_SUDO rm -f "$DESKTOP_FILE" "$ICON_FILE"
    command -v update-desktop-database >/dev/null 2>&1 && $BIN_SUDO update-desktop-database "$DESKTOP_DIR" >/dev/null 2>&1
    command -v gtk-update-icon-cache >/dev/null 2>&1 && $BIN_SUDO gtk-update-icon-cache -f -t "$(dirname "$(dirname "$ICON_DIR")")" >/dev/null 2>&1

    echo "Dependencies (GTK3, WebKit2GTK 4.1) and the VC++ redistributable/protontricks setup were left untouched."
    exit 0
fi

# locate binary
BINARY_SRC=""
for candidate in \
    "$SCRIPT_DIR/build/bin/alpharing-launcher-v2" \
    "$SCRIPT_DIR/bin/alpharing-launcher-v2"; do
    if [ -f "$candidate" ]; then
        BINARY_SRC="$candidate"
        break
    fi
done

BRANCH="main"
download_release_binary() {
    local download_url="https://raw.githubusercontent.com/$REPO/$BRANCH/$RELEASE_ASSET"
    echo "No local build found. Downloading binary from GitHub ($download_url)..."

    DOWNLOADED_TMP_DIR="$(mktemp -d)"
    local dest="$DOWNLOADED_TMP_DIR/$RELEASE_ASSET"
    if ! curl -fL --output "$dest" "$download_url"; then
        echo "error: failed to download $download_url" >&2
        rm -rf "$DOWNLOADED_TMP_DIR"
        DOWNLOADED_TMP_DIR=""
        return 1
    fi

    chmod +x "$dest"
    BINARY_SRC="$dest"
}

if [ -z "$BINARY_SRC" ]; then
    download_release_binary || true
fi

if [ -z "$BINARY_SRC" ]; then
    echo "error: could not find or download the alpharing-launcher-v2 binary." >&2
    echo "Build it first with: wails build -tags webkit2_41" >&2
    exit 1
fi

echo "Found binary: $BINARY_SRC"

# install deps
install_deps() {
    if command -v apt-get >/dev/null 2>&1; then
        echo "Installing dependencies via apt..."
        $SUDO apt-get update
        $SUDO apt-get install -y libgtk-3-0 libwebkit2gtk-4.1-0
    elif command -v dnf >/dev/null 2>&1; then
        echo "Installing dependencies via dnf..."
        $SUDO dnf install -y gtk3 webkit2gtk4.1
    elif command -v pacman >/dev/null 2>&1; then
        echo "Installing dependencies via pacman..."
        $SUDO pacman -S --needed --noconfirm gtk3 webkit2gtk-4.1
    elif command -v zypper >/dev/null 2>&1; then
        echo "Installing dependencies via zypper..."
        $SUDO zypper install -y gtk3 webkit2gtk4.1
    else
        echo "warning: could not detect a supported package manager (apt/dnf/pacman/zypper)." >&2
        echo "Please make sure GTK3 and WebKit2GTK 4.1 runtime libraries are installed manually." >&2
    fi
}

install_deps

# install binary
$BIN_SUDO mkdir -p "$INSTALL_DIR"
$BIN_SUDO install -m 755 "$BINARY_SRC" "$INSTALL_DIR/$COMMAND_NAME"

echo "Installed '$COMMAND_NAME' to $INSTALL_DIR"

# install desktop entry + icon (so the window manager's taskbar shows the
# real icon instead of falling back to a generic binary icon - most Linux
# desktops, notably KDE Plasma and GNOME, resolve the taskbar icon via the
# .desktop file's Icon=, matched to the running window through its WM_CLASS,
# rather than the raw icon pixmap the window itself reports)
ICON_SRC="$SCRIPT_DIR/build/appicon.png"
if [ ! -f "$ICON_SRC" ]; then
    ICON_SRC=""
    [ -n "$DOWNLOADED_TMP_DIR" ] || DOWNLOADED_TMP_DIR="$(mktemp -d)"
    CANDIDATE_ICON="$DOWNLOADED_TMP_DIR/appicon.png"
    if curl -fsSL --output "$CANDIDATE_ICON" "https://raw.githubusercontent.com/$REPO/$BRANCH/build/appicon.png"; then
        ICON_SRC="$CANDIDATE_ICON"
    fi
fi

$BIN_SUDO mkdir -p "$ICON_DIR" "$DESKTOP_DIR"
if [ -n "$ICON_SRC" ]; then
    $BIN_SUDO install -m 644 "$ICON_SRC" "$ICON_FILE"
else
    echo "warning: could not find appicon.png; skipping icon install." >&2
fi
TMP_DESKTOP="$(mktemp)"
cat > "$TMP_DESKTOP" <<EOF
[Desktop Entry]
Type=Application
Name=AlphaRing Launcher
Comment=Splitscreen mod launcher for Halo: The Master Chief Collection
Exec=$COMMAND_NAME
Icon=$COMMAND_NAME
Categories=Game;
Terminal=false
StartupWMClass=$COMMAND_NAME
EOF
$BIN_SUDO install -m 644 "$TMP_DESKTOP" "$DESKTOP_FILE"
rm -f "$TMP_DESKTOP"

command -v update-desktop-database >/dev/null 2>&1 && $BIN_SUDO update-desktop-database "$DESKTOP_DIR" >/dev/null 2>&1
command -v gtk-update-icon-cache >/dev/null 2>&1 && $BIN_SUDO gtk-update-icon-cache -f -t "$(dirname "$(dirname "$ICON_DIR")")" >/dev/null 2>&1

echo "Installed desktop entry and icon ($DESKTOP_FILE, $ICON_FILE)"

# installs vc redistributable
PROTONTRICKS_LAUNCH_CMD=()
install_protontricks_flatpak() {
    if ! command -v flatpak >/dev/null 2>&1; then
        echo "flatpak not found; cannot fall back to the flatpak protontricks package." >&2
        return 1
    fi

    echo "Attempting to install protontricks via flatpak (com.github.Matoking.protontricks)..."
    if ! flatpak install -y --noninteractive flathub com.github.Matoking.protontricks; then
        echo "Failed to install protontricks via flatpak." >&2
        return 1
    fi

    PROTONTRICKS_LAUNCH_CMD=(flatpak run --command=protontricks-launch com.github.Matoking.protontricks)
}

install_vcredist() {
    if ! command -v protontricks-launch >/dev/null 2>&1; then
        echo "protontricks not found, installing..."
        if command -v apt-get >/dev/null 2>&1; then
            $SUDO apt-get install -y protontricks
        elif command -v dnf >/dev/null 2>&1; then
            $SUDO dnf install -y protontricks
        elif command -v pacman >/dev/null 2>&1; then
            $SUDO pacman -S --needed --noconfirm protontricks
        elif command -v zypper >/dev/null 2>&1; then
            $SUDO zypper install -y protontricks
        fi
    fi

    if command -v protontricks-launch >/dev/null 2>&1; then
        PROTONTRICKS_LAUNCH_CMD=(protontricks-launch)
    elif ! install_protontricks_flatpak; then
        echo "Could not install protontricks automatically." >&2
        echo "Try installing it manually (e.g. pipx install protontricks, or flatpak install flathub com.github.Matoking.protontricks) then re-run this script." >&2
        return 1
    fi

    local prefix=""
    for candidate in \
        "$HOME/.local/share/Steam/steamapps/compatdata/$APPID" \
        "$HOME/.var/app/com.valvesoftware.Steam/.local/share/Steam/steamapps/compatdata/$APPID"; do
        if [ -d "$candidate" ]; then
            prefix="$candidate"
            break
        fi
    done

    if [ -z "$prefix" ]; then
        echo "No Proton prefix found for AppID $APPID." >&2
        echo "Launch Halo: The Master Chief Collection at least once via Steam, then re-run this script." >&2
        return 1
    fi

    echo "Found Proton prefix: $prefix"

    local tmp_dir
    tmp_dir="$(mktemp -d)"
    local installer="$tmp_dir/vc_redist.x64.exe"

    echo "Downloading vc_redist.x64.exe..."
    if ! curl -fL --output "$installer" "$VCREDIST_URL"; then
        echo "Failed to download $VCREDIST_URL" >&2
        rm -rf "$tmp_dir"
        return 1
    fi

    echo "Installing VC++ redistributable into the AppID $APPID prefix..."
    if ! "${PROTONTRICKS_LAUNCH_CMD[@]}" --appid "$APPID" "$installer" /install /quiet /norestart; then
        echo "protontricks-launch failed to install the VC++ redistributable." >&2
        rm -rf "$tmp_dir"
        return 1
    fi

    rm -rf "$tmp_dir"
    echo "VC++ redistributable installed into the Halo: The Master Chief Collection Proton prefix."
}

if ! install_vcredist; then
    echo "warning: skipped VC++ redistributable setup (see above). The launcher itself is still installed." >&2
fi

if [ "$USER_INSTALL" -eq 1 ] && [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo
    echo "warning: $INSTALL_DIR is not on your PATH."
    echo "Add this to your shell profile (e.g. ~/.bashrc or ~/.zshrc):"
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi

echo
echo "Run it with: $COMMAND_NAME"
echo "To uninstall: $0 --uninstall$([ "$USER_INSTALL" -eq 1 ] && echo ' --user')"
