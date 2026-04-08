#!/bin/sh
# Post-install provisioning script for WLC Simulator appliance
set -eu

echo "=== Installing LTS kernel for broad hypervisor support ==="
# The alpine-virt kernel only has virtio drivers (QEMU/KVM). VMware Fusion ARM
# uses NVMe storage and vmxnet3 networking which require the full LTS kernel.
apk add --no-cache linux-lts

# Swap virt kernel for lts in bootloader config
VIRT_VERSION=$(ls /lib/modules/ | grep virt | head -1)
LTS_VERSION=$(ls /lib/modules/ | grep lts | head -1)
echo "Virt kernel: ${VIRT_VERSION}, LTS kernel: ${LTS_VERSION}"

# Update GRUB config to use LTS kernel
if [ -f /boot/grub/grub.cfg ]; then
  echo "Updating GRUB config..."
  # Replace virt kernel/initramfs references with lts
  sed -i "s|vmlinuz-virt|vmlinuz-lts|g" /boot/grub/grub.cfg
  sed -i "s|initramfs-virt|initramfs-lts|g" /boot/grub/grub.cfg
fi

# Update extlinux config if present
if [ -f /boot/extlinux.conf ]; then
  echo "Updating extlinux config..."
  sed -i "s|vmlinuz-virt|vmlinuz-lts|g" /boot/extlinux.conf
  sed -i "s|initramfs-virt|initramfs-lts|g" /boot/extlinux.conf
fi

# Add NVMe to initramfs so root can be mounted on VMware Fusion ARM (NVMe storage)
echo "Rebuilding initramfs with NVMe support..."
if [ -f /etc/mkinitfs/mkinitfs.conf ]; then
  grep -q 'nvme' /etc/mkinitfs/mkinitfs.conf || \
    sed -i 's/features="/features="nvme /' /etc/mkinitfs/mkinitfs.conf
  cat /etc/mkinitfs/mkinitfs.conf
fi
mkinitfs "${LTS_VERSION}"

# Remove virt kernel to save space
apk del --no-cache linux-virt 2>/dev/null || true

echo "=== Installing packages ==="
apk add --no-cache iproute2 iputils

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
# Replace tty1 getty with our console TUI (framebuffer console — works on all hypervisors)
sed -i 's|^tty1.*|tty1::respawn:/usr/local/bin/wlcsim-console|' /etc/inittab
# Disable all other TTY gettys and serial consoles to avoid "can't open" errors
# on devices that don't exist in every hypervisor (e.g. ttyAMA0 is QEMU-only)
sed -i '/^tty[2-9]/s/^/#/' /etc/inittab
sed -i '/^ttyS[0-9]/s/^/#/' /etc/inittab
sed -i '/^ttyAMA[0-9]/s/^/#/' /etc/inittab

echo "=== Configuring bootloader for console output ==="
for cfg in /boot/grub/grub.cfg /boot/extlinux.conf; do
  if [ -f "$cfg" ]; then
    # Add console parameters for all hypervisor display types
    if ! grep -q 'console=tty0' "$cfg"; then
      sed -i 's|\(vmlinuz[^ ]* \)|\1console=tty0 console=ttyS0,115200 console=ttyAMA0,115200 quiet |' "$cfg" 2>/dev/null || true
      sed -i 's|\(APPEND.*root=\)|APPEND console=tty0 console=ttyS0,115200 console=ttyAMA0,115200 quiet root=|' "$cfg" 2>/dev/null || true
    fi
  fi
done

echo "=== Configuring network ==="
# Use predictable interface names and DHCP for any available interface
cat > /etc/network/interfaces << 'EOF'
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet dhcp

auto ens160
iface ens160 inet dhcp
EOF

echo "=== Setting MOTD ==="
cat > /etc/motd << 'MOTD'

  Cisco 9800-CL WLC Simulator Appliance
  Dashboard: http://<this-ip>:8080

MOTD

echo "=== Cleaning up ==="
rm -f /tmp/wlcsim /tmp/wlcsim-console /tmp/devices.yaml /tmp/running-config.tmpl /tmp/wlcsim-init

echo "=== Setup complete ==="
