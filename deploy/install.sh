#!/usr/bin/env bash
# install.sh — bootstrap Kite into a K3s cluster
set -euo pipefail

NAMESPACE="${KITE_NAMESPACE:-kite-system}"
MANIFEST_DIR="$(cd "$(dirname "$0")/manifests" && pwd)"

echo "Installing Kite into namespace: $NAMESPACE"

kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f "$MANIFEST_DIR/kite-agent-crd.yaml"
kubectl apply -f "$MANIFEST_DIR/rbac.yaml"
kubectl apply -f "$MANIFEST_DIR/deployment.yaml"

echo ""
echo "Done. Next steps:"
echo "  1. Create a Secret with your LLM API key:"
echo "     kubectl create secret generic kite-llm-secret \\"
echo "       --from-literal=ANTHROPIC_API_KEY=<your-key> -n $NAMESPACE"
echo "  2. Apply a KiteAgent CR:"
echo "     kubectl apply -f $MANIFEST_DIR/sample-kiteagent.yaml"
echo "  3. Watch the agent:"
echo "     kubectl get kiteagents -n $NAMESPACE"
