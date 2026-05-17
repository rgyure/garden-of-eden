import os
import threading
import logging
from time import sleep
from datetime import datetime

import yaml
import pytz
import paho.mqtt.client as mqtt

from config import USERNAME, PASSWORD, BROKER, PORT, KEEP_ALIVE_INTERVAL, BASE_TOPIC, IDENTIFIER
from scheduler_lib.sun import get_sun_times
from scheduler_lib.pump_schedule import compute_pump_times
from scheduler_lib.reconciler import Reconciler

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler("scheduler.log"),
        logging.StreamHandler(),
    ]
)

logger = logging.getLogger(__name__)


CONFIG_PATH = 'schedule.yml'


def load_config(path=CONFIG_PATH):
    with open(path, 'r') as f:
        return yaml.safe_load(f)


def log_schedule(config):
    """Log today's sunrise/sunset and pump run times."""
    tz = pytz.timezone(config['location']['timezone'])
    now = datetime.now(tz)
    sunrise, noon, sunset = get_sun_times(config, date=now.date())

    logger.info(f"Location: {config['location']['name']}")
    logger.info(f"Date: {now.date()}")
    logger.info(f"Sunrise: {sunrise.strftime('%I:%M %p')}")
    logger.info(f"Solar noon: {noon.strftime('%I:%M %p')}")
    logger.info(f"Sunset: {sunset.strftime('%I:%M %p')}")

    pump_times = compute_pump_times(
        sunrise, sunset, config['pump']['runs_per_day']
    )
    duration = config['pump']['run_duration_minutes']
    for i, t in enumerate(pump_times, 1):
        logger.info(f"Pump run {i}: {t.strftime('%I:%M %p')} ({duration} min)")


SENSOR_LOG_FILE = "/var/log/gardyn-data.log"
SENSOR_LOG_INTERVAL = 30 * 60  # 30 minutes

sensor_logger = logging.getLogger("sensor_data")
sensor_file_handler = logging.FileHandler(SENSOR_LOG_FILE)
sensor_file_handler.setFormatter(logging.Formatter('%(message)s'))
sensor_logger.addHandler(sensor_file_handler)
sensor_logger.setLevel(logging.INFO)


def on_connect(client, userdata, flags, rc, properties=None):
    logger.info(f"Scheduler connected to MQTT broker (rc={rc})")
    # Subscribe to state topics to track actual hardware state
    client.subscribe(BASE_TOPIC + "/light/state")
    client.subscribe(BASE_TOPIC + "/light/brightness/state")
    client.subscribe(BASE_TOPIC + "/pump/state")
    # Subscribe to override topics
    client.subscribe(BASE_TOPIC + "/light/override")
    client.subscribe(BASE_TOPIC + "/pump/override")
    # Subscribe to sensor data topics for logging
    client.subscribe(BASE_TOPIC + "/temperature")
    client.subscribe(BASE_TOPIC + "/humidity")
    client.subscribe(BASE_TOPIC + "/pcb/temperature")
    # Nutrient sensors drive the auto-dose controller in scheduler_lib/nutrients.py
    client.subscribe(BASE_TOPIC + "/ph")
    client.subscribe(BASE_TOPIC + "/ec")


def create_on_message(reconciler):
    def on_message(client, userdata, msg):
        try:
            payload = msg.payload.decode("utf-8").strip()
        except UnicodeDecodeError:
            return

        topic_suffix = msg.topic.replace(BASE_TOPIC + "/", "")
        reconciler.update_state(topic_suffix, payload)

        # Log sensor readings when they arrive
        timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
        if topic_suffix == "temperature":
            sensor_logger.info(f"{timestamp}, temperature, value={payload}")
        elif topic_suffix == "humidity":
            sensor_logger.info(f"{timestamp}, humidity, value={payload}")
        elif topic_suffix == "pcb/temperature":
            sensor_logger.info(f"{timestamp}, pcb_temp, value={payload}")

    return on_message


def reconciliation_loop(reconciler, interval):
    """Run reconciliation every `interval` seconds. Hot-reloads schedule.yml on change."""
    last_mtime = None
    try:
        last_mtime = os.path.getmtime(CONFIG_PATH)
    except OSError:
        pass

    while True:
        try:
            current_mtime = os.path.getmtime(CONFIG_PATH)
            if last_mtime is not None and current_mtime != last_mtime:
                logger.info("schedule.yml changed — reloading config")
                new_config = load_config()
                reconciler.config = new_config
                log_schedule(new_config)
                interval = new_config['scheduler']['reconcile_interval_seconds']
            last_mtime = current_mtime
        except Exception as e:
            logger.exception(f"Config reload error: {e}")

        try:
            reconciler.reconcile()
        except Exception as e:
            logger.exception(f"Reconciliation error: {e}")
        sleep(interval)


def sensor_request_loop(client):
    """Request sensor readings every 30 minutes via MQTT."""
    while True:
        sleep(SENSOR_LOG_INTERVAL)
        try:
            logger.info("Requesting sensor data")
            client.publish(BASE_TOPIC + "/temperature/get", "")
            sleep(2)
            client.publish(BASE_TOPIC + "/humidity/get", "")
            sleep(2)
            client.publish(BASE_TOPIC + "/pcb/temperature/get", "")
        except Exception as e:
            logger.exception(f"Sensor request error: {e}")


if __name__ == "__main__":
    config = load_config()

    log_schedule(config)

    client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2, client_id=f"{IDENTIFIER}_scheduler")
    client.username_pw_set(USERNAME, PASSWORD)

    reconciler = Reconciler(client, BASE_TOPIC, config)

    client.on_connect = on_connect
    client.on_message = create_on_message(reconciler)

    logger.info(f"Connecting to MQTT broker at {BROKER}:{PORT}")
    client.connect(BROKER, PORT, KEEP_ALIVE_INTERVAL)

    interval = config['scheduler']['reconcile_interval_seconds']
    reconcile_thread = threading.Thread(
        target=reconciliation_loop, args=(reconciler, interval)
    )
    reconcile_thread.daemon = True
    reconcile_thread.start()

    sensor_thread = threading.Thread(
        target=sensor_request_loop, args=(client,)
    )
    sensor_thread.daemon = True
    sensor_thread.start()

    client.loop_forever()
