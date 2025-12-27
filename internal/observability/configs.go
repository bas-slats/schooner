package observability

import (
	"fmt"
)

// getLokiConfig returns the Loki configuration
func getLokiConfig(retention string) string {
	return fmt.Sprintf(`auth_enabled: false

server:
  http_listen_port: 3100
  grpc_listen_port: 9096

common:
  instance_addr: 127.0.0.1
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory

query_range:
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 100

schema_config:
  configs:
    - from: 2020-10-24
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

ruler:
  alertmanager_url: http://localhost:9093

limits_config:
  retention_period: %s
  enforce_metric_name: false
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  ingestion_rate_mb: 10
  ingestion_burst_size_mb: 20

compactor:
  working_directory: /loki/compactor
  compaction_interval: 10m
  retention_enabled: true
  retention_delete_delay: 2h
  retention_delete_worker_count: 150
`, retention)
}

// getPromtailConfig returns the Promtail configuration
func getPromtailConfig() string {
	return `server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://schooner-loki:3100/loki/api/v1/push

scrape_configs:
  - job_name: schooner-containers
    docker_sd_configs:
      - host: unix:///var/run/docker.sock
        refresh_interval: 5s
        filters:
          - name: label
            values: ["schooner.managed=true"]
    relabel_configs:
      # Use container name as the primary label
      - source_labels: ['__meta_docker_container_name']
        regex: '/(.*)'
        target_label: 'container'
      # Extract schooner app name
      - source_labels: ['__meta_docker_container_label_schooner_app']
        target_label: 'app'
      # Extract schooner app ID
      - source_labels: ['__meta_docker_container_label_schooner_app_id']
        target_label: 'app_id'
      # Extract schooner build ID
      - source_labels: ['__meta_docker_container_label_schooner_build_id']
        target_label: 'build_id'
      # Extract schooner service type (for infrastructure containers)
      - source_labels: ['__meta_docker_container_label_schooner_service']
        target_label: 'service'
      # Extract container ID
      - source_labels: ['__meta_docker_container_id']
        target_label: 'container_id'
      # Extract image name
      - source_labels: ['__meta_docker_container_image']
        target_label: 'image'
    pipeline_stages:
      - docker: {}
`
}

// getGrafanaDatasourceConfig returns the Grafana datasource provisioning config
func getGrafanaDatasourceConfig() string {
	return `apiVersion: 1

datasources:
  - name: Loki
    type: loki
    access: proxy
    url: http://schooner-loki:3100
    isDefault: true
    editable: false
    jsonData:
      maxLines: 1000
`
}

// getGrafanaDashboardProvisionerConfig returns the Grafana dashboard provisioner config
func getGrafanaDashboardProvisionerConfig() string {
	return `apiVersion: 1

providers:
  - name: 'Schooner'
    orgId: 1
    folder: 'Schooner'
    folderUid: 'schooner'
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    options:
      path: /etc/grafana/provisioning/dashboards
`
}

// getSchoonerDashboard returns the pre-built Schooner logs dashboard
func getSchoonerDashboard() string {
	return `{
  "annotations": {
    "list": []
  },
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 0,
  "id": null,
  "links": [],
  "liveNow": false,
  "panels": [
    {
      "datasource": {
        "type": "loki",
        "uid": "loki"
      },
      "gridPos": {
        "h": 4,
        "w": 24,
        "x": 0,
        "y": 0
      },
      "id": 1,
      "options": {
        "dedupStrategy": "none",
        "enableLogDetails": true,
        "prettifyLogMessage": false,
        "showCommonLabels": false,
        "showLabels": false,
        "showTime": true,
        "sortOrder": "Descending",
        "wrapLogMessage": false
      },
      "targets": [
        {
          "datasource": {
            "type": "loki",
            "uid": "loki"
          },
          "editorMode": "builder",
          "expr": "sum by(app) (count_over_time({app=~\".+\"}[$__interval]))",
          "queryType": "range",
          "refId": "A"
        }
      ],
      "title": "Log Volume by App",
      "type": "timeseries"
    },
    {
      "datasource": {
        "type": "loki",
        "uid": "loki"
      },
      "gridPos": {
        "h": 20,
        "w": 24,
        "x": 0,
        "y": 4
      },
      "id": 2,
      "options": {
        "dedupStrategy": "none",
        "enableLogDetails": true,
        "prettifyLogMessage": false,
        "showCommonLabels": false,
        "showLabels": true,
        "showTime": true,
        "sortOrder": "Descending",
        "wrapLogMessage": true
      },
      "targets": [
        {
          "datasource": {
            "type": "loki",
            "uid": "loki"
          },
          "editorMode": "builder",
          "expr": "{app=~\"${app:regex}\", container=~\"${container:regex}\"}",
          "queryType": "range",
          "refId": "A"
        }
      ],
      "title": "Container Logs",
      "type": "logs"
    }
  ],
  "refresh": "5s",
  "schemaVersion": 38,
  "style": "dark",
  "tags": ["schooner", "logs"],
  "templating": {
    "list": [
      {
        "current": {
          "selected": true,
          "text": "All",
          "value": "$__all"
        },
        "datasource": {
          "type": "loki",
          "uid": "loki"
        },
        "definition": "label_values(app)",
        "hide": 0,
        "includeAll": true,
        "label": "App",
        "multi": true,
        "name": "app",
        "options": [],
        "query": "label_values(app)",
        "refresh": 2,
        "regex": "",
        "skipUrlSync": false,
        "sort": 1,
        "type": "query"
      },
      {
        "current": {
          "selected": true,
          "text": "All",
          "value": "$__all"
        },
        "datasource": {
          "type": "loki",
          "uid": "loki"
        },
        "definition": "label_values(container)",
        "hide": 0,
        "includeAll": true,
        "label": "Container",
        "multi": true,
        "name": "container",
        "options": [],
        "query": "label_values(container)",
        "refresh": 2,
        "regex": "",
        "skipUrlSync": false,
        "sort": 1,
        "type": "query"
      }
    ]
  },
  "time": {
    "from": "now-1h",
    "to": "now"
  },
  "timepicker": {},
  "timezone": "",
  "title": "Schooner Logs",
  "uid": "schooner-logs",
  "version": 1,
  "weekStart": ""
}`
}
