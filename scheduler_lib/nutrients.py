"""
Nutrient reconciler.

Bang-bang controller with per-pump cooldowns:
  - pH > target_max     -> dose pH-down (cooldown)
  - EC < target_min     -> dose nutrient A + B (cooldown)
  - pH < target_min     -> log only (no pH-up pump in default install)

Cooldown state is in-memory; restarting the scheduler resets it.
The controller never touches GPIO directly. Doses are published as MQTT
commands which mqtt.py routes to the DosePump driver. This keeps the
hardware vs. control layer separation that the rest of the project uses.
"""

import logging
import time

logger = logging.getLogger(__name__)


class NutrientController:
    def __init__(self, client, base_topic):
        self.client = client
        self.base_topic = base_topic

        # Current sensor readings (set by Reconciler.update_state).
        self.ph = None
        self.ec = None

        # Per-pump last-dose timestamp (epoch seconds).
        self._last_dose = {
            "ph_down":    0.0,
            "nutrient_a": 0.0,
            "nutrient_b": 0.0,
            "cal_mag":    0.0,
        }

    def update_ph(self, ph: float):
        self.ph = ph

    def update_ec(self, ec: float):
        self.ec = ec

    def tick(self, config: dict):
        """Run one reconciliation cycle. No-op if nutrients are disabled."""
        nutrients = config.get("nutrients") or {}
        if not nutrients.get("enabled"):
            return

        self._reconcile_ph(nutrients.get("ph") or {}, config)
        self._reconcile_ec(nutrients.get("ec") or {}, config)

    def _reconcile_ph(self, ph_cfg: dict, config: dict):
        if self.ph is None:
            return
        target_min = ph_cfg.get("target_min", 5.8)
        target_max = ph_cfg.get("target_max", 6.2)
        dose_ml = ph_cfg.get("dose_ml", 1.0)
        cooldown = ph_cfg.get("cooldown_minutes", 30) * 60

        if self.ph > target_max and self._cooldown_ok("ph_down", cooldown):
            if self._pump_enabled(config, "ph_down"):
                logger.info(
                    f"NutrientController: pH={self.ph:.2f} above max {target_max:.2f}, "
                    f"dosing {dose_ml:.2f} mL pH-down"
                )
                self._dispatch("ph_down", dose_ml)
        elif self.ph < target_min:
            logger.warning(
                f"NutrientController: pH={self.ph:.2f} below min {target_min:.2f} - "
                f"no pH-up pump configured, manual intervention required"
            )

    def _reconcile_ec(self, ec_cfg: dict, config: dict):
        if self.ec is None:
            return
        target_min = ec_cfg.get("target_min", 1.2)
        target_max = ec_cfg.get("target_max", 1.8)
        dose_a_ml = ec_cfg.get("dose_a_ml", 5.0)
        dose_b_ml = ec_cfg.get("dose_b_ml", 5.0)
        cooldown = ec_cfg.get("cooldown_minutes", 30) * 60

        if self.ec < target_min:
            for name, vol in (("nutrient_a", dose_a_ml), ("nutrient_b", dose_b_ml)):
                if not self._cooldown_ok(name, cooldown):
                    continue
                if not self._pump_enabled(config, name):
                    continue
                logger.info(
                    f"NutrientController: EC={self.ec:.2f} below min {target_min:.2f}, "
                    f"dosing {vol:.2f} mL {name}"
                )
                self._dispatch(name, vol)
        elif self.ec > target_max:
            logger.warning(
                f"NutrientController: EC={self.ec:.2f} above max {target_max:.2f} - "
                f"only dilution can lower EC (top up reservoir with fresh water)"
            )

    def _pump_enabled(self, config: dict, name: str) -> bool:
        pumps = config.get("dose_pumps") or {}
        pump = pumps.get(name) or {}
        return bool(pump.get("enabled", True))

    def _cooldown_ok(self, name: str, seconds: float) -> bool:
        now = time.time()
        if now - self._last_dose.get(name, 0.0) >= seconds:
            self._last_dose[name] = now
            return True
        return False

    def _dispatch(self, name: str, volume_ml: float):
        topic = f"{self.base_topic}/dose/{name}/command"
        self.client.publish(topic, f"{volume_ml:.2f}")
