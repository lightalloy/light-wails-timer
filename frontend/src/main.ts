import "./style.css";
import { Events } from "@wailsio/runtime";
import { TimerService } from "../bindings/light-wails-timer";
import { TimerState } from "../bindings/events";
import { strings } from "./strings";

/** Snapshot emitted by the Go side on every tick and state change. */
interface Snapshot {
    status: "idle" | "running" | "paused";
    presetMinutes: number;
    remainingSec: number;
    totalSec: number;
    notice: string;
    autostart: boolean;
}

const RING_RADIUS = 52;
const RING_CIRCUMFERENCE = 2 * Math.PI * RING_RADIUS;

const timeElement = document.getElementById("time") as HTMLDivElement;
const ringElement = document.getElementById("ring") as SVGCircleElement;
const pauseButton = document.getElementById("pause") as HTMLButtonElement;
const stopButton = document.getElementById("stop") as HTMLButtonElement;
const noticeElement = document.getElementById("notice") as HTMLParagraphElement;
const presetButtons = Array.from(
    document.querySelectorAll<HTMLButtonElement>(".preset"),
);

ringElement.style.strokeDasharray = `${RING_CIRCUMFERENCE}`;

/** Formats seconds as MM:SS, clamping negatives to zero. */
export function formatRemaining(seconds: number): string {
    const safe = Math.max(0, Math.floor(seconds));
    const minutes = Math.floor(safe / 60);
    const rest = safe % 60;
    return `${String(minutes).padStart(2, "0")}:${String(rest).padStart(2, "0")}`;
}

/** Fraction of the interval that is still ahead (1 = full ring). */
export function remainingFraction(snapshot: Snapshot): number {
    if (
        (snapshot.status !== "running" && snapshot.status !== "paused") ||
        snapshot.totalSec <= 0
    ) {
        return 1;
    }
    return Math.min(1, Math.max(0, snapshot.remainingSec / snapshot.totalSec));
}

function render(snapshot: Snapshot): void {
    const active = snapshot.status === "running" || snapshot.status === "paused";

    document.body.dataset.status = snapshot.status;
    timeElement.textContent = active
        ? formatRemaining(snapshot.remainingSec)
        : strings.idleTime;

    ringElement.style.strokeDashoffset = `${RING_CIRCUMFERENCE * (1 - remainingFraction(snapshot))}`;

    pauseButton.disabled = !active;
    pauseButton.textContent =
        snapshot.status === "paused" ? strings.resume : strings.pause;
    stopButton.disabled = !active;

    if (snapshot.notice) {
        noticeElement.textContent = snapshot.notice;
        noticeElement.hidden = false;
    } else {
        noticeElement.textContent = "";
        noticeElement.hidden = true;
    }
}

function startTimer(minutes: number): void {
    void TimerService.Start(minutes).catch((error: unknown) => {
        console.error("start timer:", error);
    });
}

presetButtons.forEach((button) => {
    button.addEventListener("click", () => {
        startTimer(Number(button.dataset.minutes));
    });
});

pauseButton.addEventListener("click", () => {
    const paused = document.body.dataset.status === "paused";
    const action = paused ? TimerService.Resume() : TimerService.Pause();
    void action.catch((error: unknown) => {
        console.error(paused ? "resume timer:" : "pause timer:", error);
    });
});

stopButton.addEventListener("click", () => {
    void TimerService.Stop().catch((error: unknown) => {
        console.error("stop timer:", error);
    });
});

Events.On(TimerState, (event) => {
    render(event.data as Snapshot);
});

void TimerService.GetState()
    .then((state) => {
        render(state as Snapshot);
    })
    .catch((error: unknown) => {
        console.error("initial state:", error);
    });
