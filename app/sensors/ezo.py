"""
Atlas Scientific EZO I2C base driver.

Implements the request/wait/read protocol shared by all EZO circuits
(pH, EC, ORP, DO, RTD). The protocol over I2C:

  1. Write a command string (e.g. "R", "I", "Cal,mid,7.00").
  2. Wait for the circuit to process. Atlas publishes per-command
     processing times - R is the slowest (~900ms for pH, ~600ms for EC).
  3. Read up to 32 bytes from the device. The first byte is a status
     code:
       1   = success, data follows (null-terminated ASCII)
       2   = syntax error
       254 = still processing, request not ready
       255 = no data to send
     Anything else is treated as an error.

Datasheets:
  pH EZO:  https://files.atlas-scientific.com/pH_EZO_Datasheet.pdf
  EC EZO:  https://files.atlas-scientific.com/EC_EZO_Datasheet.pdf
"""

import logging
import time

try:
    from smbus2 import SMBus, i2c_msg
    SMBUS_AVAILABLE = True
except ImportError:
    SMBUS_AVAILABLE = False

logger = logging.getLogger(__name__)


class EZOError(Exception):
    """Base for all EZO error conditions."""


class EZONotReady(EZOError):
    """Status 254 - device is still processing the previous command."""


class EZONoData(EZOError):
    """Status 255 - no data available."""


class EZOSyntaxError(EZOError):
    """Status 2 - bad command syntax."""


class EZODevice:
    """One Atlas EZO circuit on the shared I2C bus.

    Thread-safety: not safe across threads. Wrap calls in a Lock if you
    will issue commands from multiple publish loops concurrently.
    """

    DEFAULT_DELAY = 0.3
    READ_DELAY = 0.9
    CAL_DELAY = 1.6

    def __init__(self, address: int, bus_num: int = 1):
        if not SMBUS_AVAILABLE:
            raise EZOError(
                "smbus2 is required for the real EZO driver. "
                "pip install smbus2 (already in requirements.txt)."
            )
        self.address = address
        self.bus_num = bus_num

    def command(self, cmd: str, delay: float = None) -> str:
        """Send a command and return the parsed response string.

        delay: seconds to wait before reading. If None, uses READ_DELAY
        for "R"/"RT" commands and DEFAULT_DELAY otherwise.
        """
        if delay is None:
            head = cmd.split(",")[0].upper()
            if head in ("R", "RT"):
                delay = self.READ_DELAY
            elif head == "CAL":
                delay = self.CAL_DELAY
            else:
                delay = self.DEFAULT_DELAY

        with SMBus(self.bus_num) as bus:
            write = i2c_msg.write(self.address, list(cmd.encode("ascii")))
            bus.i2c_rdwr(write)
            time.sleep(delay)
            read = i2c_msg.read(self.address, 32)
            bus.i2c_rdwr(read)
            raw = bytes(read)

        return self._parse(raw, cmd)

    def _parse(self, raw: bytes, cmd: str) -> str:
        if not raw:
            raise EZOError(f"{cmd}: empty response")
        status = raw[0]
        payload = bytes(b for b in raw[1:] if b != 0).decode("ascii", errors="replace").strip()
        if status == 1:
            return payload
        if status == 254:
            raise EZONotReady(f"{cmd}: still processing")
        if status == 255:
            raise EZONoData(f"{cmd}: no data to send")
        if status == 2:
            raise EZOSyntaxError(f"{cmd}: syntax error")
        raise EZOError(f"{cmd}: unexpected status byte {status} (payload {payload!r})")

    def info(self) -> str:
        """Returns e.g. '?I,pH,2.10' or '?I,EC,2.13'."""
        return self.command("I")

    def status(self) -> str:
        """Returns power-on reason + voltage, e.g. '?STATUS,P,5.038'."""
        return self.command("Status")

    def sleep(self):
        """Put the EZO to sleep (no response, no delay needed)."""
        with SMBus(self.bus_num) as bus:
            write = i2c_msg.write(self.address, list(b"Sleep"))
            bus.i2c_rdwr(write)

    def set_temperature_compensation(self, celsius: float):
        """Tell the EZO the current water temperature for compensation."""
        return self.command(f"T,{celsius:.2f}")
