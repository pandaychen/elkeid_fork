#!/bin/bash
set -e

PRODUCT="my-security-agent"
INSTALL_DIR="/opt/${PRODUCT}"
CTL="${INSTALL_DIR}/${PRODUCT}ctl"

info()  { echo -e "\033[96m[INFO]\033[0m $1"; }
succ()  { echo -e "\033[92m[OK]\033[0m   $1"; }
error() { echo -e "\033[91m[ERR]\033[0m  $1"; exit 1; }

[ "$(id -u)" -ne 0 ] && error "must run as root"

mkdir -p "${INSTALL_DIR}/plugins/process-collector"
mkdir -p "${INSTALL_DIR}/log"

cp -f ./my-security-agent       "${INSTALL_DIR}/"
cp -f ./my-security-agentctl    "${CTL}"
cp -f ./process-collector        "${INSTALL_DIR}/plugins/process-collector/"

chmod 700 "${INSTALL_DIR}/${PRODUCT}"
chmod 700 "${CTL}"
chmod 700 "${INSTALL_DIR}/plugins/process-collector/process-collector"

ln -sf "${CTL}" /usr/local/bin/${PRODUCT}ctl 2>/dev/null || true

info "installing service..."
"${CTL}" install

info "starting agent..."
"${CTL}" start

succ "installation complete!"
echo
echo "Useful commands:"
echo "  ${PRODUCT}ctl status    - Check guard type"
echo "  ${PRODUCT}ctl restart   - Restart agent"
echo "  ${PRODUCT}ctl stop      - Stop agent"
echo "  ${PRODUCT}ctl uninstall - Remove service"
echo
echo "Debug commands:"
echo "  kill -USR1 \$(pidof ${PRODUCT})  - Toggle pprof"
echo "  kill -USR2 \$(pidof ${PRODUCT})  - Release memory"
echo "  journalctl -u ${PRODUCT} -f     - View logs (systemd)"
