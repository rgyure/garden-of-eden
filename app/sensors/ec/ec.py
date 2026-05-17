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

    TODO: when hardware arrives, implement real I2C reads. EZO-EC datasheet:
    https://files.atlas-scientific.com/EC_EZO_Datasheet.pdf
    """
    def __init__(self, address: int = None):
        self.address = address or config.EC_I2C_ADDRESS

    def read(self) -> dict:
        raise NotImplementedError(
            "Real EZO-EC driver not yet implemented. "
            "Set EC_STUB=true in .env until hardware is wired."
        )


def make_sensor() -> ECSensor:
    return ECSensorStub() if config.EC_STUB else ECSensorEZO()


ec_sensor = make_sensor()


if __name__ == "__main__":
    print(f"EC reading: {ec_sensor.read()}")
