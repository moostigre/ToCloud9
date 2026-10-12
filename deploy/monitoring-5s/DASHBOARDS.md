# Provisioned Grafana dashboards

These files back the existing Grafana instances on debian2:

- `files/var/lib/grafana/dashboards/server.json`: physical server metrics,
  including per-fan RPM directly below CPU temperature graphs.
- `files/var/lib/grafana-tc9/dashboards/tc9.json`: realm metrics, including
  connected players per namespace. Only game-server `active_connections`
  samples are counted; gateway connections are excluded to avoid duplication.

Deploy each JSON to its corresponding absolute path. Existing Grafana file
provisioning reloads dashboards every five seconds; no restart is required.
Both dashboards retain five-second refresh and existing selection variables.
Missing realm exporters remain missing data rather than fabricated zeros.
