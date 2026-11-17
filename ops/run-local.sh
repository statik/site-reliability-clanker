#!/usr/bin/env bash
# Run a static linux/arm64 binary as a Nanos unikernel under QEMU on an Apple Silicon Mac.
#
# Usage: ops/run-local.sh <binary> [port]
#
# Environment:
#   NANOS_ACCEL   hvf (default) or tcg. hvf needs the nightly Nanos kernel.
#   NANOS_KERNEL  nightly (default) or release.
#   NANOS_CONFIG  optional ops configuration file, for example for "Env".
#
# The script starts QEMU directly, because `ops run` fails with QEMU 11.1 and later on
# arm64 macOS. docs/HANDOFF.md, "Nanos on a laptop", gives the three causes.
set -euo pipefail

if [ $# -lt 1 ]; then
  echo "usage: $0 <binary> [port]" >&2
  exit 2
fi

binary=$1
port=${2:-7070}
accel=${NANOS_ACCEL:-hvf}
kernel_kind=${NANOS_KERNEL:-nightly}
ops_dir=${OPS_DIR:-$HOME/.ops}
ops_bin=$ops_dir/bin/ops

if [ ! -f "$binary" ]; then
  echo "error: binary not found: $binary" >&2
  exit 1
fi
if [ ! -x "$ops_bin" ]; then
  echo "error: ops not found at $ops_bin. Install it from https://ops.city" >&2
  exit 1
fi
if ! command -v qemu-system-aarch64 >/dev/null; then
  echo "error: qemu-system-aarch64 not found. Run: brew install qemu" >&2
  exit 1
fi

image_name="$(basename "$binary")-local"
build_args=()
case "$kernel_kind" in
nightly)
  build_args+=(-n)
  kernel=$ops_dir/nightly-arm/kernel.img
  ;;
release)
  kernel=$(find "$ops_dir" -maxdepth 2 -path '*-arm/kernel.img' ! -path '*nightly*' | sort -V | tail -1)
  ;;
*)
  echo "error: NANOS_KERNEL must be nightly or release, not $kernel_kind" >&2
  exit 2
  ;;
esac

case "$accel" in
hvf)
  if [ "$kernel_kind" = release ]; then
    echo "error: Nanos 0.1.56 does not boot with hvf. Use NANOS_KERNEL=nightly or NANOS_ACCEL=tcg" >&2
    exit 2
  fi
  # From QEMU 11.1, hvf uses the vGIC of the kernel, which is GICv3 only. Nanos needs GICv2,
  # so the GIC must be the one that QEMU emulates.
  accel_args=(-accel "hvf,kernel-irqchip=off" -cpu host)
  ;;
tcg)
  accel_args=(-accel tcg -cpu max)
  ;;
*)
  echo "error: NANOS_ACCEL must be hvf or tcg, not $accel" >&2
  exit 2
  ;;
esac

if [ -n "${NANOS_CONFIG:-}" ]; then
  build_args+=(-c "$NANOS_CONFIG")
fi
# The ${var[@]+...} form keeps bash 3.2 from failing on an empty array with `set -u`.
"$ops_bin" build "$binary" ${build_args[@]+"${build_args[@]}"} -i "$image_name" >/dev/null
image=$ops_dir/images/$image_name

if [ ! -f "$kernel" ]; then
  echo "error: Nanos kernel not found: $kernel" >&2
  exit 1
fi

echo "Nanos ($kernel_kind kernel, $accel) on http://127.0.0.1:$port. Stop with Ctrl-C." >&2
exec qemu-system-aarch64 \
  -machine virt,gic-version=2 "${accel_args[@]}" -m 512M \
  -kernel "$kernel" -display none -serial stdio \
  -drive "file=$image,format=raw,if=none,id=hd0" -device virtio-blk-pci,drive=hd0 \
  -device virtio-net-pci,netdev=n0,romfile= \
  -netdev "user,id=n0,hostfwd=tcp:127.0.0.1:$port-:$port"
