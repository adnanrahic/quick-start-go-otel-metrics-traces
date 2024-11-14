# Quick Start - OpenTelemetry metrics and traces in your Go apps

1. Tidy up packages

    ```bash
    go mod tidy
    ```

2. Run the Go app

    ```bash
    go run main.go
    ```

3. [Download OpenTelemetry Collector locally for your OS.](https://opentelemetry.io/docs/collector/installation/)

    Sample for MacOS

    ```bash
    wget https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.113.0/otelcol-contrib_0.113.0_darwin_arm64.tar.gz
    ```

4. Untar and run the OpenTelemetry Collector bin

    ```bash
    mkdir otelcol-contrib && tar xvzf otelcol-contrib_0.113.0_darwin_arm64.tar.gz -C otelcol-contrib
    ```

5. Create a `config.yaml` in the otelcol-contrib directory and paste the contents from the `config.yaml` in the root of this repo

6. Run the OpenTelemetry Collector from the `otelcol-contrib` directory

    ```bash
    ./otelcol-contrib --config ./config.yaml
    ```
