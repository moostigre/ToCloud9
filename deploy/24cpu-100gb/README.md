# Resource profile for the 24-CPU, 100-GB VM

All Kubernetes pods share 22 logical CPUs and the available application memory.
The host reports approximately 96 GiB of usable RAM. Reserve two CPUs and 4 GiB
for the OS and K3s, plus 1 GiB for memory-pressure eviction. This leaves about
91 GiB allocatable to pods. Systemd caps the pod slice at 2200% CPU time, so pods
collectively cannot consume more than 22 CPUs of execution time. Processes may
run on any of the 24 logical CPUs; the two-CPU reserve is capacity headroom, not
exclusive CPU isolation.

## Node configuration

On this VM, with K3s 1.32 or later, systemd, and cgroup v2:

```sh
sudo bash deploy/24cpu-100gb/apply-node-budget.sh
```

The script installs a kubelet configuration drop-in and a systemd slice override,
then restarts K3s to load the reservations. It does not delete pods or volumes.
It backs up existing overrides under `/root/tocloud9-resource-backup.*`.

Verify the effective configuration after installation and after reboot:

```sh
kubectl get nodes -o custom-columns=NAME:.metadata.name,CPU:.status.allocatable.cpu,MEMORY:.status.allocatable.memory
cat /sys/fs/cgroup/kubepods.slice/cpu.max
kubectl top nodes
kubectl top pods -A
```

Expected: 22 allocatable CPUs and `cpu.max` of `2200000 100000` (quota / period = 22).
The memory value is based
on the RAM detected by the kernel, minus the 5 GiB total reservation.

## Service configuration

Pass `values.yaml` after the environment's existing Helm values on subsequent
deployments. Keep the environment's images, credentials, volumes, ports, and
custom configuration. Review the rendered changes before upgrading an existing
installation, especially one with modifications outside Helm.

```sh
helm upgrade ptr ./chart -n tc9-ptr --reuse-values \
  -f deploy/24cpu-100gb/values.yaml
```

ToCloud9 application services can burst up to 22 CPUs and 90 GiB individually,
while the parent pod cgroup enforces the shared node memory budget and CPU quota. These ceilings are
not guaranteed allocations for each service. CPU and memory requests determine
scheduling and contention priority. Services use capacity only when their work
requires it; a single-threaded query cannot use 22 CPUs at once.

MySQL has a 22-CPU / 60-GiB ceiling and requests two CPUs and 34 GiB of memory,
allowing room for the 30 GiB InnoDB buffer pool and connection/server overhead.
The previous 2-CPU / 4-GiB ceiling and 128-MiB buffer pool were small for this host.
The pool intentionally leaves memory available for worldservers, test realms,
Redis, NATS, and the OS. Increase
it when the database working set grows; it is not an automatic elastic cache.
If the environment already uses `mysql.primary.extraFlags`, preserve those flags
when adding `--innodb-buffer-pool-size=30G`.

Go services request 50m CPU / 128 MiB. AzerothCore has a separate resource override
of 500m CPU / 3 GiB, falling back to chart-wide resources when omitted. Redis
requests 100m CPU / 128 MiB. Additional operator-managed realms remain governed
by the shared node budget; account for their actual memory usage when scaling.

Memory is finite and the large service ceilings are overcommitted. Watch node
memory pressure and pod evictions. The node reservation protects host headroom;
it does not guarantee that every service can simultaneously use its full ceiling.

## References

- [K3s kubelet configuration drop-ins](https://docs.k3s.io/installation/configuration#kubelet-configuration-files)
- [Kubernetes node resource reservations](https://kubernetes.io/docs/tasks/administer-cluster/reserve-compute-resources/)
- [MySQL buffer pool configuration](https://dev.mysql.com/doc/refman/8.4/en/innodb-buffer-pool-resize.html)

## Rollback

Restore prior overrides from the script's backup, or remove the two installed
`50-tocloud9-resources.conf` overrides if none existed. Restore the slice's runtime
quota with `systemctl set-property --runtime kubepods.slice CPUQuota=infinity`, then
run `systemctl daemon-reload` and restart K3s. Restore the previous service resource
values and MySQL configuration separately before reducing database memory limits.
