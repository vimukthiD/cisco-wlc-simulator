packer {
  required_plugins {
    qemu = {
      version = "~> 1"
      source  = "github.com/hashicorp/qemu"
    }
  }
}

variable "arch" {
  type    = string
  default = "amd64"
  validation {
    condition     = contains(["amd64", "arm64"], var.arch)
    error_message = "The arch variable must be amd64 or arm64."
  }
}

variable "alpine_version" {
  type    = string
  default = "3.21"
}

variable "alpine_release" {
  type    = string
  default = "3.21.3"
}

variable "output_dir" {
  type    = string
  default = "../../build"
}

variable "binary_dir" {
  type    = string
  default = "../../build"
}

variable "disk_size" {
  type    = string
  default = "2G"
}

variable "memory" {
  type    = number
  default = 512
}

locals {
  qemu_arch     = var.arch == "arm64" ? "aarch64" : "x86_64"
  alpine_arch   = var.arch == "arm64" ? "aarch64" : "x86_64"
  iso_url       = "https://dl-cdn.alpinelinux.org/alpine/v${var.alpine_version}/releases/${local.alpine_arch}/alpine-virt-${var.alpine_release}-${local.alpine_arch}.iso"
  machine_type  = var.arch == "arm64" ? "virt" : "pc"
  cpu_model     = var.arch == "arm64" ? "cortex-a72" : "qemu64"
  accelerator   = var.arch == "arm64" ? "hvf" : "hvf"
  efi_boot      = var.arch == "arm64" ? true : false
  output_name   = "wlcsim-${var.arch}"
}

source "qemu" "wlcsim" {
  iso_url          = local.iso_url
  iso_checksum     = "file:https://dl-cdn.alpinelinux.org/alpine/v${var.alpine_version}/releases/${local.alpine_arch}/alpine-virt-${var.alpine_release}-${local.alpine_arch}.iso.sha256"
  output_directory = "${var.output_dir}/packer-${var.arch}"
  vm_name          = "${local.output_name}.qcow2"
  format           = "qcow2"
  disk_size        = var.disk_size
  memory           = var.memory
  cpus             = 2
  headless          = true

  qemu_binary       = "qemu-system-${local.qemu_arch}"
  machine_type      = local.machine_type
  accelerator       = local.accelerator
  efi_boot          = local.efi_boot
  efi_firmware_code = local.efi_boot ? "/opt/homebrew/share/qemu/edk2-aarch64-code.fd" : ""
  efi_firmware_vars = local.efi_boot ? "/opt/homebrew/share/qemu/edk2-arm-vars.fd" : ""

  net_device       = "virtio-net-pci"
  disk_interface   = "virtio"

  // Use virtio-gpu for ARM64 (no VGA) so VNC shows the guest console
  qemuargs = var.arch == "arm64" ? [
    ["-device", "virtio-gpu-pci"],
    ["-device", "usb-ehci"],
    ["-device", "usb-kbd"],
  ] : []

  ssh_username     = "root"
  ssh_password     = "packer"
  ssh_timeout      = "10m"
  shutdown_command  = "poweroff"

  // EFI boot on ARM64 takes longer due to UEFI firmware init
  boot_wait = var.arch == "arm64" ? "60s" : "30s"
  boot_command = [
    // Login as root (no password on live ISO)
    "root<enter><wait5>",

    // Get network up
    "ifconfig eth0 up && udhcpc -i eth0<enter><wait5>",

    // Set root password for SSH
    "echo 'root:packer' | chpasswd<enter><wait>",

    // Install and start openssh so Packer can connect via SSH
    "apk add openssh<enter><wait10>",
    "echo 'PermitRootLogin yes' >> /etc/ssh/sshd_config<enter>",
    "rc-service sshd start<enter><wait3>",
    ""
  ]

  // No CD files needed — answers created via SSH provisioner
}

build {
  sources = ["source.qemu.wlcsim"]

  # Step 1: Install Alpine to disk via SSH (we're on the live ISO now)
  provisioner "shell" {
    inline = [
      "cat > /tmp/install.sh << 'SCRIPT'",
      "#!/bin/sh",
      "set -e",
      "",
      "# Run each setup step individually to avoid interactive prompts",
      "setup-keymap us us",
      "setup-hostname -n wlcsim",
      "setup-interfaces -a -i <<EOF",
      "auto lo",
      "iface lo inet loopback",
      "",
      "auto eth0",
      "iface eth0 inet dhcp",
      "EOF",
      "rc-service networking start || true",
      "setup-dns -d wlcsim.local 8.8.8.8",
      "setup-timezone -z UTC",
      "setup-proxy none",
      "setup-apkrepos -1",
      "setup-sshd -c openssh",
      "setup-ntp -c none",
      "",
      "# Install to disk — answer 'y' to erase and 'sys' for mode",
      "export ERASE_DISKS=/dev/vda",
      "echo y | setup-disk -m sys /dev/vda",
      "",
      "# Configure the installed system",
      "# Find the root partition (could be vda2 or vda3 depending on EFI layout)",
      "ROOT_DEV=$(blkid | grep -v fat | grep -v swap | grep vda | head -1 | cut -d: -f1)",
      "echo \"Mounting root at $ROOT_DEV\"",
      "mount $ROOT_DEV /mnt",
      "# Enable root SSH login on installed system",
      "sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /mnt/etc/ssh/sshd_config",
      "echo 'PermitRootLogin yes' >> /mnt/etc/ssh/sshd_config",
      "# Set root password on installed system",
      "chroot /mnt sh -c \"echo 'root:packer' | chpasswd\"",
      "# Make sure sshd and networking start on boot",
      "chroot /mnt rc-update add sshd default 2>/dev/null || true",
      "chroot /mnt rc-update add networking boot 2>/dev/null || true",
      "# Ensure eth0 is configured for DHCP",
      "cat > /mnt/etc/network/interfaces << 'NETEOF'",
      "auto lo",
      "iface lo inet loopback",
      "",
      "auto eth0",
      "iface eth0 inet dhcp",
      "NETEOF",
      "sync",
      "umount /mnt",
      "SCRIPT",
      "chmod +x /tmp/install.sh",
      "/tmp/install.sh",
      "reboot"
    ]
    expect_disconnect = true
  }

  # Wait for reboot into installed system (EFI boot + DHCP takes time)
  provisioner "shell" {
    pause_before = "60s"
    inline       = ["echo 'Booted into installed system'; uname -a; hostname"]
  }

  # Upload binaries and config files
  provisioner "file" {
    source      = "${var.binary_dir}/wlcsim-linux-${var.arch}"
    destination = "/tmp/wlcsim"
  }

  provisioner "file" {
    source      = "${var.binary_dir}/wlcsim-console-linux-${var.arch}"
    destination = "/tmp/wlcsim-console"
  }

  provisioner "file" {
    source      = "../rootfs/etc/wlcsim/devices.yaml"
    destination = "/tmp/devices.yaml"
  }

  provisioner "file" {
    source      = "../rootfs/etc/wlcsim/running-config.tmpl"
    destination = "/tmp/running-config.tmpl"
  }

  provisioner "file" {
    source      = "../rootfs/etc/init.d/wlcsim"
    destination = "/tmp/wlcsim-init"
  }

  # Run setup script
  provisioner "shell" {
    script = "setup.sh"
  }
}
