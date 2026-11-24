# AlphaRing Launcher
<img width="1005" height="757" alt="image" src="https://github.com/user-attachments/assets/f0936361-728f-45af-9073-d6347b16b4ef" />
This project is a launcher for my AlphaRing mod for Halo The Master Chief Collection.
https://github.com/megabitt01/AlphaRing/tree/master-chief

## Installation
### Windows
Grab the latest .exe build from the Releases tab and it's ready to go.  The executable will generate a .cfg file next to it where you can specify a custom install path for the launcher to look for MCC in.
### Linux
Download the install.sh script.  Run `chmod +x ./install.sh \ ./install.sh` in the command line and the binary will install itself on your system.  It will also install protontricks and install VC redistributable as a patch for the "fatal error" issue.  To uninstall, run `./install.sh --uninstall` and it will remove the binary but leave its dependencies installed.  Currently, you'll have to create a .sh or .desktop file to start the launcher.

## Command-line flags
- `-fullscreen` starts the launcher window in fullscreen.
- `-wait` keeps the launcher process alive (hidden) until MCC exits instead of quitting as soon as Steam has been asked to launch the game. Use this when starting the launcher from a frontend that tracks the launcher process, such as Playnite or a Steam non-Steam shortcut, so the frontend shows the game as running for the whole session and doesn't take focus back when the launcher hands off to Steam. On Windows the launcher also hands keyboard focus to the game's window as it appears, since a game started by Steam isn't otherwise allowed to take the foreground once the launcher has hidden itself.

### Playnite
Add the launcher as a game with a **File** play action pointing at the launcher executable, arguments `-fullscreen -wait`, and tracking mode **Original process**. The launcher stays running until MCC closes, so Playnite records the whole session and only returns to its UI afterwards.

## Live Development

To run in live development mode, run `wails dev` in the project directory. This will run a Vite development
server that will provide very fast hot reload of your frontend changes. If you want to develop in a browser
and have access to your Go methods, there is also a dev server that runs on http://localhost:34115. Connect
to this in your browser, and you can call your Go code from devtools.

## Building

To build a redistributable, production mode package, use `wails build`.
For building on Linux, you may need to run `wails build -tags webkit2_41`.

## Todo
For Windows:
The AlphaRing pipeline needs to start publishing WTSAPI32.dll.sha256 in order for fixes to work otherwise, users will get a Windows Defender issue.