#!/usr/bin/env bash
set -o nounset

# set your ipinfo.io token here
if [[ -z "${IPINFO_TOKEN:-}" ]]; then
    echo "Error: IPINFO_TOKEN environment variable is not set."
    echo "Please set it first: export IPINFO_TOKEN='your_token'"
    exit 1
fi

DB_NAME='ipinfo_lite.mmdb'
DATE="$(date +%Y%m%d)"
TEMP_FILE="${DATE}_${DB_NAME}"

echo "Downloading ipinfo lite MMDB database..."

if ! curl -s --fail --output "${TEMP_FILE}" -L "https://ipinfo.io/data/ipinfo_lite.mmdb?token=${IPINFO_TOKEN}"; then
    echo "Error: Failed to download MMDB file. Check your token."
    [[ -f "${TEMP_FILE}" ]] && rm "${TEMP_FILE}"
    exit 1
fi

if [[ ! -f "${TEMP_FILE}" ]]; then
    echo "Error: Downloaded file does not exist."
    exit 1
fi

if [[ -f "${DB_NAME}" ]] && cmp -s "${TEMP_FILE}" "${DB_NAME}"; then
    echo "No changes detected in MMDB database. Exiting."
    rm "${TEMP_FILE}"
    exit 0
fi

[[ -f "${DB_NAME}" ]] && mv "${DB_NAME}" "${DB_NAME}.old"
mv "${TEMP_FILE}" "${DB_NAME}"

echo "MMDB database updated successfully."
[[ -f "${DB_NAME}.old" ]] && echo "Previous database backed up to ${DB_NAME}.old"
