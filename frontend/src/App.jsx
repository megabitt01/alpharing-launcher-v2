import { useEffect, useRef, useState } from "react";
import { EventsOn, Quit } from "../wailsjs/runtime/runtime";
import { BackgroundVideoURLs, LatestModVersion, OpenInstallDir, Play } from "../wailsjs/go/main/App";
import alphaRingLogo from "./assets/logo_alpharing.png";
import bkgVideoMp4 from "./assets/bkgVideo.mp4";
import bkgVideoWebm from "./assets/bkgVideo.webm";
import "./App.css";

const CLOSE_DELAY_MS = 1000;
const ERROR_DISPLAY_MS = 2000;
const ACTION_TIMEOUT_MS = 10000;
const GAMEPAD_AXIS_DEADZONE = 0.5;
const VIDEO_MIME_TYPES = { mp4: "video/mp4", webm: "video/webm" };
const DEFAULT_VIDEO_SOURCES = [
  { src: bkgVideoWebm, type: VIDEO_MIME_TYPES.webm },
  { src: bkgVideoMp4, type: VIDEO_MIME_TYPES.mp4 },
];

const closeWindowDelayed = () => {
  setTimeout(() => Quit(), CLOSE_DELAY_MS);
};

const MENU_BUTTONS = [
  { label: "Play MCC with Anti-Cheat", action: () => Play(true), returnToMenu: true },
  { label: "Play Splitscreen Halo", action: () => Play(false), returnToMenu: true },
  { label: "Open Launcher Folder", startMessage: "Opening Folder...", action: () => OpenInstallDir(), returnToMenu: true },
  { label: "Quit to Desktop", startMessage: "Shutting Down...", action: () => closeWindowDelayed() },
];

function App({ buildInfo = "", modInfo: initialModInfo = "" }) {
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
  const forceRevertTimeoutRef = useRef(null);
  const videoRef = useRef(null);
  const [videoSources, setVideoSources] = useState(DEFAULT_VIDEO_SOURCES);

  useEffect(() => {
    LatestModVersion().then((tag) => setModInfo(`AlphaRing v${tag}`)).catch((error) => console.error("Error fetching latest AlphaRing version:", error));
  }, []);

  useEffect(() => {
    BackgroundVideoURLs().then((urls) => {
      if (!urls || urls.length === 0) return;
      setVideoSources(urls.map((url) => ({ src: url, type: VIDEO_MIME_TYPES[url.split(".").pop()] })));
    }).catch((error) => console.error("Error fetching background video URLs:", error));
  }, []);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const tryPlay = () => video.play().catch((error) => console.error("Error playing background video:", error));
    video.load();
    tryPlay();
    video.addEventListener("loadeddata", tryPlay);
    return () => video.removeEventListener("loadeddata", tryPlay);
  }, [videoSources]);

  const moveSelection = (delta) => setSelectedIndex((selectedIndexRef.current + delta + MENU_BUTTONS.length) % MENU_BUTTONS.length);

  const runAction = (index) => {
    if (errorRevertTimeoutRef.current) clearTimeout(errorRevertTimeoutRef.current);
    if (forceRevertTimeoutRef.current) clearTimeout(forceRevertTimeoutRef.current);
    setSelectedIndex(index);
    const button = buttonRefs.current[index];
    const container = backgroundRef.current;
    if (button && container) {
      const buttonRect = button.getBoundingClientRect();
      const containerRect = container.getBoundingClientRect();
      setLogPosition({ top: buttonRect.top - containerRect.top, left: buttonRect.left - containerRect.left, width: buttonRect.width });
    }
    setLog(MENU_BUTTONS[index].startMessage ?? "");
    setShowLog(true);

    // Safety net: no matter what the backend action does (hangs, errors,
    // takes longer than expected), the UI always becomes usable again after
    // ACTION_TIMEOUT_MS. `settled` stops a late resolution/rejection of the
    // original action from re-triggering the log panel after that.
    let settled = false;
    forceRevertTimeoutRef.current = setTimeout(() => {
      settled = true;
      setShowLog(false);
    }, ACTION_TIMEOUT_MS);

    Promise.resolve(MENU_BUTTONS[index].action()).then(() => {
      if (settled) return;
      clearTimeout(forceRevertTimeoutRef.current);
      if (MENU_BUTTONS[index].returnToMenu) setShowLog(false);
    }).catch((error) => {
      if (settled) return;
      clearTimeout(forceRevertTimeoutRef.current);
      console.error("Error:", error);
      setLog(`Error: ${error}`);
      errorRevertTimeoutRef.current = setTimeout(() => setShowLog(false), ERROR_DISPLAY_MS);
    });
  };

  useEffect(() => {
    const cancel = EventsOn("log", (message) => setLog(message));
    return () => cancel?.();
  }, []);

  useEffect(() => {
    const handleKeyDown = (event) => {
      switch (event.key) {
        case "ArrowUp": case "ArrowLeft": case "w": case "W": case "a": case "A":
          event.preventDefault(); moveSelection(-1); break;
        case "ArrowDown": case "ArrowRight": case "s": case "S": case "d": case "D":
          event.preventDefault(); moveSelection(1); break;
        case "Enter": case " ":
          event.preventDefault(); runAction(selectedIndexRef.current); break;
        default: break;
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  useEffect(() => {
    let frame;
    const held = { prev: false, next: false, confirm: false };
    const poll = () => {
      const pads = navigator.getGamepads ? navigator.getGamepads() : [];
      const gamepad = Array.prototype.find.call(pads, Boolean);
      if (gamepad) {
        const axisX = gamepad.axes[0] ?? 0;
        const axisY = gamepad.axes[1] ?? 0;
        const prev = gamepad.buttons[12]?.pressed || gamepad.buttons[14]?.pressed || axisY < -GAMEPAD_AXIS_DEADZONE || axisX < -GAMEPAD_AXIS_DEADZONE;
        const next = gamepad.buttons[13]?.pressed || gamepad.buttons[15]?.pressed || axisY > GAMEPAD_AXIS_DEADZONE || axisX > GAMEPAD_AXIS_DEADZONE;
        const confirm = gamepad.buttons[0]?.pressed ?? false;
        if (prev && !held.prev) moveSelection(-1);
        if (next && !held.next) moveSelection(1);
        if (confirm && !held.confirm) runAction(selectedIndexRef.current);
        held.prev = prev; held.next = next; held.confirm = confirm;
      }
      frame = requestAnimationFrame(poll);
    };
    frame = requestAnimationFrame(poll);
    return () => cancelAnimationFrame(frame);
  }, []);

  return (
    <main className="container">
      <div className="background" ref={backgroundRef}>
        <video
          ref={videoRef}
          className="background-video"
          autoPlay
          loop
          muted
          playsInline
          preload="auto"
          onError={(e) => console.log("VIDEO ERROR:", e.currentTarget.error)}
          onLoadedData={() => console.log("VIDEO LOADED")}
        >
          {videoSources.map((source) => <source key={source.src} src={source.src} type={source.type} />)}
        </video>
        <img className="logo" src={alphaRingLogo} alt="AlphaRing" />
        {showLog ? <div className="log-panel" style={logPosition ?? undefined}><p className="log-message">{log || "Starting Up..."}</p></div> : (
          <div className="menu">
            {MENU_BUTTONS.map((button, index) => <button key={button.label} ref={(element) => { buttonRefs.current[index] = element; }} type="button" className={`menu-button${index === selectedIndex ? " selected" : ""}`} onMouseEnter={() => setSelectedIndex(index)} onClick={() => runAction(index)}>{button.label}</button>)}
          </div>
        )}
        <p className="info">{buildInfo} {modInfo}</p>
      </div>
      <div>Fonts made from <a href="http://www.onlinewebfonts.com">Web Fonts</a> is licensed by CC BY 4.0</div>
    </main>
  );
}

export default App;
