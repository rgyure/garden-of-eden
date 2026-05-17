"""
Peristaltic dose-pump driver.

Three dose pumps are planned for the Garden of Eden hydroponic loop:
  - ph_down:   meters acid (typically pH-down solution) to reduce pH
  - nutrient_a: half of a 2-part nutrient
  - nutrient_b: the other half

Each pump is driven by an H-bridge channel (e.g. DRV8833) via two GPIOs.
For a one-direction peristaltic the convention is:
  forward_pin -> high while running
  reverse_pin -> low

Two modes:
  - Stub (default, DOSE_STUB=true): logs the call, sleeps briefly, never
    touches GPIO. Safe to run on dev machines.
  - Real (DOSE_STUB=false): drives the GPIO via gpiozero.

Calibrate FLOW_RATE_ML_PER_SEC per-pump against a graduated cylinder once
real pumps are wired.
"""

import logging
import sys
import os
import threading
import time

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '../../..')))
import config

logger = logging.getLogger(__name__)

MIN_DOSE_ML = 0.1
MAX_DOSE_SECONDS = 60  # safety cap per single dose


class DosePump:
    def __init__(self, name: str, forward_pin: int, reverse_pin: int,
                 flow_rate_ml_per_sec: float = 1.67, pin_factory=None):
        self.name = name
        self.forward_pin = forward_pin
        self.reverse_pin = reverse_pin
        self.flow_rate = flow_rate_ml_per_sec
        self.last_dose_at = 0.0
        self.last_dose_ml = 0.0
        self.is_running = False
        self._lock = threading.Lock()
        self._pin_factory = pin_factory
        self._hw = None
        if not config.DOSE_STUB:
            self._init_hw()

    def _init_hw(self):
        try:
            from gpiozero import OutputDevice
            self._hw = {
                "fwd": OutputDevice(self.forward_pin, pin_factory=self._pin_factory),
                "rev": OutputDevice(self.reverse_pin, pin_factory=self._pin_factory),
            }
        except Exception as e:
            logger.warning(f"DosePump[{self.name}]: failed to init GPIO ({e}); falling back to stub")
            self._hw = None

    def dose_ml(self, volume_ml: float, max_ml: float = None) -> dict:
        """Run the pump long enough to deliver `volume_ml`. Blocks until done."""
        volume_ml = float(volume_ml)
        if volume_ml < MIN_DOSE_ML:
            return {"ok": False, "reason": f"dose below {MIN_DOSE_ML} mL"}
        if max_ml is not None and volume_ml > max_ml:
            volume_ml = max_ml
        seconds = volume_ml / self.flow_rate
        if seconds > MAX_DOSE_SECONDS:
            seconds = MAX_DOSE_SECONDS
            volume_ml = seconds * self.flow_rate

        with self._lock:
            self.is_running = True
            try:
                logger.info(
                    f"DosePump[{self.name}]: dosing {volume_ml:.2f} mL "
                    f"({seconds:.2f}s @ {self.flow_rate} mL/s) "
                    f"{'(stub)' if self._is_stub() else ''}"
                )
                if not self._is_stub():
                    self._hw["rev"].off()
                    self._hw["fwd"].on()
                    time.sleep(seconds)
                    self._hw["fwd"].off()
                else:
                    time.sleep(min(seconds, 0.2))
                self.last_dose_at = time.time()
                self.last_dose_ml = volume_ml
            finally:
                self.is_running = False

        return {
            "ok":        True,
            "name":      self.name,
            "volume_ml": round(volume_ml, 2),
            "seconds":   round(seconds, 2),
            "stub":      self._is_stub(),
        }

    def _is_stub(self) -> bool:
        return config.DOSE_STUB or self._hw is None

    def status(self) -> dict:
        return {
            "name":          self.name,
            "running":       self.is_running,
            "flow_rate":     self.flow_rate,
            "last_dose_ml":  round(self.last_dose_ml, 2),
            "last_dose_at":  self.last_dose_at,
            "stub":          self._is_stub(),
        }


# Default GPIO assignments using free pins on the Pi. Verify these don't
# conflict when wiring the DRV8833.
DEFAULT_PUMPS = {
    "ph_down":     {"forward_pin": 26, "reverse_pin": 19, "flow_rate_ml_per_sec": 1.67},
    "nutrient_a":  {"forward_pin": 21, "reverse_pin": 20, "flow_rate_ml_per_sec": 1.67},
    "nutrient_b":  {"forward_pin": 16, "reverse_pin": 12, "flow_rate_ml_per_sec": 1.67},
}


def make_pumps(pin_factory=None) -> dict:
    return {
        name: DosePump(name, pin_factory=pin_factory, **kwargs)
        for name, kwargs in DEFAULT_PUMPS.items()
    }


if __name__ == "__main__":
    pumps = make_pumps()
    for name, pump in pumps.items():
        result = pump.dose_ml(2.0)
        print(name, result)
