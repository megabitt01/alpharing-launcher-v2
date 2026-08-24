import { useEffect, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { getCurrentWindow } from "@tauri-apps/api/window";
import alphaRingLogo from "./assets/logo_alpharing.png";
import bkgVideo from "./assets/bkgVideo.mp4";
import "./App.css";

const CLOSE_DELAY_MS = 1000;
const ERROR_DISPLAY_MS = 2000;

const closeWindowDelayed = () => {
  setTimeout(() => getCurrentWindow().close(), CLOSE_DELAY_MS);
};

const launchVanilla = async () => {
  await invoke("play", {
    vanillaMode: true
  })
  closeWindowDelayed();
}

const launchModded = async () => {
  await invoke("play", {
    vanillaMode: false
  })
  closeWindowDelayed();
}

const openInstallDir = async () => {
  await invoke("open_install_dir");
}

const MENU_BUTTONS = [
  { label: "Play MCC with Anti-Cheat", action: () => launchVanilla() },
  { label: "Play Splitscreen Halo", action: () => launchModded() },
  {
    label: "Open Launcher Folder",
    startMessage: "Opening Folder...",
    action: () => openInstallDir(),
    returnToMenu: true,
  },
  {
    label: "Quit to Desktop",
    startMessage: "Shutting Down...",
    action: () => closeWindowDelayed(),
  },
];

const GAMEPAD_AXIS_DEADZONE = 0.5;

function App({buildInfo = "", modInfo: initialModInfo = ""}) {
  const [selectedIndex, setSelectedIndex] = useState(0);
  const selectedIndexRef = useRef(selectedIndex);
  selectedIndexRef.current = selectedIndex;

  const [showLog, setShowLog] = useState(false);
  const [log, setLog] = useState("");
  const [modInfo, setModInfo] = useState(initialModInfo);
  const [logPosition, setLogPosition] = useState(null);
  const backgroundRef = useRef(null);
  const buttonRefs = useRef([]);
  const errorRevertTimeoutRef = useRef(null);

  useEffect(() => {
    invoke("latest_mod_version")
      .then((tag) => setModInfo(`AlphaRing v${tag}`))
      .catch((err) => console.error("Error fetching latest AlphaRing version: ", err));
  }, []);

  const changeSelection = (index) => {
    setSelectedIndex((prev) => {
      return index;
    });
  };

  const moveSelection = (delta) => {
    changeSelection((selectedIndexRef.current + delta + MENU_BUTTONS.length) % MENU_BUTTONS.length);
  };

  const runAction = (index) => {
    if (errorRevertTimeoutRef.current) {
      clearTimeout(errorRevertTimeoutRef.current);
      errorRevertTimeoutRef.current = null;
    }

    setSelectedIndex(index);

    const button = buttonRefs.current[index];
    const container = backgroundRef.current;
    if (button && container) {
      const buttonRect = button.getBoundingClientRect();
      const containerRect = container.getBoundingClientRect();
      setLogPosition({
        top: buttonRect.top - containerRect.top,
        left: buttonRect.left - containerRect.left,
        width: buttonRect.width,
      });
    }

    setLog(MENU_BUTTONS[index].startMessage ?? "");
    setShowLog(true);

    Promise.resolve(MENU_BUTTONS[index].action())
      .then(() => {
        if (MENU_BUTTONS[index].returnToMenu) {
          setShowLog(false);
        }
      })
      .catch((err) => {
        console.error("Error: ", err);
        setLog(`Error: ${err}`);
        errorRevertTimeoutRef.current = setTimeout(() => setShowLog(false), ERROR_DISPLAY_MS);
      });
  };

  const activateSelection = () => {
    runAction(selectedIndexRef.current);
  };

  useEffect(() => {
    let unlisten;

    listen("log", (event) => setLog(event.payload)).then((fn) => {
      unlisten = fn;
    });

    return () => unlisten?.();
  }, []);

  useEffect(() => {
    const handleKeyDown = (e) => {
      switch (e.key) {
        case "ArrowUp":
        case "ArrowLeft":
        case "w":
        case "W":
        case "a":
        case "A":
          e.preventDefault();
          moveSelection(-1);
          break;
        case "ArrowDown":
        case "ArrowRight":
        case "s":
        case "S":
        case "d":
        case "D":
          e.preventDefault();
          moveSelection(1);
          break;
        case "Enter":
        case " ":
          e.preventDefault();
          activateSelection();
          break;
        default:
          break;
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  useEffect(() => {
    let rafId;
    const wasHeld = { prev: false, next: false, confirm: false };

    const poll = () => {
      const pads = navigator.getGamepads ? navigator.getGamepads() : [];
      const gamepad = Array.prototype.find.call(pads, Boolean);

      if (gamepad) {
        const axisX = gamepad.axes[0] ?? 0;
        const axisY = gamepad.axes[1] ?? 0;
        const dpadUp = gamepad.buttons[12]?.pressed ?? false;
        const dpadDown = gamepad.buttons[13]?.pressed ?? false;
        const dpadLeft = gamepad.buttons[14]?.pressed ?? false;
        const dpadRight = gamepad.buttons[15]?.pressed ?? false;
        const faceDown = gamepad.buttons[0]?.pressed ?? false;

        const prevHeld = dpadUp || dpadLeft || axisY < -GAMEPAD_AXIS_DEADZONE || axisX < -GAMEPAD_AXIS_DEADZONE;
        const nextHeld = dpadDown || dpadRight || axisY > GAMEPAD_AXIS_DEADZONE || axisX > GAMEPAD_AXIS_DEADZONE;

        if (prevHeld && !wasHeld.prev) moveSelection(-1);
        if (nextHeld && !wasHeld.next) moveSelection(1);
        if (faceDown && !wasHeld.confirm) activateSelection();

        wasHeld.prev = prevHeld;
        wasHeld.next = nextHeld;
        wasHeld.confirm = faceDown;
      }

      rafId = requestAnimationFrame(poll);
    };

    rafId = requestAnimationFrame(poll);
    return () => cancelAnimationFrame(rafId);
  }, []);

  return (
    <main className="container">
      <div className="background" ref={backgroundRef}>
        <video className="background-video" src={bkgVideo} autoPlay loop muted playsInline/>
        <img className="logo" src={alphaRingLogo}/>
        {showLog ? (
          <div className="log-panel" style={logPosition ?? undefined}>
            <p className="log-message">{log || "Starting Up..."}</p>
          </div>
        ) : (
          <div className="menu">
            {MENU_BUTTONS.map((button, index) => (
              <button
                key={button.label}
                ref={(el) => { buttonRefs.current[index] = el; }}
                type="button"
                className={`menu-button${index === selectedIndex ? " selected" : ""}`}
                onMouseEnter={() => changeSelection(index)}
                onClick={() => runAction(index)}
              >
                {button.label}
              </button>
            ))}
          </div>
        )}
          <p className="info">{buildInfo} {modInfo}</p>
      </div>
      <div>Fonts made from <a href="http://www.onlinewebfonts.com">Web Fonts</a> is licensed by CC BY 4.0</div>
    </main>
  );
}

export default App;
