#!/bin/bash
# LMF full deploy script
# Usage: ./deploy.sh
# Recreates kind cluster with port mapping and deploys all LMF services

set -e

LMF_DIR="$HOME/Desktop/LMF_ORIG/lmf"
CLUSTER_NAME="lmf-dev"

echo "=== Step 1: Creating kind cluster config ==="
cat > "$LMF_DIR/deploy/kind-cluster.yaml" << 'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: lmf-dev
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 31789
    hostPort: 8000
    protocol: TCP
EOF

echo "=== Step 2: Delete existing cluster ==="
kind delete cluster --name $CLUSTER_NAME 2>/dev/null || echo "No existing cluster to delete"

echo "=== Step 3: Create new cluster with port mapping ==="
kind create cluster --config "$LMF_DIR/deploy/kind-cluster.yaml"

echo "=== Step 4: Apply namespace and config ==="
kubectl apply -f "$LMF_DIR/deploy/k8s/00-namespace.yaml"
kubectl apply -f "$LMF_DIR/deploy/k8s/01-configmap.yaml"
kubectl apply -f "$LMF_DIR/deploy/k8s/02-secrets.yaml"

echo "=== Step 5: Load all docker images into kind ==="
for svc in sbi-gateway location-request session-manager protocol-handler method-selector \
           gnss-engine tdoa-engine ecid-engine rtt-engine fusion-engine \
           qos-manager privacy-auth assistance-data event-manager; do
  echo "Loading lmf/$svc:latest..."
  kind load docker-image lmf/$svc:latest --name $CLUSTER_NAME 2>/dev/null || echo "Warning: lmf/$svc:latest not found, skipping"
done

echo "=== Step 6: Deploy Redis, Kafka, Cassandra ==="
# Always use the StatefulSet manifests from k8s/ directory
kubectl apply -f "$LMF_DIR/deploy/k8s/20-redis-statefulset.yaml"
kubectl apply -f "$LMF_DIR/deploy/k8s/21-kafka-statefulset.yaml"
kubectl apply -f "$LMF_DIR/deploy/k8s/22-cassandra-statefulset.yaml"

echo "=== Step 7: Scale Redis to 3 nodes ==="
kubectl scale statefulset redis-cluster -n lmf --replicas=3

echo "Waiting for all 3 redis-cluster pods to be ready..."
kubectl wait --for=condition=ready pod/redis-cluster-0 -n lmf --timeout=120s
kubectl wait --for=condition=ready pod/redis-cluster-1 -n lmf --timeout=120s
kubectl wait --for=condition=ready pod/redis-cluster-2 -n lmf --timeout=120s

echo "=== Step 8: Initialize Redis cluster ==="
sleep 5

# Reset all nodes first in case of stale state
for pod in redis-cluster-0 redis-cluster-1 redis-cluster-2; do
  kubectl exec -n lmf $pod -- redis-cli FLUSHALL 2>/dev/null || true
  kubectl exec -n lmf $pod -- redis-cli CLUSTER RESET HARD 2>/dev/null || true
done
sleep 3

kubectl exec -it redis-cluster-0 -n lmf -- redis-cli --cluster create \
  $(kubectl get pods -n lmf -l app=redis-cluster -o jsonpath='{range.items[*]}{.status.podIP}:6379 {end}') \
  --cluster-replicas 0 --cluster-yes

echo "Verifying Redis cluster state..."
kubectl exec -it redis-cluster-0 -n lmf -- redis-cli cluster info | grep cluster_state

echo "=== Step 9: Deploy all LMF services ==="
kubectl apply -f "$LMF_DIR/deploy/k8s/10-sbi-gateway.yaml"
kubectl apply -f "$LMF_DIR/deploy/k8s/11-location-request.yaml"
kubectl apply -f "$LMF_DIR/deploy/k8s/12-session-manager.yaml"
kubectl apply -f "$LMF_DIR/deploy/k8s/13-protocol-handler.yaml"
kubectl apply -f "$LMF_DIR/deploy/k8s/14-method-selector.yaml"
kubectl apply -f "$LMF_DIR/deploy/k8s/15-positioning-engines.yaml"
kubectl apply -f "$LMF_DIR/deploy/k8s/16-support-services.yaml"

echo "=== Step 10: Apply remaining manifests ==="
kubectl apply -f "$LMF_DIR/deploy/k8s/30-network-policy.yaml" 2>/dev/null || true
kubectl apply -f "$LMF_DIR/deploy/k8s/40-monitoring.yaml" 2>/dev/null || true

# Apply any extra manifests in subdirectories
for dir in deployments services configmaps hpa networkpolicies; do
  if [ -d "$LMF_DIR/deploy/k8s/$dir" ]; then
    echo "Applying $dir/..."
    kubectl apply -f "$LMF_DIR/deploy/k8s/$dir/" 2>/dev/null || true
  fi
done

echo "=== Step 11: Create NodePort service for sbi-gateway ==="
kubectl expose deployment sbi-gateway -n lmf \
  --name=sbi-gateway-external \
  --type=NodePort \
  --port=8000 \
  --target-port=8000 \
  --overrides='{"spec":{"ports":[{"port":8000,"targetPort":8000,"nodePort":31789}]}}' 2>/dev/null || true

echo "=== Step 12: Update env vars for external access ==="
kubectl set env deployment/sbi-gateway -n lmf \
  NRF_BASE_URL=http://192.168.145.26:7777 \
  LMF_SBI_IPV4=192.168.172.53 \
  LMF_SBI_PORT=8000

echo "=== Done! Waiting for all pods to be ready ==="
kubectl wait --for=condition=ready pod --all -n lmf --timeout=300s 2>/dev/null || true
kubectl get pods -n lmf

echo ""
echo "=== Test with: ==="
echo "curl http://192.168.172.53:8000/health/live"
