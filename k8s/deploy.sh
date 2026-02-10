#!/bin/bash
set -e

# KoukeBOT Kubernetes Deployment Script

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
NAMESPACE="kouke-bot"
IMAGE_NAME="kouke-bot"
IMAGE_TAG="${IMAGE_TAG:-latest}"

# Functions
print_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_prerequisites() {
    print_info "Checking prerequisites..."

    if ! command -v kubectl &> /dev/null; then
        print_error "kubectl not found. Please install kubectl."
        exit 1
    fi

    if ! kubectl cluster-info &> /dev/null; then
        print_error "Cannot connect to Kubernetes cluster."
        exit 1
    fi

    print_info "Prerequisites OK"
}

build_image() {
    print_info "Building Docker image..."
    cd ..
    docker build -t ${IMAGE_NAME}:${IMAGE_TAG} .
    cd k8s
    print_info "Image built: ${IMAGE_NAME}:${IMAGE_TAG}"
}

push_image() {
    if [ -n "$REGISTRY" ]; then
        print_info "Tagging and pushing image to registry..."
        docker tag ${IMAGE_NAME}:${IMAGE_TAG} ${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}
        docker push ${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}
        print_info "Image pushed: ${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"
    else
        print_warn "No REGISTRY set, skipping push"
    fi
}

deploy() {
    print_info "Deploying to Kubernetes..."

    # Check if namespace exists
    if ! kubectl get namespace ${NAMESPACE} &> /dev/null; then
        print_info "Creating namespace ${NAMESPACE}..."
        kubectl apply -f namespace.yaml
    fi

    # Apply configurations
    print_info "Applying configurations..."
    kubectl apply -f serviceaccount.yaml
    kubectl apply -f configmap.yaml

    # Check if secret exists
    if ! kubectl get secret kouke-bot-secret -n ${NAMESPACE} &> /dev/null; then
        print_warn "Secret not found. Please update secret.yaml before deploying."
        read -p "Continue anyway? (y/n) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            exit 1
        fi
    fi
    kubectl apply -f secret.yaml

    # Apply storage
    print_info "Applying storage configurations..."
    kubectl apply -f persistentvolume.yaml
    kubectl apply -f persistentvolumeclaim.yaml

    # Wait for PVCs to bind
    print_info "Waiting for PVCs to bind..."
    kubectl wait --for=jsonpath='{.status.phase}'=Bound pvc/kouke-bot-data-pvc -n ${NAMESPACE} --timeout=60s || print_warn "Data PVC not bound"
    kubectl wait --for=jsonpath='{.status.phase}'=Bound pvc/kouke-bot-config-pvc -n ${NAMESPACE} --timeout=60s || print_warn "Config PVC not bound"

    # Apply deployment
    print_info "Applying deployment..."
    kubectl apply -f deployment.yaml

    # Apply services
    print_info "Applying service..."
    kubectl apply -f service.yaml

    # Apply network policies
    print_info "Applying network policies..."
    kubectl apply -f networkpolicy.yaml

    # Apply PDB
    print_info "Applying pod disruption budget..."
    kubectl apply -f poddisruptionbudget.yaml

    # Apply HPA
    print_info "Applying horizontal pod autoscaler..."
    kubectl apply -f hpa.yaml

    # Apply ingress (optional)
    if [ "$DEPLOY_INGRESS" = "true" ]; then
        print_info "Applying ingress..."
        kubectl apply -f ingress.yaml
    else
        print_warn "Skipping ingress deployment (set DEPLOY_INGRESS=true to enable)"
    fi

    print_info "Deployment complete!"
}

wait_ready() {
    print_info "Waiting for deployment to be ready..."
    kubectl rollout status deployment/kouke-bot -n ${NAMESPACE} --timeout=300s
    print_info "Deployment is ready!"
}

show_status() {
    print_info "Current status:"
    echo ""
    kubectl get all -n ${NAMESPACE}
    echo ""
    kubectl get pvc -n ${NAMESPACE}
    echo ""
    print_info "Pod logs:"
    kubectl logs -n ${NAMESPACE} -l app=kouke-bot --tail=50 || true
}

cleanup() {
    print_warn "This will delete all resources in namespace ${NAMESPACE}"
    read -p "Are you sure? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        print_info "Deleting resources..."
        kubectl delete -f . || true
        kubectl delete namespace ${NAMESPACE} || true
        print_info "Cleanup complete"
    fi
}

# Main script
case "${1:-deploy}" in
    build)
        build_image
        ;;
    push)
        build_image
        push_image
        ;;
    deploy)
        check_prerequisites
        deploy
        wait_ready
        show_status
        ;;
    update)
        check_prerequisites
        print_info "Updating deployment..."
        kubectl apply -f deployment.yaml
        kubectl rollout restart deployment/kouke-bot -n ${NAMESPACE}
        wait_ready
        ;;
    status)
        show_status
        ;;
    logs)
        kubectl logs -n ${NAMESPACE} -l app=kouke-bot -f
        ;;
    cleanup)
        cleanup
        ;;
    *)
        echo "Usage: $0 {build|push|deploy|update|status|logs|cleanup}"
        echo ""
        echo "Commands:"
        echo "  build   - Build Docker image"
        echo "  push    - Build and push image to registry (requires REGISTRY env var)"
        echo "  deploy  - Deploy to Kubernetes (default)"
        echo "  update  - Update existing deployment"
        echo "  status  - Show current status"
        echo "  logs    - Follow pod logs"
        echo "  cleanup - Delete all resources"
        echo ""
        echo "Environment variables:"
        echo "  IMAGE_TAG       - Docker image tag (default: latest)"
        echo "  REGISTRY        - Docker registry URL"
        echo "  DEPLOY_INGRESS  - Deploy ingress (default: false)"
        exit 1
        ;;
esac
