#!/bin/bash
set -euo pipefail
ip -br addr
ls /sys/class/net
rfkill list
lsusb -d 534d:0021
ls -l /dev/cvbs-hid
systemctl is-active cvbs-rtsp cvbs-capture cvbs-weak-video cvbs-panel
systemctl show cvbs-rtsp cvbs-capture cvbs-weak-video cvbs-panel -p Id -p NRestarts
sudo tail -n 2 "$(sudo cat /var/lib/cvbs-patch/current-run)/events.jsonl"
ffprobe -v error -rtsp_transport tcp -timeout 10000000 -show_entries stream=codec_name,width,height -of json rtsp://127.0.0.1:554/live
echo 'Open panel, apply one adjustment and verify image on the actual receiver.'
