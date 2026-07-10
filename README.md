# Experiment Executor

Experiment Executor runs one reproducible Kubernetes experiment configuration
one or more times. Each experiment lives in its own directory and is an
immutable combination of infrastructure, tooling, application, and
load-generation configuration.

## Run with Docker

Docker is the recommended way to run Experiment Executor. The image contains
the executor, proxmox-k3s, kubectl, Helm, Load Gen, the scheduler-plugins and
descheduler charts, and chaos-injector.

From the `experiment-executor` directory, build the image:

```bash
make docker-build
```

This creates `experiment-executor:local`. The image pins each upstream
dependency through Docker build arguments:
`PROXMOX_K3S_VERSION`, `LOAD_GEN_REF`, `SCHEDULER_PLUGINS_REF`,
`DESCHEDULER_REF`, and `CHAOS_INJECTOR_REF`. A ref may be a tag, branch, or
commit; release images should use tags or commits rather than moving branches.

Define a small shell helper for subsequent commands:

```bash
WORKSPACE_ROOT="$(cd .. && pwd)"

executor() {
  docker run --rm --network host \
    --user "$(id -u):$(id -g)" \
    -v "$WORKSPACE_ROOT:/workspace" \
    -v "$HOME/.proxmox-k3s:/tmp/.proxmox-k3s:ro" \
    -w /workspace/experiment-executor \
    experiment-executor:local "$@"
}
```

The parent directory is mounted at `/workspace` because the example experiment
references application templates, Locust files, and kubeconfigs from sibling
directories. Running with the host UID and GID keeps generated results owned by
the current user.

Validate the experiment and inspect its execution plan:

```bash
executor validate -c experiments/network-aware-scheduler/experiment.yaml
executor plan -c experiments/network-aware-scheduler/experiment.yaml
executor plan -c experiments/network-aware-scheduler/experiment.yaml --json
```

Render all artifacts and commands without changing Proxmox or Kubernetes:

```bash
executor run -c experiments/network-aware-scheduler/experiment.yaml --dry-run
```

Execute all configured runs:

```bash
executor run -c experiments/network-aware-scheduler/experiment.yaml
```

`run` is mutating. In particular, the `recreate` lifecycle deletes cluster VMs
without an additional interactive prompt because that destructive policy is
explicit in the experiment.

The host network gives the container direct access to Proxmox and the
Kubernetes API. If proxmox-k3s needs SSH credentials, mount the key directory
into the container and point `ssh_key_path` at the mounted path in the
experiment config.

For a key at `$HOME/.proxmox-k3s/id_ed25519`, add this mount to the `docker
run` command inside the helper:

```bash
-v "$HOME/.proxmox-k3s:/tmp/.proxmox-k3s:ro" \
```

And set the corresponding field under `tools.proxmoxK3s.config` in the
experiment file:

```yaml
tools:
  proxmoxK3s:
    config:
      ssh_key_path: /tmp/.proxmox-k3s/id_ed25519
```

Experiment inputs, templates, Locust files, kubeconfigs, and results remain in
the mounted workspace. Relative paths are resolved from the experiment
directory.

### Build different dependency versions

Override any bundled version at build time:

```bash
docker build -t experiment-executor:custom \
  --build-arg PROXMOX_K3S_VERSION=v0.9.0 \
  --build-arg LOAD_GEN_REF=v0.0.1 \
  --build-arg SCHEDULER_PLUGINS_REF=sophos-v0.2.0 \
  --build-arg DESCHEDULER_REF=sophos-v0.0.1 \
  --build-arg CHAOS_INJECTOR_REF=v0.0.1 \
  .
```

## Native development

For local development, the native build remains available:

```bash
make build
./bin/experiment-executor validate -c experiments/network-aware-scheduler/experiment.yaml
./bin/experiment-executor plan -c experiments/network-aware-scheduler/experiment.yaml
./bin/experiment-executor plan -c experiments/network-aware-scheduler/experiment.yaml --json
```

Native execution requires the external commands and chart paths configured by
the experiment to exist on the host.

## Experiment layout

Each experiment has one input file. The executor creates the shared `config/`
directory and one output directory per run:

```text
experiments/
  network-aware-scheduler/
    experiment.yaml
    config/
      experiment.resolved.yaml
      proxmox-k3s.yaml
      kubeconfig
      scheduler-values.yaml
      descheduler-values.yaml
      namespace.yaml
      application.yaml
      network-chaos.yaml
      load-gen.yaml
    runs/
      run-001/
        run.json
        logs/
        load-gen/
    summary.json
```

The experiment file is ordinary YAML, not a Kubernetes resource or CRD. Its
root is deliberately flat:

```yaml
name: network-aware-scheduler
runs: 3
lifecycle:
  cluster: reuse
tools: {}
```

An experiment configures:

- the complete proxmox-k3s cluster configuration inline;
- mon-agent version, scrape interval, and PromQL metrics range;
- Mentat version and network probe settings;
- scheduler-plugins Helm chart plus inline Helm values;
- inline descheduler policy, interval, and dry-run mode;
- cross-zone chaos-injector latency, bandwidth restriction, packet loss, topology label, node selector, and network interface;
- the application template, namespace, group, scheduler, and proxy-node selection;
- the inline Load Gen configuration used to generate traffic and per-run plots.

Application workers can optionally be generated from `tools.proxmoxK3s.applicationPool`.
Each zone specifies a node count, first static IP, and resources; addresses are
allocated sequentially and generated nodes receive `nodepool=app` plus the
standard topology zone label. When load-gen defines `zone_distribution`, the
executor attaches discovered zones to app-node proxy endpoints automatically.

It also selects a cluster lifecycle:

```yaml
lifecycle:
  cluster: reuse
```

- `recreate`: delete and provision the configured cluster before every run;
- `reuse`: provision it for the first run and reset experiment resources before
  subsequent runs;
- `existing`: never create or delete infrastructure; validate cluster access
  and reset experiment resources before every run.

The default is `existing`, which avoids accidental infrastructure deletion.

Only reusable code or manifest assets remain references. Paths are resolved
relative to `experiment.yaml`. The executor strictly validates orchestration
fields; embedded native configuration is preserved and is additionally
validated by its own tool during execution.

See [`experiments/network-aware-scheduler/experiment.yaml`](experiments/network-aware-scheduler/experiment.yaml)
for a complete example.

The application configuration includes the LocalAI fields directly and the
executor supports the `until` and `add` template functions used by its
template. See
[`experiments/localai/application.example.yaml`](experiments/localai/application.example.yaml)
for a LocalAI application fragment with all of its parameters.

Application scheduler selection is independent from scheduler installation.
`tools.schedulerPlugins.enabled: false` means the executor will not install the
scheduler-plugins Helm chart. You can still render Pods for a scheduler already
running in the cluster:

```yaml
tools:
  schedulerPlugins:
    enabled: false
  application:
    schedulerName: scheduler-plugins-scheduler
```

## Execution lifecycle

Every run has explicit phases:

1. provision or reset the cluster;
2. configure mon-agent and Mentat;
3. install scheduler-plugins when enabled;
4. install the descheduler Helm release when enabled;
5. create the application namespace and apply derived addon labels;
6. render and deploy the application into its configured namespace;
7. generate and deploy cross-zone network chaos when enabled;
8. invoke Load Gen;
9. collect configurations, logs, metrics, plots, and summaries;
10. remove injected chaos and clean up according to the experiment lifecycle policy.

The executor snapshots every resolved input, writes one log per external
command, and records run status in `run.json`. Descheduler is installed as a
managed Helm release and uninstalled on success, failure, or interruption.
Chaos resources are generated as `config/network-chaos.yaml` and
removed during cleanup, including when a later phase fails.

## Run artifacts

Load Gen owns the plots and summary of each run. The experiment summary
aggregates matching metrics across successful runs and reports the
sample count, mean, sample standard deviation, minimum, and maximum. Aggregate
time-series plots remain future work;
they should show the across-run mean with a variability band rather than hiding
unstable runs behind a mean alone.


## Application

The current executor contract is intentionally single-tenant. An experiment
contains one application deployment with one concrete `namespace` and one
`group` label. Load Gen targets that deployment and samples replicas from the
same namespace. Multitenancy is not inferred from naming conventions and will
be designed separately after this execution path is validated.

Namespace labels are derived rather than duplicated in the experiment. The
executor applies `istio-injection=enabled` when Istio is enabled in the cluster
configuration and `mon-agent/enabled=true` when mon-agent is enabled. Applying
them is idempotent and overwrites stale values before deploying the application.

`proxyNodes` accepts:

- `all`: create a proxy on every cluster node;
- `workers`: create proxies only on worker nodes;
- `none`: do not create node proxies.

Node discovery and template rendering belong to the application deployment
phase, so proxy counts follow the actual provisioned cluster.
