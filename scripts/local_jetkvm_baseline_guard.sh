#!/bin/sh
#
# Local JetKVM baseline guard.
#
# Install this on the local test device as:
#   /userdata/init.d/S20jetkvm-baseline-guard
#
# The local default app must be built from batestinha/jetkvm-android-support.
# Stock/upstream binaries and unrelated experiment binaries may exist as
# backups or debug artifacts, but they must not become /userdata/jetkvm/bin/jetkvm_app.

set -u

MARKER="jetkvm-android-support"
ROOT="/userdata/jetkvm"
BIN_DIR="${ROOT}/bin"
DEFAULT_APP="${BIN_DIR}/jetkvm_app"
BASELINE_APP="${BIN_DIR}/jetkvm_app.android-support-current"
UPDATE_APP="${ROOT}/jetkvm_app.update"
SYSTEM_UPDATE="${ROOT}/update_system.tar"
LOG_FILE="${ROOT}/baseline-guard.log"
CONFIG_FILE="/userdata/kvm_config.json"

log() {
	printf '%s %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*" >> "${LOG_FILE}"
}

stamp() {
	date -u '+%Y%m%d-%H%M%S'
}

has_android_support_marker() {
	[ -f "$1" ] && strings "$1" 2>/dev/null | grep -q "${MARKER}"
}

quarantine() {
	src="$1"
	reason="$2"
	[ -f "${src}" ] || return 0
	dst="${src}.blocked-${reason}-$(stamp)"
	mv -f "${src}" "${dst}"
	log "quarantined ${src} as ${dst}"
}

restore_baseline() {
	if ! has_android_support_marker "${BASELINE_APP}"; then
		log "cannot restore baseline: ${BASELINE_APP} is missing or lacks ${MARKER}"
		return 1
	fi

	cp -f "${BASELINE_APP}" "${DEFAULT_APP}"
	chmod 755 "${DEFAULT_APP}"
	log "restored ${DEFAULT_APP} from ${BASELINE_APP}"
}

disable_auto_updates() {
	[ -f "${CONFIG_FILE}" ] || return 0

	if grep -q '"auto_update_enabled"[[:space:]]*:[[:space:]]*true' "${CONFIG_FILE}"; then
		sed -i 's/"auto_update_enabled"[[:space:]]*:[[:space:]]*true/"auto_update_enabled": false/' "${CONFIG_FILE}"
		log "disabled auto_update_enabled in ${CONFIG_FILE}"
	fi

	if grep -q '"include_pre_release"[[:space:]]*:[[:space:]]*true' "${CONFIG_FILE}"; then
		sed -i 's/"include_pre_release"[[:space:]]*:[[:space:]]*true/"include_pre_release": false/' "${CONFIG_FILE}"
		log "disabled include_pre_release in ${CONFIG_FILE}"
	fi
}

case "${1:-start}" in
	start)
		disable_auto_updates

		if [ -f "${UPDATE_APP}" ]; then
			if has_android_support_marker "${UPDATE_APP}"; then
				cp -f "${UPDATE_APP}" "${BASELINE_APP}"
				chmod 755 "${BASELINE_APP}"
				log "accepted android-support update artifact and refreshed ${BASELINE_APP}"
			else
				quarantine "${UPDATE_APP}" "non-android-support-update"
			fi
		fi

		if [ -f "${SYSTEM_UPDATE}" ]; then
			quarantine "${SYSTEM_UPDATE}" "system-ota-disabled"
		fi

		if ! has_android_support_marker "${DEFAULT_APP}"; then
			quarantine "${DEFAULT_APP}" "non-android-support-default"
			restore_baseline
		else
			log "default app already contains ${MARKER}"
		fi
		;;
	stop)
		;;
	*)
		echo "Usage: $0 {start|stop}"
		exit 1
		;;
esac
