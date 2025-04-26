#!/bin/bash
set -x

go install sigs.k8s.io/kind
go install sigs.k8s.io/kubebuilder/v4
go install k8s.io/kubernetes/cmd/kubectl

docker network create -d=bridge --subnet=172.19.0.0/24 kind

kind version
kubebuilder version
docker --version
go version
kubectl version --client
