This directory must contain the Steamworks SDK redistributable native libraries, which are not checked into this repo:

- `steam_api64.dll` — from `sdk/redistributable_bin/win64/steam_api64.dll` in the Steamworks SDK
- `libsteam_api.so` — from `sdk/redistributable_bin/linux64/libsteam_api.so` in the Steamworks SDK

Download the SDK from a Steamworks partner account (free to create) at https://partner.steamgames.com/downloads/list, and copy the two files above into this directory. `go:embed` in `steamworks_windows.go` / `steamworks_linux.go` requires them to be present for the project to build.

Record the SDK version used here once added, since the exported symbol names/versions referenced in `steamworks.go` (`SteamAPI_SteamUGC_v021`, `SteamAPI_SteamUtils_v011`, and the `RemoteStorageSubscribePublishedFileResult_t` callback ID `1313`) are tied to the SDK version. These have been stable for years, but verify with `dumpbin /exports steam_api64.dll` (Windows) / `nm -D libsteam_api.so` (Linux) against the SDK actually downloaded.
