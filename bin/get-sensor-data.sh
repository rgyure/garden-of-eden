#!/bin/bash
pwd=$(dirname $(readlink -f $0))
wkdir=$(realpath $pwd/..)

py="$wkdir/venv/bin/python"
# Define the log file location
LOG_FILE="/var/log/gardyn-data.log"

sudo touch $LOG_FILE

# Function to check if pigpiod is running
check_pigpiod() {
    if ! netstat -tulpn 2>/dev/null | grep -q ":8888 "; then
        echo "pigpiod not running on port 8888, attempting to start..."
        sudo pigpiod
        sleep 2  # Give pigpiod time to start

        # Verify it started
        if ! netstat -tulpn 2>/dev/null | grep -q ":8888 "; then
            echo "ERROR: Failed to start pigpiod" >&2
            return 1
        fi
        echo "pigpiod started successfully"
    fi
    return 0
}

# Check and start pigpiod if needed
check_pigpiod || echo "pigpiod not running and unable to start" >> $LOG_FILE || exit 1

# Get the current timestamp
timestamp=$(date +"%Y-%m-%d %H:%M:%S")

# Execute Python scripts and capture their output
humidity=$($py $wkdir/app/sensors/humidity/humidity.py)
sleep 1

temperature=$($py $wkdir/app/sensors/temperature/temperature.py)
sleep 1

distance=$($py $wkdir/app/sensors/distance/distance.py)

# Append the results to the log file
echo "$timestamp, $humidity" >> $LOG_FILE
echo "$timestamp, $temperature" >> $LOG_FILE
echo "$timestamp, $distance" >> $LOG_FILE
