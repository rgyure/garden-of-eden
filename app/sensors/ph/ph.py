"""
pH sensor driver.

Two modes:
  - Stub (default): emits realistic varying values around 6.0 so the rest of
    the stack can be wired up before the Atlas Scientific EZO-pH circuit is
    in hand.
  - Real (when PH_STUB=false): talks to an Atlas EZO-pH on the shared I2C bus.

The Atlas command protocol is text-based ("R\\r" to request a reading, response
arrives ~1s later). The placeholder implementation below is the minimum
boilerplate; refine once probes are calibrated.
"""

import math
import random
import sys
import os
import time

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '../../..')))
import config


class PHSensor:
    def read(self) -> float:
        raise NotImplementedError


class PHSensorStub(PHSensor):
    """Slowly oscillates around 6.0 with small noise so the dashboard moves."""
    def __init__(self):
        self._t0 = time.time()

    def read(self) -> float:
        elapsed = time.time() - self._t0
        baseline = 6.0 + 0.4 * math.sin(elapsed / 1800.0)  # ~30 min period
        noise = random.uniform(-0.05, 0.05)
        return round(baseline + noise, 2)


class PHSensorEZO(PHSensor):
    """Real Atlas Scientific EZO-pH driver.

    Issues the "R" command and parses the single floating-point value the
    EZO-pH returns. See app/sensors/ezo.py for protocol details.

    Calibration (do this once with fresh probes, every ~3 months thereafter):
        ph_sensor = PHSensorEZO()
        ph_sensor.calibrate('mid', 7.00)   # rinse, place in pH 7 buffer
        ph_sensor.calibrate('low', 4.00)   # rinse, place in pH 4 buffer
        ph_sensor.calibrate('high', 10.00) # rinse, place in pH 10 buffer
    """
    def __init__(self, address: int = None):
        from app.sensors.ezo import EZODevice
        self.address = address or config.PH_I2C_ADDRESS
        self._device = EZODevice(self.address)

    def read(self) -> float:
        raw = self._device.command("R")
        return float(raw)

    def calibrate(self, point: str, value: float) -> str:
        """point: 'mid' | 'low' | 'high'. Probe must be in the buffer first."""
        if point not in ("mid", "low", "high"):
            raise ValueError("point must be 'mid', 'low', or 'high'")
        return self._device.command(f"Cal,{point},{value:.2f}")

    def calibration_status(self) -> str:
        """Returns '?CAL,n' where n is 0,1,2,3 points calibrated."""
        return self._device.command("Cal,?")

    def set_temperature_compensation(self, celsius: float) -> str:
        return self._device.set_temperature_compensation(celsius)


def make_sensor() -> PHSensor:
    return PHSensorStub() if config.PH_STUB else PHSensorEZO()


ph_sensor = make_sensor()


if __name__ == "__main__":
    print(f"pH reading: {ph_sensor.read()}")
