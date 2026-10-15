#!/bin/bash
set -euo pipefail
set -a
source /etc/cvbs-camera/device.env
source /etc/cvbs-camera/image.env
set +a
exec /usr/bin/python3 /opt/cvbs-camera/capture_auto.py
