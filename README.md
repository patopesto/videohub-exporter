# Blackmagic Videohub exporter

A prometheus exporter for [Blackmagic Videohubs](https://www.blackmagicdesign.com/products/blackmagicvideohub) using the TCP ethernet protocol.

## Usage

### Binaries

Download binary from [releases](https://gitlab.com/patopest/videohub-exporter/releases)

```bash
./videohub_exporter <flags>
```

View all available configuration options

```bash
./videohub_exporter -h
```

### Docker

```bash
docker run --rm \
  -p 9990:9990 \
  --name videohub_exporter \
  registry.gitlab.com/patopest/videohub-exporter:latest
```

## Metrics

| Metric                           | Description                                | Labels        |
| -------------------------------- | -------------------------------------------| ------------- |
| videohub_scrape_success          | Successful connection to videohub          |               |
| videohub_scrape_duration_seconds | Duration to connect and read videohub data |               |
| videohub_device_info             | General device info (model, name, ...etc)  | model_name, name, version, inputs, outputs, processing_units, monitoring_ouputs |
| videohub_inputs_total            | Number of SDI inputs                       |               |
| videohub_outputs_total           | Number of SDI outputs                      |               |
| videohub_input_labels            | List of each input's label                 | input, input_label |
| videohub_output_labels           | List of each output's label                | output, output_label |
| videohub_routing                 | The input connected to each output         | output, output_label, input, input_label |

## Prometheus configuration

This exporter implements the multi-target exporter pattern similar to [Blackbox exporter](https://github.com/prometheus/blackbox_exporter/tree/master). Read the guide [Understanding and using the multi-target exporter pattern](https://prometheus.io/docs/guides/multi-target-exporter/) to get the general idea about the configuration.

The videohub exporter needs to be passed the target as a parameter, this can be done with relabelling.

Example config:

```yaml
scrape_configs:
  - job_name: 'videohub'
    metrics_path: /scrape
    static_configs:
      - targets:
        - 10.0.0.1  # IP address of videohub
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: 127.0.0.1:9990  # The videohub exporter's real hostname:port.

  - job_name: 'videohub_exporter'  # Collect videohub exporter's operational metrics.
    static_configs:
      - targets: ['127.0.0.1:9990']
```


## Development

If you do not have videohub hardware available, you can use this great [mockup server](https://github.com/bitfocusas/mockup-bmd-videohub) by William Viker for Bitfocus

### Running

```bash
go run . <flags>
```

### Building

```bash
go build
```