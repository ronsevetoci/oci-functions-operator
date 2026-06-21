#!/usr/bin/env bash
set -euo pipefail

MODE="${INVOKER_MODE:-fake}"
OCI_MODE_AUTH="${OCI_AUTH_MODE:-workload}"

echo "Checking demo prerequisites..."

if ! command -v kubectl >/dev/null 2>&1; then
  echo "error: kubectl is not available on PATH" >&2
  exit 1
fi

echo "kubectl: $(kubectl version --client=true --short 2>/dev/null || kubectl version --client=true)"

if ! kubectl get crd functions.functions.oci.oracle.com >/dev/null 2>&1; then
  echo "error: Function CRD is not installed. Run: make manifests && kubectl apply -k config/crd" >&2
  exit 1
fi

if ! kubectl get crd functionjobs.functions.oci.oracle.com >/dev/null 2>&1; then
  echo "error: FunctionJob CRD is not installed. Run: make manifests && kubectl apply -k config/crd" >&2
  exit 1
fi

case "$MODE" in
  fake)
    if [[ -z "${INVOKER_MODE:-}" ]]; then
      echo "INVOKER_MODE is not set; manager defaults to fake."
    else
      echo "INVOKER_MODE=fake"
    fi
    ;;
  oci)
    echo "INVOKER_MODE=oci"
    echo "No global OCI_FUNCTIONS_INVOKE_ENDPOINT is required."
    echo "Existing-mode Function resources must set spec.invokeEndpoint; managed Functions discover status.invokeEndpoint."
    case "$OCI_MODE_AUTH" in
      workload)
        if [[ -z "${OCI_AUTH_MODE:-}" ]]; then
          echo "OCI_AUTH_MODE is not set; OCI mode defaults to workload."
        else
          echo "OCI_AUTH_MODE=workload"
        fi
        echo "OKE workload identity is expected; no OCI config or PEM Secret is required."
        ;;
      config)
        echo "OCI_AUTH_MODE=config"
        CONFIG_FILE="${OCI_CONFIG_FILE:-$HOME/.oci/config}"
        if [[ ! -f "$CONFIG_FILE" ]]; then
          echo "error: OCI config file not found at $CONFIG_FILE" >&2
          exit 1
        fi
        echo "OCI_CONFIG_FILE=$CONFIG_FILE"
        echo "OCI_CONFIG_PROFILE=${OCI_CONFIG_PROFILE:-DEFAULT}"
        ;;
      *)
        echo "error: unsupported OCI_AUTH_MODE=$OCI_MODE_AUTH. Supported values: workload, config" >&2
        exit 1
        ;;
    esac
    ;;
  *)
    echo "error: unsupported INVOKER_MODE=$MODE. Supported values: fake, oci" >&2
    exit 1
    ;;
esac

echo "Demo prerequisites look good."
