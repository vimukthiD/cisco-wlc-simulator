#!/bin/bash
# Package a QEMU disk image into an OVA file.
# For ARM64, also creates a .vmwarevm bundle (VMware Fusion on Apple Silicon).
# Usage: package-ova.sh <arch> [build_dir]
set -euo pipefail

ARCH="${1:?Usage: package-ova.sh <amd64|arm64> [build_dir]}"
BUILD_DIR="${2:-build}"
TEMPLATE_DIR="ova/templates"

QCOW2="${BUILD_DIR}/wlcsim-${ARCH}.qcow2"
VMDK_NAME="wlcsim-${ARCH}.vmdk"
VMDK="${BUILD_DIR}/${VMDK_NAME}"
OVF_NAME="wlcsim-${ARCH}.ovf"
OVF="${BUILD_DIR}/${OVF_NAME}"
MF_NAME="wlcsim-${ARCH}.mf"
MF="${BUILD_DIR}/${MF_NAME}"
OVA="${BUILD_DIR}/wlcsim-${ARCH}.ova"

# Architecture-specific OVF parameters
case "${ARCH}" in
  arm64)
    VSYS_TYPE="vmx-20"
    OS_SECTION_ATTRS='ovf:id="101" vmw:osType="arm-other5xlinux-64"'
    OS_DESCRIPTION="Linux ARM 64-Bit"
    SCSI_SUBTYPE="VirtualSCSI"
    NET_SUBTYPE="vmxnet3"
    NET_DESCRIPTION="vmxnet3 ethernet adapter on VM Network"
    EXTRA_HW_CONFIG='<vmw:Config ovf:required="false" vmw:key="firmware" vmw:value="efi"/>'
    EXTRA_VMX_CONFIG='<vmw:ExtraConfig ovf:required="false" vmw:key="guestos" vmw:value="arm-other5xlinux-64"/>'
    ;;
  amd64)
    VSYS_TYPE="vmx-13"
    OS_SECTION_ATTRS='ovf:id="101"'
    OS_DESCRIPTION="Linux 64-Bit"
    SCSI_SUBTYPE="lsilogic"
    NET_SUBTYPE="E1000"
    NET_DESCRIPTION="E1000 ethernet adapter on VM Network"
    EXTRA_HW_CONFIG=""
    EXTRA_VMX_CONFIG=""
    ;;
  *)
    echo "ERROR: unsupported architecture: ${ARCH}" >&2
    exit 1
    ;;
esac

echo "=== Converting qcow2 to streamOptimized VMDK ==="
qemu-img convert -f qcow2 -O vmdk -o subformat=streamOptimized \
  "${QCOW2}" "${VMDK}"

echo "=== Generating OVF descriptor (${ARCH}) ==="
VMDK_SIZE=$(stat -f%z "${VMDK}" 2>/dev/null || stat -c%s "${VMDK}")
sed -e "s|VMDK_FILENAME|${VMDK_NAME}|g" \
    -e "s|VMDK_SIZE|${VMDK_SIZE}|g" \
    -e "s|VSYS_TYPE|${VSYS_TYPE}|g" \
    -e "s|OS_SECTION_ATTRS|${OS_SECTION_ATTRS}|g" \
    -e "s|OS_DESCRIPTION|${OS_DESCRIPTION}|g" \
    -e "s|SCSI_SUBTYPE|${SCSI_SUBTYPE}|g" \
    -e "s|NET_SUBTYPE|${NET_SUBTYPE}|g" \
    -e "s|NET_DESCRIPTION|${NET_DESCRIPTION}|g" \
    -e "s|EXTRA_HW_CONFIG|${EXTRA_HW_CONFIG}|g" \
    -e "s|EXTRA_VMX_CONFIG|${EXTRA_VMX_CONFIG}|g" \
    "${TEMPLATE_DIR}/wlcsim.ovf.tmpl" > "${OVF}"

echo "=== Generating manifest ==="
cd "${BUILD_DIR}"
SHA_OVF=$(shasum -a 256 "${OVF_NAME}" | awk '{print $1}')
SHA_VMDK=$(shasum -a 256 "${VMDK_NAME}" | awk '{print $1}')
cat > "${MF_NAME}" <<EOF
SHA256(${OVF_NAME})= ${SHA_OVF}
SHA256(${VMDK_NAME})= ${SHA_VMDK}
EOF
cd - >/dev/null

# Strip macOS extended attributes before packaging
if command -v xattr >/dev/null 2>&1; then
  xattr -c "${OVF}" "${VMDK}" "${MF}" 2>/dev/null || true
fi

echo "=== Packaging OVA ==="
cd "${BUILD_DIR}"
COPYFILE_DISABLE=1 tar --format=ustar -cf "wlcsim-${ARCH}.ova" "${OVF_NAME}" "${MF_NAME}" "${VMDK_NAME}"
cd - >/dev/null

OVA_SIZE=$(du -h "${OVA}" | awk '{print $1}')
echo "=== Created: ${OVA} (${OVA_SIZE}) ==="

# For ARM64, also create a .vmwarevm bundle that VMware Fusion on Apple Silicon
# can open directly (OVA import for ARM is not officially supported by ovftool).
if [ "${ARCH}" = "arm64" ]; then
  echo "=== Creating .vmwarevm bundle for VMware Fusion ARM64 ==="
  VMWAREVM_DIR="${BUILD_DIR}/wlcsim-arm64.vmwarevm"
  VMWAREVM_VMDK="disk0.vmdk"

  rm -rf "${VMWAREVM_DIR}"
  mkdir -p "${VMWAREVM_DIR}"

  # Convert to monolithicSparse VMDK (required for .vmwarevm bundles)
  qemu-img convert -f qcow2 -O vmdk -o subformat=monolithicSparse \
    "${QCOW2}" "${VMWAREVM_DIR}/${VMWAREVM_VMDK}"

  VMDK_DISK_SIZE=$(qemu-img info --output=json "${VMWAREVM_DIR}/${VMWAREVM_VMDK}" | \
    python3 -c "import sys,json; print(json.load(sys.stdin)['virtual-size'])")

  # Create .vmx file
  cat > "${VMWAREVM_DIR}/wlcsim.vmx" <<VMXEOF
.encoding = "UTF-8"
config.version = "8"
virtualHW.version = "20"
virtualHW.productCompatibility = "hosted"
architecture = "arm64"
guestOS = "arm-other5xlinux-64"
firmware = "efi"
displayName = "WLC-Simulator"
annotation = "Cisco 9800-CL WLC Simulator. Dashboard at http://<vm-ip>:8080"
numvcpus = "2"
memsize = "256"
pciBridge0.present = "TRUE"
pciBridge0.pciSlotNumber = "17"
pciBridge4.present = "TRUE"
pciBridge4.virtualDev = "pcieRootPort"
pciBridge4.pciSlotNumber = "21"
pciBridge4.functions = "8"
pciBridge5.present = "TRUE"
pciBridge5.virtualDev = "pcieRootPort"
pciBridge5.pciSlotNumber = "22"
pciBridge5.functions = "8"
pciBridge6.present = "TRUE"
pciBridge6.virtualDev = "pcieRootPort"
pciBridge6.pciSlotNumber = "23"
pciBridge6.functions = "8"
pciBridge7.present = "TRUE"
pciBridge7.virtualDev = "pcieRootPort"
pciBridge7.pciSlotNumber = "24"
pciBridge7.functions = "8"
nvme0.present = "TRUE"
nvme0:0.present = "TRUE"
nvme0:0.fileName = "${VMWAREVM_VMDK}"
ethernet0.present = "TRUE"
ethernet0.virtualDev = "vmxnet3"
ethernet0.connectionType = "bridged"
ethernet0.startConnected = "TRUE"
ethernet0.addressType = "generated"
ethernet0.pciSlotNumber = "160"
usb_xhci.present = "TRUE"
toolsInstallManager.updateCounter = "1"
softPowerOff = "TRUE"
VMXEOF

  # Zip for distribution
  VMWAREVM_ZIP="${BUILD_DIR}/wlcsim-arm64-vmwarevm.zip"
  cd "${BUILD_DIR}"
  zip -r "wlcsim-arm64-vmwarevm.zip" "wlcsim-arm64.vmwarevm/"
  cd - >/dev/null

  ZIP_SIZE=$(du -h "${VMWAREVM_ZIP}" | awk '{print $1}')
  echo "=== Created: ${VMWAREVM_ZIP} (${ZIP_SIZE}) ==="
fi
