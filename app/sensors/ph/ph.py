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

    TODO: when hardware arrives, swap the body of read() with real I2C calls.
    The EZO protocol over I2C is documented at
    https://files.atlas-scientific.com/pH_EZO_Datasheet.pdf
    """
    def __init__(self, address: int = None):
        self.address = address or config.PH_I2C_ADDRESS

    def read(self) -> float:
        raise NotImplementedError(
            "Real EZO-pH driver not yet implemented. "
            "Set PH_STUB=true in .env until hardware is wired."
        )


def make_sensor() -> PHSensor:
    return PHSensorStub() if config.PH_STUB else PHSensorEZO()


ph_sensor = make_sensor()


if __name__ == "__main__":
    print(f"pH reading: {ph_sensor.read()}")
