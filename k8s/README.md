# Kubernetes Deployment Guide

This directory contains Kubernetes manifests for deploying KoukeBOT.

## Prerequisites

- Kubernetes cluster (v1.20+)
- `kubectl` configured to access your cluster
- Docker image built and available: `kouke-bot:latest`
- Storage provisioner or manual PV setup

## Quick Start

### 1. Build and Push Docker Image

```bash
# Build the image
docker build -t kouke-bot:latest .

# Tag for your registry (if using remote cluster)
docker tag kouke-bot:latest your-registry/kouke-bot:latest

# Push to registry
docker push your-registry/kouke-bot:latest
```

### 2. Update Configuration

Edit the following files before deployment:

**`secret.yaml`** - Add your secrets:
```yaml
stringData:
  DISCORD_TOKEN: "your-discord-bot-token"
  GEMINI_API_KEY: "your-gemini-api-key"
```

**`persistentvolume.yaml`** - Update storage paths:
```yaml
hostPath:
  path: /mnt/data/kouke-bot/data  # Change to your data path
```

**`ingress.yaml`** - Update domain:
```yaml
rules:
- host: kouke-bot.example.com  # Change to your domain
```

### 3. Deploy to Kubernetes

#### Option A: Using kubectl

```bash
# Apply all manifests
kubectl apply -f k8s/

# Or apply in order
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/secret.yaml
kubectl apply -f k8s/persistentvolume.yaml
kubectl apply -f k8s/persistentvolumeclaim.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/ingress.yaml
```

#### Option B: Using Kustomize

```bash
# Apply with kustomize
kubectl apply -k k8s/

# Or preview changes
kubectl kustomize k8s/
```

## Verification

```bash
# Check namespace
kubectl get ns kouke-bot

# Check all resources
kubectl get all -n kouke-bot

# Check persistent volumes
kubectl get pv,pvc -n kouke-bot

# View logs
kubectl logs -n kouke-bot -l app=kouke-bot -f

# Check pod status
kubectl describe pod -n kouke-bot -l app=kouke-bot
```

## Managing the Deployment

### Update Deployment

```bash
# After updating the image
kubectl rollout restart deployment/kouke-bot -n kouke-bot

# Check rollout status
kubectl rollout status deployment/kouke-bot -n kouke-bot
```

### Scale Deployment

```bash
# Note: Only scale to 1 replica due to persistent volume constraints
kubectl scale deployment kouke-bot --replicas=1 -n kouke-bot
```

### View Logs

```bash
# Follow logs
kubectl logs -n kouke-bot -l app=kouke-bot -f

# Previous logs
kubectl logs -n kouke-bot -l app=kouke-bot --previous
```

### Execute Commands in Pod

```bash
# Get a shell
kubectl exec -n kouke-bot -it deployment/kouke-bot -- sh

# Run a command
kubectl exec -n kouke-bot deployment/kouke-bot -- ls -la /app/data
```

### Port Forward for Local Testing

```bash
# Forward port 8080 to localhost
kubectl port-forward -n kouke-bot service/kouke-bot 8080:8080

# Access at http://localhost:8080
```

## Storage Configuration

### Local Storage (Default)

The default configuration uses `hostPath` volumes with `local-storage` StorageClass. This is suitable for:
- Single-node clusters
- Development environments
- Testing

**Important**: Update the paths in `persistentvolume.yaml` to match your node's storage location.

### Dynamic Storage

For production with dynamic provisioning:

1. Remove `persistentvolume.yaml` from deployment
2. Update `persistentvolumeclaim.yaml`:
```yaml
storageClassName: your-storage-class  # e.g., gp2, standard, nfs
# Remove the selector section
```

### Cloud Provider Storage

**AWS EBS**:
```yaml
storageClassName: gp3
```

**GCE Persistent Disk**:
```yaml
storageClassName: standard
```

**Azure Disk**:
```yaml
storageClassName: managed-premium
```

## Ingress Configuration

### Using NGINX Ingress Controller

```bash
# Install NGINX ingress
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.10.0/deploy/static/provider/cloud/deploy.yaml
```

### Enable TLS with cert-manager

```bash
# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml

# Create ClusterIssuer
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: your-email@example.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
    - http01:
        ingress:
          class: nginx
EOF
```

Then uncomment TLS section in `ingress.yaml`.

## Resource Management

### Current Resource Limits

- **Requests**: 512Mi RAM, 250m CPU
- **Limits**: 2Gi RAM, 2000m CPU

Adjust in `deployment.yaml` based on your needs:

```yaml
resources:
  requests:
    memory: "512Mi"
    cpu: "250m"
  limits:
    memory: "2Gi"
    cpu: "2000m"
```

## Troubleshooting

### Pod Not Starting

```bash
# Check pod events
kubectl describe pod -n kouke-bot -l app=kouke-bot

# Check logs
kubectl logs -n kouke-bot -l app=kouke-bot
```

### PVC Not Binding

```bash
# Check PVC status
kubectl get pvc -n kouke-bot

# Check PV status
kubectl get pv

# Describe PVC for events
kubectl describe pvc -n kouke-bot kouke-bot-data-pvc
```

### Image Pull Issues

```bash
# Check image pull secrets
kubectl get pods -n kouke-bot -l app=kouke-bot -o jsonpath='{.items[0].spec.imagePullSecrets}'

# For private registries, create image pull secret
kubectl create secret docker-registry regcred \
  --docker-server=your-registry \
  --docker-username=your-username \
  --docker-password=your-password \
  -n kouke-bot

# Add to deployment.yaml
spec:
  template:
    spec:
      imagePullSecrets:
      - name: regcred
```

### Service Not Accessible

```bash
# Check service endpoints
kubectl get endpoints -n kouke-bot kouke-bot

# Test from another pod
kubectl run -n kouke-bot test-pod --image=curlimages/curl --rm -it -- curl http://kouke-bot:8080
```

## Cleanup

```bash
# Delete all resources
kubectl delete -k k8s/

# Or delete namespace (removes everything)
kubectl delete namespace kouke-bot

# Note: PVs with Retain policy must be manually deleted
kubectl delete pv kouke-bot-data-pv kouke-bot-config-pv
```

## Production Checklist

- [ ] Update secret.yaml with real credentials
- [ ] Configure proper storage class for your environment
- [ ] Set appropriate resource limits based on load testing
- [ ] Configure ingress with your domain
- [ ] Enable TLS/HTTPS with cert-manager
- [ ] Set up monitoring (Prometheus/Grafana)
- [ ] Configure backup strategy for data PVC
- [ ] Review and adjust security policies
- [ ] Set up log aggregation (ELK/Loki)
- [ ] Configure pod disruption budget for high availability
- [ ] Test disaster recovery procedures
