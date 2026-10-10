#!/bin/bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
PACKAGE=$PWD
IP=${CVBS_IP:-192.168.144.21/24}
IFACE=${CVBS_INTERFACE:-eth0}
SERIAL=${CVBS_SERIAL:-20200909}
KEEP_NETWORK=0
KEEP_RADIOS=0
for arg in "$@"; do
    case "$arg" in
        --keep-network) KEEP_NETWORK=1 ;;
        --keep-radios) KEEP_RADIOS=1 ;;
        *) echo "Unknown option: $arg" >&2; exit 2 ;;
    esac
done
[[ $EUID -eq 0 ]] || { echo 'Run with sudo bash install.sh'; exit 1; }
[[ $(uname -m) == aarch64 ]] || { echo 'Requires Linux ARM64 / 64-bit Raspberry Pi OS'; exit 1; }
command -v apt-get >/dev/null || { echo 'Requires Debian/Raspberry Pi OS with apt'; exit 1; }
[[ -d /run/systemd/system ]] || { echo 'Requires systemd'; exit 1; }
[[ $IFACE =~ ^[a-zA-Z0-9_-]+$ && $SERIAL =~ ^[a-zA-Z0-9_-]+$ ]] || exit 2
BOOT=/boot/firmware/config.txt
if (( ! KEEP_RADIOS )); then
    [[ -f $BOOT ]] || { echo 'Raspberry boot config missing; use --keep-radios for another ARM64 board'; exit 1; }
fi
if [[ ! -f binaries/mediamtx-v1.21.1-arm64.tar.gz ]]; then
    if ! command -v curl >/dev/null; then
        apt-get update
        DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates
    fi
    curl --fail --location --retry 2 https://github.com/bluenviron/mediamtx/releases/download/v1.21.1/mediamtx_v1.21.1_linux_arm64.tar.gz -o binaries/mediamtx-v1.21.1-arm64.tar.gz.part
    echo '6a3aa635fb60ea9b8d566ec306f0a42ff1b6b52a3942bc2baffbe55880d4c3dd  binaries/mediamtx-v1.21.1-arm64.tar.gz.part' | sha256sum -c -
    mv binaries/mediamtx-v1.21.1-arm64.tar.gz.part binaries/mediamtx-v1.21.1-arm64.tar.gz
fi
sha256sum --check SHA256SUMS
# Install/download before changing Ethernet or disabling radios.
missing=0
for pkg in ffmpeg v4l-utils python3 network-manager rfkill util-linux; do
    dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q '^install ok installed$' || missing=1
done
if (( missing )); then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y ffmpeg v4l-utils python3 network-manager rfkill util-linux
fi
python3 - "$IP" <<'PY'
import ipaddress,sys
ipaddress.IPv4Interface(sys.argv[1])
PY
if (( ! KEEP_NETWORK )); then
    nmcli -t -f RUNNING general | grep -q running || { echo 'NetworkManager must be running'; exit 1; }
    [[ -d /sys/class/net/$IFACE ]] || { echo "Interface missing: $IFACE"; exit 1; }
fi
backup=/root/cvbs-backup-$(date +%Y%m%d-%H%M%S)-$$
mkdir -m 700 "$backup"
for path in /etc/cvbs-camera /etc/systemd/system/cvbs-capture.service /etc/systemd/system/cvbs-capture.service.d /etc/systemd/system/cvbs-rtsp.service /etc/systemd/system/cvbs-weak-video.service /etc/systemd/system/cvbs-panel.service /etc/NetworkManager/system-connections "$BOOT" /etc/udev/rules.d/70-cvbs-hid.rules; do
    if [[ -e $path ]]; then cp -a --parents "$path" "$backup/"; fi
done
# A running RAM patch must restore original code before replacing its worker.
if systemctl is-active --quiet cvbs-weak-video; then
    systemctl stop cvbs-weak-video
    old_run=$(cat /var/lib/cvbs-patch/current-run)
    grep -Fq 'restoration failures: []' "$old_run/restore-status.txt" || { echo 'Patch rollback not verified; aborting'; exit 1; }
fi
systemctl stop cvbs-capture cvbs-panel 2>/dev/null || true
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
tar -xzf binaries/mediamtx-v1.21.1-arm64.tar.gz -C "$work" mediamtx
install -m 755 "$work/mediamtx" /usr/local/bin/mediamtx
install -m 755 binaries/ms-tools-arm64 /usr/local/bin/ms-tools-arm64
id cvbscam >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin --groups video cvbscam
usermod -a -G video cvbscam
install -d -m 755 /etc/cvbs-camera /opt/cvbs-camera
cp mediamtx.yml /etc/cvbs-camera/mediamtx.yml
install -m 755 capture.sh /usr/local/sbin/cvbs-capture-run
printf 'CVBS_DEVICE=/dev/v4l/by-id/usb-MACROSILICON_AV_TO_USB2.0_%s-video-index0\n' "$SERIAL" > /etc/cvbs-camera/device.env
install -m 644 panel.py index.html /opt/cvbs-camera/
python3 seed-settings.py
chmod 644 /etc/cvbs-camera/image.env /etc/cvbs-camera/settings.json
if [[ ! -s /etc/cvbs-camera/panel-password ]]; then
    python3 -c 'import secrets; print(secrets.token_urlsafe(18))' > /etc/cvbs-camera/panel-password
fi
chmod 600 /etc/cvbs-camera/panel-password
# Replace only our historical correction override. Other overrides are left in the backup and in place.
rm -f /etc/systemd/system/cvbs-capture.service.d/20-image-correction.conf
cp services/*.service /etc/systemd/system/
sed "s/20200909/$SERIAL/g" 70-cvbs-hid.rules > /etc/udev/rules.d/70-cvbs-hid.rules
install -d /var/lib/cvbs-patch
install -m 755 cvbs-patch-run cvbs-patch-stop /usr/local/sbin/
udevadm control --reload-rules
udevadm trigger --subsystem-match=hidraw
udevadm settle
systemd-analyze verify /etc/systemd/system/cvbs-{rtsp,capture,weak-video,panel}.service
systemctl daemon-reload
systemctl enable --now cvbs-rtsp cvbs-capture cvbs-weak-video cvbs-panel
if (( ! KEEP_RADIOS )); then
    if ! grep -q '^# CVBS wired appliance radios$' "$BOOT"; then
        printf '\n[all]\n# CVBS wired appliance radios\ndtoverlay=disable-wifi\ndtoverlay=disable-bt\n' >> "$BOOT"
    fi
    systemctl mask bluetooth.service hciuart.service
fi
echo "Backup: $backup"
echo "Panel: http://${IP%/*}:8080 (admin)"
echo 'Panel password (save it):'
cat /etc/cvbs-camera/panel-password
echo "RTSP: rtsp://${IP%/*}:554/live"
echo 'Hardware validation: sudo bash /opt/cvbs-camera/check.sh'
install -m 755 check.sh /opt/cvbs-camera/check.sh
# Delayed activation permits SSH to receive the result before its address changes.
# Profile is tied to the chosen device; reinstallation updates the same UUID.
if (( ! KEEP_NETWORK )); then
    if ! nmcli connection show cvbs-wired >/dev/null 2>&1; then
        nmcli connection add type ethernet ifname "$IFACE" con-name cvbs-wired ipv4.method manual ipv4.addresses "$IP"
    fi
    nmcli connection modify cvbs-wired connection.interface-name "$IFACE" connection.autoconnect yes connection.autoconnect-priority 999 ipv4.method manual ipv4.addresses "$IP" ipv4.gateway '' ipv4.dns '' ipv4.never-default yes
fi
cat > /usr/local/sbin/cvbs-finish-network <<EOF
#!/bin/bash
sleep 10
EOF
if (( ! KEEP_NETWORK )); then
    printf 'nmcli connection up cvbs-wired ifname %q\n' "$IFACE" >> /usr/local/sbin/cvbs-finish-network
fi
if (( ! KEEP_RADIOS )); then
    cat >> /usr/local/sbin/cvbs-finish-network <<'EOF'
nmcli radio wifi off
rfkill block wifi
rfkill block bluetooth
systemctl stop bluetooth.service hciuart.service 2>/dev/null || true
EOF
fi
chmod 700 /usr/local/sbin/cvbs-finish-network
systemd-run --unit="cvbs-finish-$$" /usr/local/sbin/cvbs-finish-network
echo 'Installed. Network/radio changes apply in 10 seconds. Reboot to fully disable onboard radios.'
echo 'Capture/patch require a connected supported USB device; service start alone is not a video test.'
