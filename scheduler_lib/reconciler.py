import logging

from scheduler_lib.brightness import get_target_brightness
from scheduler_lib.pump_schedule import should_pump_be_on
from scheduler_lib.nutrients import NutrientController

logger = logging.getLogger(__name__)

BRIGHTNESS_TOLERANCE = 2  # percent


class Reconciler:
    """Compares desired state with actual state and publishes MQTT corrections."""

    def __init__(self, client, base_topic, config):
        self.client = client
        self.base_topic = base_topic
        self.config = config

        # Actual state tracked via MQTT subscriptions
        self.actual_brightness = None  # int 0-100 or None if unknown
        self.actual_light_state = None  # "ON" / "OFF" or None
        self.actual_pump_state = None  # "ON" / "OFF" or None

        # Override flags — when ON, reconciler skips that device
        self.light_override = False
        self.pump_override = False

        # Nutrient sub-controller (no-op until schedule.nutrients.enabled=true)
        self.nutrients = NutrientController(client, base_topic)

    def update_state(self, topic_suffix, payload):
        """Called from on_message to track actual hardware state."""
        if topic_suffix == "light/brightness/state":
            try:
                self.actual_brightness = int(payload)
            except (ValueError, TypeError):
                pass
        elif topic_suffix == "light/state":
            self.actual_light_state = payload.upper()
        elif topic_suffix == "pump/state":
            self.actual_pump_state = payload.upper()
        elif topic_suffix == "light/override":
            self.light_override = payload.upper() == "ON"
            logger.info(f"Light override {'enabled' if self.light_override else 'disabled'}")
        elif topic_suffix == "pump/override":
            self.pump_override = payload.upper() == "ON"
            logger.info(f"Pump override {'enabled' if self.pump_override else 'disabled'}")
        elif topic_suffix == "ph":
            try:
                self.nutrients.update_ph(float(payload))
            except (ValueError, TypeError):
                pass
        elif topic_suffix == "ec":
            try:
                self.nutrients.update_ec(float(payload))
            except (ValueError, TypeError):
                pass

    def reconcile(self):
        """Run one reconciliation cycle. Publish corrections as needed."""
        self._reconcile_light()
        self._reconcile_pump()
        self.nutrients.tick(self.config)

    def _reconcile_light(self):
        if self.light_override:
            return
        target = get_target_brightness(self.config)

        if target == 0:
            # Light should be off
            if self.actual_light_state != "OFF":
                logger.info("Reconciler: turning light OFF")
                self.client.publish(self.base_topic + "/light/command", "OFF")
        else:
            # Light should be on at target brightness
            if self.actual_light_state != "ON":
                logger.info(f"Reconciler: turning light ON at brightness {target}")
                self.client.publish(self.base_topic + "/light/brightness/set", str(target))
                self.client.publish(self.base_topic + "/light/command", "ON")
            elif self.actual_brightness is None or abs(self.actual_brightness - target) > BRIGHTNESS_TOLERANCE:
                logger.info(f"Reconciler: adjusting brightness {self.actual_brightness} -> {target}")
                self.client.publish(self.base_topic + "/light/brightness/set", str(target))

    def _reconcile_pump(self):
        if self.pump_override:
            return
        should_be_on, speed = should_pump_be_on(self.config)

        if should_be_on:
            if self.actual_pump_state != "ON":
                logger.info(f"Reconciler: turning pump ON at speed {speed}")
                self.client.publish(self.base_topic + "/pump/speed/set", str(speed))
                self.client.publish(self.base_topic + "/pump/command", "ON")
        else:
            if self.actual_pump_state == "ON":
                logger.info("Reconciler: turning pump OFF")
                self.client.publish(self.base_topic + "/pump/command", "OFF")
