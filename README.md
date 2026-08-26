# AlphaRing Launcher
<img width="1005" height="757" alt="image" src="https://github.com/user-attachments/assets/f0936361-728f-45af-9073-d6347b16b4ef" />
This project is a launcher for my AlphaRing mod for Halo The Master Chief Collection.
https://github.com/megabitt01/AlphaRing/tree/master-chief

## Installation
### Windows
Grab the latest .exe build from the Releases tab and it's ready to go.  The executable will generate a .cfg file next to it where you can specify a custom install path for the launcher to look for MCC in.
### Linux
Download the install.sh script.  Run `chmod +x ./install.sh \ ./install.sh` in the command line and the binary will install itself on your system.  It will also install protontricks and install VC redistributable as a patch for the "fatal error" issue.  To uninstall, run `./install.sh --uninstall` and it will remove the binary but leave its dependencies installed.  Currently, you'll have to create a .sh or .desktop file to start the launcher.

## Live Development

To run in live development mode, run `wails dev` in the project directory. This will run a Vite development
server that will provide very fast hot reload of your frontend changes. If you want to develop in a browser
and have access to your Go methods, there is also a dev server that runs on http://localhost:34115. Connect
to this in your browser, and you can call your Go code from devtools.

## Building

To build a redistributable, production mode package, use `wails build`.
For building on Linux, you may need to run `wails build -tags webkit2_41`.
