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
    error_message = "arch must be amd64 or arm64"
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
  iso_checksum     = "none"
  output_directory = "${var.output_dir}"
  vm_name          = "${local.output_name}.qcow2"
  format           = "qcow2"
  disk_size        = var.disk_size
  memory           = var.memory
  cpus             = 2
  headless         = true

  qemu_binary  = "qemu-system-${local.qemu_arch}"
  machine_type = local.machine_type
  accelerator  = local.accelerator
  efi_boot     = local.efi_boot

  net_device       = "virtio-net-pci"
  disk_interface   = "virtio"

  ssh_username     = "root"
  ssh_password     = "packer"
  ssh_timeout      = "10m"
  shutdown_command  = "poweroff"

  boot_wait = "30s"
  boot_command = [
    "root<enter><wait>",
    "ifconfig eth0 up && udhcpc -i eth0<enter><wait5>",
    "echo 'packer' | chpasswd<enter><wait>",
    "echo 'PermitRootLogin yes' >> /etc/ssh/sshd_config<enter>",
    "mkdir -p /run/sshd && /usr/sbin/sshd<enter><wait3>",
    "setup-alpine -f /media/cdrom/answers<enter><wait5>",
    "y<enter><wait30>",
    ""
  ]

  cd_files = [
    "answers"
  ]
}

build {
  sources = ["source.qemu.wlcsim"]

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
