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
# haveged seeds the kernel entropy pool in software — without it, crypto/rand
# blocks for ~60s at startup on ARM64 VMs while RESTCONF/SSH generate keys.
apk add --no-cache iproute2 iputils haveged
rc-update add haveged boot 2>/dev/null || true

# Disable host sshd — it would bind 0.0.0.0:22 and hijack SSH traffic to the
# simulated device alias IPs. Users reach the simulator via the console TUI;
# the wlcsim binary runs its own SSH server per device.
rc-update del sshd default 2>/dev/null || true
rc-service sshd stop 2>/dev/null || true

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

echo "=== Installing EFI fallback bootloader ==="
# UEFI looks for \EFI\BOOT\BOOTAA64.EFI (ARM64) or BOOTX64.EFI (AMD64) when
# there's no NVRAM boot entry. Without this, the VM drops to UEFI shell on
# hypervisors other than the one that built it (UTM, VirtualBox, etc.).
# --removable installs to the standard fallback path that all UEFI firmware checks.
# --no-nvram avoids writing boot entries (they won't survive hypervisor changes).

# Find the EFI System Partition mount point
ESP=""
for mp in /boot/efi /boot; do
  if [ -d "$mp/EFI" ] || mount | grep -q " $mp .*vfat"; then
    ESP="$mp"
    break
  fi
done

# Detect architecture for grub target
ARCH_GRUB=""
case "$(uname -m)" in
  aarch64) ARCH_GRUB="arm64-efi" ;;
  x86_64)  ARCH_GRUB="x86_64-efi" ;;
esac

if [ -n "$ESP" ] && [ -n "$ARCH_GRUB" ]; then
  echo "ESP at: $ESP, GRUB target: $ARCH_GRUB"
  echo "ESP contents before:"
  find "$ESP" -type f 2>/dev/null || true

  # Use grub-install --removable to create the standard fallback bootloader
  apk add --no-cache grub-efi 2>/dev/null || true
  grub-install --target="$ARCH_GRUB" --efi-directory="$ESP" --removable --no-nvram 2>&1 || true

  # If grub-install didn't work, try manual copy as fallback
  if [ ! -f "$ESP/EFI/BOOT/BOOTAA64.EFI" ] && [ ! -f "$ESP/EFI/BOOT/BOOTX64.EFI" ]; then
    echo "grub-install fallback: copying manually..."
    mkdir -p "$ESP/EFI/BOOT"
    for src in "$ESP"/EFI/*/grub*.efi /usr/lib/grub/arm64-efi/monolithic/grubaa64.efi; do
      if [ -f "$src" ]; then
        case "$(uname -m)" in
          aarch64) cp "$src" "$ESP/EFI/BOOT/BOOTAA64.EFI" ;;
          x86_64)  cp "$src" "$ESP/EFI/BOOT/BOOTX64.EFI" ;;
        esac
        echo "Copied $src"
        break
      fi
    done
  fi

  echo "ESP contents after:"
  find "$ESP" -type f 2>/dev/null || true
else
  echo "WARNING: Could not find ESP (checked /boot/efi, /boot) or detect arch"
  mount | grep -E 'boot|efi' || true
fi

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
# Static interfaces file for loopback only — dynamic DHCP handled by boot script
cat > /etc/network/interfaces << 'EOF'
auto lo
iface lo inet loopback
EOF

# Dynamic network setup: auto-detect and DHCP any non-loopback interface at boot.
# This works across all hypervisors regardless of interface naming (eth0, enp0s1, ens160, etc.)
mkdir -p /etc/local.d
cat > /etc/local.d/dhcp-all.start << 'NETSCRIPT'
#!/bin/sh
for sysif in /sys/class/net/*; do
    iface=$(basename "$sysif")
    [ "$iface" = "lo" ] && continue
    ip link set "$iface" up
    udhcpc -i "$iface" -b -q -S -p "/var/run/udhcpc.${iface}.pid" 2>/dev/null &
done
NETSCRIPT
chmod +x /etc/local.d/dhcp-all.start
rc-update add local default

echo "=== Setting MOTD ==="
cat > /etc/motd << 'MOTD'

  Cisco 9800-CL WLC Simulator Appliance
  Dashboard: http://<this-ip>:8080

MOTD

echo "=== Cleaning up ==="
rm -f /tmp/wlcsim /tmp/wlcsim-console /tmp/devices.yaml /tmp/running-config.tmpl /tmp/wlcsim-init

echo "=== Setup complete ==="
