"""GPU readings from nvidia-smi, when there is one to read.

One nvidia-smi call costs about 70ms, so calling it once a second would spend
7% of a core to measure how much CPU the run is using - the sampler would
pollute its own numbers. It runs once in loop mode instead and a reader thread
keeps the last line.
"""

from __future__ import annotations

import shutil
import subprocess
import threading

_QUERY = "utilization.gpu,memory.used"
# Pinned to the first device: the reading is meant to say what the run's box was
# doing, not to survey a multi-gpu host.
_ARGS = ["--query-gpu=" + _QUERY, "--format=csv,noheader,nounits", "-i", "0"]


class GPUReader:
    """Last reading from a long-running nvidia-smi, or None when absent.

    utilization is the whole device's, not this process's - nvidia-smi reports
    no per-process utilization, so another process on the same card shows up
    here too. Memory is the device's used total for the same reason.
    """

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._latest: tuple[float, int] | None = None
        self._proc: subprocess.Popen[str] | None = None

    def start(self) -> bool:
        exe = shutil.which("nvidia-smi")
        if exe is None:
            return False
        try:
            probe = subprocess.run(  # noqa: S603
                [exe, *_ARGS], capture_output=True, text=True, timeout=5, check=False
            )
        except (OSError, subprocess.SubprocessError):
            return False
        if probe.returncode != 0 or not _parse(probe.stdout):
            return False
        self._latest = _parse(probe.stdout)
        try:
            self._proc = subprocess.Popen(  # noqa: S603
                [exe, *_ARGS, "-l", "1"],
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                text=True,
            )
        except (OSError, subprocess.SubprocessError):
            return False
        threading.Thread(target=self._pump, daemon=True).start()
        return True

    def _pump(self) -> None:
        proc = self._proc
        if proc is None or proc.stdout is None:
            return
        for line in proc.stdout:
            reading = _parse(line)
            if reading is None:
                continue
            with self._lock:
                self._latest = reading

    def reading(self) -> tuple[float, int] | None:
        with self._lock:
            return self._latest

    def stop(self) -> None:
        if self._proc is not None:
            self._proc.terminate()
            self._proc = None


def _parse(text: str) -> tuple[float, int] | None:
    """Turn "19, 1330" into a utilization fraction and a byte count."""
    line = text.strip().splitlines()[0] if text.strip() else ""
    parts = [p.strip() for p in line.split(",")]
    if len(parts) != 2:
        return None
    try:
        return int(parts[0]) / 100, int(parts[1]) * 1024 * 1024
    except ValueError:
        return None
