#!/bin/sh
# Post-install provisioning script for WLC Simulator appliance
set -eu

echo "=== Installing packages ==="
apk add --no-cache iproute2 arping

echo "=== Installing simulator binaries ==="
install -m 755 /tmp/wlcsim /usr/local/bin/wlcsim
install -m 755 /tmp/wlcsim-console /usr/local/bin/wlcsim-console

echo "=== Installing configuration ==="
mkdir -p /etc/wlcsim
cp /tmp/devices.yaml /etc/wlcsim/
cp /tmp/running-config.tmpl /etc/wlcsim/

echo "=== Installing service ==="
cp /tmp/wlcsim-init /etc/init.d/wlcsim
chmod +x /etc/init.d/wlcsim
rc-update add wlcsim default

echo "=== Configuring console ==="
# Replace tty1 getty with our console TUI
sed -i 's|^tty1.*|tty1::respawn:/usr/local/bin/wlcsim-console|' /etc/inittab

echo "=== Configuring network ==="
cat > /etc/network/interfaces << 'EOF'
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet dhcp
EOF

echo "=== Setting MOTD ==="
cat > /etc/motd << 'MOTD'

  Cisco 9800-CL WLC Simulator Appliance
  Dashboard: http://<this-ip>:8080

MOTD

echo "=== Cleaning up ==="
rm -f /tmp/wlcsim /tmp/wlcsim-console /tmp/devices.yaml /tmp/running-config.tmpl /tmp/wlcsim-init

echo "=== Setup complete ==="
