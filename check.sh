#!/bin/bash
set -euo pipefail
ip -br addr
ls /sys/class/net
rfkill list
lsusb
cat /run/cvbs-capture/selection.json || true
systemctl is-active cvbs-rtsp cvbs-capture cvbs-panel
systemctl is-active cvbs-weak-video || true
systemctl show cvbs-rtsp cvbs-capture cvbs-weak-video cvbs-panel -p Id -p NRestarts
if [[ -f /var/lib/cvbs-patch/current-run ]]; then
    sudo tail -n 2 "$(sudo cat /var/lib/cvbs-patch/current-run)/events.jsonl" || true
fi
ffprobe -v error -rtsp_transport tcp -timeout 10000000 -show_entries stream=codec_name,width,height -of json rtsp://127.0.0.1:554/live
echo 'Open panel, apply one adjustment and verify image on the actual receiver.'
