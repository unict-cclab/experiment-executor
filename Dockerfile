# syntax=docker/dockerfile:1

FROM golang:1.23-bookworm AS executor-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/experiment-executor ./cmd/experiment-executor

FROM python:3.12-slim-bookworm AS runtime-tools

ARG TARGETARCH
ARG PROXMOX_K3S_VERSION=v1.0.0
ARG LOAD_GEN_REF=v0.0.1
ARG SCHEDULER_PLUGINS_REF=sophos-v0.2.0
ARG DESCHEDULER_REF=sophos-v0.0.1
ARG CHAOS_INJECTOR_REF=v0.0.2
ARG KUBECTL_VERSION=v1.36.0
ARG HELM_VERSION=v3.18.4

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git openssh-client \
    && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    case "$TARGETARCH" in amd64|arm64) arch="$TARGETARCH" ;; *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; esac; \
    asset="proxmox-k3s_${PROXMOX_K3S_VERSION}_linux_${arch}.tar.gz"; \
    base="https://github.com/unict-cclab/proxmox-k3s/releases/download/${PROXMOX_K3S_VERSION}"; \
    curl -fsSLO "${base}/${asset}"; \
    curl -fsSLO "${base}/sha256sums.txt"; \
    grep "  ${asset}$" sha256sums.txt | sha256sum -c -; \
    tar -xzf "$asset" -C /usr/local/bin --strip-components=1 "${asset%.tar.gz}/proxmox-k3s"; \
    rm "$asset" sha256sums.txt; \
    curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${arch}/kubectl"; \
    curl -fsSLo kubectl.sha256 "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${arch}/kubectl.sha256"; \
    echo "$(cat kubectl.sha256)  /usr/local/bin/kubectl" | sha256sum -c -; \
    rm kubectl.sha256; \
    helm_asset="helm-${HELM_VERSION}-linux-${arch}.tar.gz"; \
    curl -fsSLO "https://get.helm.sh/${helm_asset}"; \
    curl -fsSLO "https://get.helm.sh/${helm_asset}.sha256sum"; \
    sha256sum -c "${helm_asset}.sha256sum"; \
    tar -xzf "$helm_asset" "linux-${arch}/helm"; \
    mv "linux-${arch}/helm" /usr/local/bin/helm; \
    rm -rf "linux-${arch}" "$helm_asset" "${helm_asset}.sha256sum"; \
    chmod +x /usr/local/bin/proxmox-k3s /usr/local/bin/kubectl /usr/local/bin/helm

RUN set -eux; \
    fetch_ref() { repo="$1"; ref="$2"; destination="$3"; git init -q "$destination"; git -C "$destination" remote add origin "$repo"; git -C "$destination" fetch -q --depth 1 origin "$ref"; git -C "$destination" checkout -q --detach FETCH_HEAD; }; \
    fetch_ref https://github.com/unict-cclab/load-gen.git "$LOAD_GEN_REF" /tmp/load-gen; \
    python -m pip install --no-cache-dir /tmp/load-gen; \
    fetch_ref https://github.com/unict-cclab/scheduler-plugins.git "$SCHEDULER_PLUGINS_REF" /tmp/scheduler-plugins; \
    fetch_ref https://github.com/unict-cclab/descheduler.git "$DESCHEDULER_REF" /tmp/descheduler; \
    fetch_ref https://github.com/unict-cclab/chaos-injector.git "$CHAOS_INJECTOR_REF" /tmp/chaos-injector; \
    mkdir -p /opt/experiment-executor/bin /opt/experiment-executor/charts; \
    cp -a /tmp/scheduler-plugins/manifests/install/charts/as-a-second-scheduler /opt/experiment-executor/charts/scheduler-plugins; \
    rm /opt/experiment-executor/charts/scheduler-plugins/crds; \
    cp -rL /tmp/scheduler-plugins/manifests/crds /opt/experiment-executor/charts/scheduler-plugins/crds; \
    cp -a /tmp/descheduler/charts/descheduler /opt/experiment-executor/charts/descheduler; \
    cp /tmp/chaos-injector/chaos-injector.sh /opt/experiment-executor/bin/chaos-injector; \
    chmod +x /opt/experiment-executor/bin/chaos-injector; \
    rm -rf /tmp/load-gen /tmp/scheduler-plugins /tmp/descheduler /tmp/chaos-injector

FROM python:3.12-slim-bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates openssh-client \
    && rm -rf /var/lib/apt/lists/*

COPY --from=runtime-tools /usr/local /usr/local
COPY --from=runtime-tools /opt/experiment-executor /opt/experiment-executor
COPY --from=executor-build /out/experiment-executor /usr/local/bin/experiment-executor

ENV HOME=/tmp \
    MPLCONFIGDIR=/tmp/loadgen-matplotlib
WORKDIR /workspace
ENTRYPOINT ["experiment-executor"]
CMD ["help"]
