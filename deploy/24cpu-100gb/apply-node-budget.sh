#!/usr/bin/env bash
set -euo pipefail

profile_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
if [[ "${EUID}" != 0 ]]; then
  echo "Run this script as root on the 24-CPU VM." >&2
  exit 1
fi
if [[ "$(getconf _NPROCESSORS_ONLN)" != 24 ]]; then
  echo "This profile requires exactly 24 online logical CPUs." >&2
  exit 1
fi
if ! awk '/MemTotal:/ { exit !($2 >= 99614720) }' /proc/meminfo; then
  echo "This profile requires at least 95 GiB of usable RAM." >&2
  exit 1
fi
if [[ ! -d /sys/fs/cgroup/kubepods.slice || ! -f /sys/fs/cgroup/cgroup.controllers ]]; then
  echo "This profile requires a running K3s node with systemd and cgroup v2." >&2
  exit 1
fi

backup_dir="$(mktemp -d /root/tocloud9-resource-backup.XXXXXX)"
install -d /var/lib/rancher/k3s/agent/etc/kubelet.conf.d /etc/systemd/system/kubepods.slice.d
kubelet_target=/var/lib/rancher/k3s/agent/etc/kubelet.conf.d/50-tocloud9-resources.conf
slice_target=/etc/systemd/system/kubepods.slice.d/50-tocloud9-resources.conf
for target in "${kubelet_target}" "${slice_target}"; do
  if [[ -f "${target}" ]]; then
    cp --parents -- "${target}" "${backup_dir}/"
  fi
done
install -m 0644 "${profile_dir}/kubelet-resources.conf" "${kubelet_target}"
install -m 0644 "${profile_dir}/kubepods.slice.conf" "${slice_target}"
systemctl daemon-reload
systemctl restart k3s
systemctl set-property --runtime kubepods.slice CPUQuota=2200%
echo "Node budget applied. Previous overrides, if any: ${backup_dir}"
echo "Verify: kubectl get node -o wide; cat /sys/fs/cgroup/kubepods.slice/cpu.max"
