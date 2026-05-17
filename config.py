import os
from dotenv import load_dotenv

# Load environment variables from .env file
load_dotenv()

# MQTT configurations
BROKER = os.getenv("MQTT_BROKER", "localhost")
PORT = int(os.getenv("MQTT_PORT", "1883"))
KEEP_ALIVE_INTERVAL = int(os.getenv("MQTT_KEEPALIVE_INTERVAL", "60"))

# Topic configurations
VERSION = os.getenv("MQTT_VERSION", "1.0.0")
IDENTIFIER = os.getenv("MQTT_IDENTIFIER", "gardyn-xx")
MODEL= os.getenv("MQTT_DEVICE_MODEL", "gardyn 3.0")
BASE_TOPIC = os.getenv("MQTT_BASETOPIC", "gardyn")

USERNAME = os.getenv("MQTT_USERNAME")
PASSWORD = os.getenv("MQTT_PASSWORD")

SENSOR_TYPE = os.getenv('SENSOR_TYPE')

WATER_LOW_CM = float(os.getenv("WATER_LOW_CM", 0)) or None

DISTANCE_SENSOR_ENABLED = os.getenv("DISTANCE_SENSOR_ENABLED", "true").lower() == "true"

UPPER_CAMERA_DEVICE = os.getenv("UPPER_CAMERA_DEVICE", "/dev/video0")
LOWER_CAMERA_DEVICE = os.getenv("LOWER_CAMERA_DEVICE", "/dev/video2")
UPPER_IMAGE_PATH = os.getenv("UPPER_IMAGE_PATH", "/tmp/upper_camera.jpg")
LOWER_IMAGE_PATH = os.getenv("LOWER_IMAGE_PATH", "/tmp/lower_camera.jpg")
CAMERA_RESOLUTION = os.getenv("CAMERA_RESOLUTION", "640x480")
IMAGE_INTERVAL_SECONDS = int(os.getenv("IMAGE_INTERVAL_SECONDS", "3600"))

# Nutrient sensing (EC + pH). Set to "false" once Atlas Scientific EZO circuits
# are wired on I2C so the real driver code runs.
PH_STUB   = os.getenv("PH_STUB",   "true").lower() == "true"
EC_STUB   = os.getenv("EC_STUB",   "true").lower() == "true"
DOSE_STUB = os.getenv("DOSE_STUB", "true").lower() == "true"

# Default I2C addresses for Atlas EZO circuits (configurable in firmware).
PH_I2C_ADDRESS = int(os.getenv("PH_I2C_ADDRESS", "0x63"), 0)
EC_I2C_ADDRESS = int(os.getenv("EC_I2C_ADDRESS", "0x64"), 0)
