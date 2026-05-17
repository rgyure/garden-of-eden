"""
EC (electrical conductivity) sensor driver.

Two modes:
  - Stub (default): emits realistic varying values around 1.5 mS/cm.
  - Real (when EC_STUB=false): talks to an Atlas Scientific EZO-EC circuit.

EZO-EC reports: EC (mS/cm), TDS (ppm), salinity (PSU), specific gravity (SG).
"""

import math
import random
import sys
import os
import time

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '../../..')))
import config


class ECSensor:
    def read(self) -> dict:
        raise NotImplementedError


class ECSensorStub(ECSensor):
    """Slowly oscillates around 1.5 mS/cm so the dashboard moves."""
    def __init__(self):
        self._t0 = time.time()

    def read(self) -> dict:
        elapsed = time.time() - self._t0
        ec_ms = 1.5 + 0.3 * math.sin(elapsed / 2400.0)  # ~40 min period
        ec_ms += random.uniform(-0.02, 0.02)
        ec_ms = max(0.0, round(ec_ms, 2))
        return {
            "ec":       ec_ms,
            "tds":      round(ec_ms * 500, 0),
            "salinity": round(ec_ms * 0.55, 2),
            "sg":       round(1.0 + ec_ms * 0.0004, 4),
        }


class ECSensorEZO(ECSensor):
    """Real Atlas Scientific EZO-EC driver.

    The EZO-EC returns a comma-delimited string controlled by its 'O' (output)
    parameters. Factory default is "EC,TDS,SAL,SG" - we keep that default and
    parse all four fields.

    Calibration (one-time, with probe rinsed before each step):
        ec_sensor = ECSensorEZO()
        ec_sensor.calibrate_dry()              # probe in air, completely dry
        ec_sensor.calibrate_single(1413)       # single-point @ 1413 uS/cm
        # OR for two-point (more accurate across range):
        ec_sensor.calibrate_low(12880)         # 12880 uS/cm buffer
        ec_sensor.calibrate_high(150000)       # 150000 uS/cm buffer (rare)
    """
    def __init__(self, address: int = None):
        from app.sensors.ezo import EZODevice
        self.address = address or config.EC_I2C_ADDRESS
        self._device = EZODevice(self.address)

    def read(self) -> dict:
        raw = self._device.command("R")
        # Default output: "ec,tds,sal,sg"  e.g. "1413.0,706,0.7,1.000"
        parts = raw.split(",")
        ec_us = self._to_float(parts, 0)
        tds = self._to_float(parts, 1)
        sal = self._to_float(parts, 2)
        sg = self._to_float(parts, 3)
        return {
            "ec": round(ec_us / 1000.0, 3) if ec_us is not None else None,  # mS/cm
            "tds": tds,
            "salinity": sal,
            "sg": sg,
        }

    @staticmethod
    def _to_float(parts, idx):
        if idx >= len(parts):
            return None
        try:
            return float(parts[idx])
        except (ValueError, TypeError):
            return None

    def calibrate_dry(self) -> str:
        return self._device.command("Cal,dry")

    def calibrate_single(self, microsiemens_per_cm: int) -> str:
        return self._device.command(f"Cal,{int(microsiemens_per_cm)}")

    def calibrate_low(self, microsiemens_per_cm: int) -> str:
        return self._device.command(f"Cal,low,{int(microsiemens_per_cm)}")

    def calibrate_high(self, microsiemens_per_cm: int) -> str:
        return self._device.command(f"Cal,high,{int(microsiemens_per_cm)}")

    def calibration_status(self) -> str:
        return self._device.command("Cal,?")

    def set_probe_k_value(self, k: float) -> str:
        """Set the K constant of the probe (default K=1.0)."""
        return self._device.command(f"K,{k:.2f}")

    def set_temperature_compensation(self, celsius: float) -> str:
        return self._device.set_temperature_compensation(celsius)


def make_sensor() -> ECSensor:
    return ECSensorStub() if config.EC_STUB else ECSensorEZO()


ec_sensor = make_sensor()


if __name__ == "__main__":
    print(f"EC reading: {ec_sensor.read()}")
