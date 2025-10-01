#!/bin/bash

# Get Current Timestamp
timestamp_clean=$(date +"%Y-%m-%d_%H_%M")

echo "Capture Camera $timestamp_clean"

# Capture Picture from Both Cameras
/usr/bin/fswebcam -d /dev/video0 -r 2500x1900 -S 2 -F 2 /camera/upper/$timestamp_clean.jpg
/usr/bin/fswebcam -d /dev/video2 -r 2500x1900 -S 2 -F 2 /camera/lower/$timestamp_clean.jpg

rsync -artvu "/camera/" "/mnt/Cam_Ext/"

#example tkae picture and save locally
#sudo fswebcam -d /dev/video0 -r 2500x1900 -S 2 -F 2 /tmp/upper_cam.jpg
#sudo fswebcam -d /dev/video2 -r 2500x1900 -S 2 -F 2 /tmp/lower_cam.jpg

# update pass if you wish to manruall trigger
#mosquitto_pub -t "gardyn/image/upper_cam" -f /tmp/upper_cam.jpg -u gardyn -P <password>
#mosquitto_pub -t "gardyn/image/lower_cam" -f /tmp/lower_cam.jpg -u gardyn -P <password>
