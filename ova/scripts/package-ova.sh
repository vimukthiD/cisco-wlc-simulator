#!/bin/bash
# Package a QEMU disk image into an OVA file.
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

echo "=== Converting qcow2 to streamOptimized VMDK ==="
qemu-img convert -f qcow2 -O vmdk -o subformat=streamOptimized \
  "${QCOW2}" "${VMDK}"

echo "=== Generating OVF descriptor ==="
VMDK_SIZE=$(stat -f%z "${VMDK}" 2>/dev/null || stat -c%s "${VMDK}")
sed -e "s|VMDK_FILENAME|${VMDK_NAME}|g" \
    -e "s|VMDK_SIZE|${VMDK_SIZE}|g" \
    "${TEMPLATE_DIR}/wlcsim.ovf.tmpl" > "${OVF}"

echo "=== Generating SHA256 manifest ==="
cd "${BUILD_DIR}"
(
  for f in "${OVF_NAME}" "${VMDK_NAME}"; do
    if command -v shasum >/dev/null 2>&1; then
      hash=$(shasum -a 256 "$f" | awk '{print $1}')
    else
      hash=$(sha256sum "$f" | awk '{print $1}')
    fi
    echo "SHA256(${f})= ${hash}"
  done
) > "${MF_NAME}"
cd - >/dev/null

echo "=== Packaging OVA ==="
cd "${BUILD_DIR}"
tar -cf "wlcsim-${ARCH}.ova" "${OVF_NAME}" "${VMDK_NAME}" "${MF_NAME}"
cd - >/dev/null

OVA_SIZE=$(du -h "${OVA}" | awk '{print $1}')
echo "=== Created: ${OVA} (${OVA_SIZE}) ==="
