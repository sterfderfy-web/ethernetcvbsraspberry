#!/bin/bash
set -euo pipefail
source /etc/cvbs-camera/device.env
source /etc/cvbs-camera/image.env
v4l2-ctl -d "$CVBS_DEVICE" --set-ctrl="$CVBS_CAPTURE_CONTROLS"
ffmpeg -stats_period 1 -progress pipe:1 -hide_banner -loglevel warning -nostdin -thread_queue_size 4 -f v4l2 -input_format mjpeg -video_size 720x480 -framerate 25 -i "$CVBS_DEVICE" -an -vf "$CVBS_VIDEO_FILTER" -c:v libx264 -preset veryfast -tune zerolatency -profile:v baseline -b:v 2500k -maxrate 3000k -bufsize 750k -g 25 -keyint_min 25 -sc_threshold 0 -bf 0 -threads 2 -f rtsp -rtsp_transport tcp rtsp://127.0.0.1:554/live | /usr/bin/python3 /opt/cvbs-camera/progress.py
