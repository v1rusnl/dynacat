# Docker Configuration & Options
## Environment Variables

Environment variables can be set in the `docker-compose.yml` file or through a `.env` file. These control runtime behavior of Dynacat without requiring changes to the configuration file.

### LOG_LEVEL

Controls the verbosity of logging output. This is useful for debugging issues or reducing log noise when caching operations fail.

**Valid values:**

| Level | Description |
| ----- | ----------- |
| `ERROR` | Only errors are logged (minimal output) |
| `WARN` | Warnings and errors are logged |
| `INFO` | General information, warnings, and errors (default) |
| `DEBUG` | Detailed debugging information, including all API requests and responses |

**Example:**

```yaml
environment:
  - LOG_LEVEL=INFO
```

By default, when image caching fails or widgets fail to update, warning messages are displayed. If you want to suppress these messages and only see critical errors, you can set `LOG_LEVEL=ERROR`:

```yaml
environment:
  - LOG_LEVEL=ERROR
```

For troubleshooting widget updates or API issues, use `LOG_LEVEL=DEBUG`:

```yaml
environment:
  - LOG_LEVEL=DEBUG
```

> [!NOTE]
>
> Changes to `LOG_LEVEL` require restarting the container. Unlike configuration file changes, environment variable changes do not trigger a hot reload.

### BIND

The address the server listens on. By default, this is set to `0.0.0.0` which allows access from any interface.

**Example:**

```yaml
environment:
  - BIND=0.0.0.0
```

To restrict access to only localhost (for local development), use:

```yaml
environment:
  - BIND=127.0.0.1
```

## Dynamic Refreshing

Dynamic refreshing allows widgets to automatically update their data at specified intervals. This behavior can be controlled through two mechanisms:

### Via Configuration File

To disable dynamic refreshing globally, you can configure the update interval on individual widgets or use the `update-interval` property set to a very large value or remove it entirely:

```yaml
pages:
  - name: Home
    columns:
      - size: full
        widgets:
          - type: monitor
            sites:
              - title: Example
                url: https://example.com
            update-interval: 0s  # Disables automatic updates
```

An `update-interval` of `0s` will disable automatic client-side polling for that widget.

### Browser-side Control

The global page update interval can be disabled by:

1. Setting `update-interval` on individual widgets to control their refresh rate
2. Using `0s` to disable updates for specific widgets
3. Removing the property entirely to use server-side defaults

> [!TIP]
>
> If you want to reduce server load and network traffic, increase the `update-interval` values for widgets that don't need frequent updates, or set it to `0s` to disable polling entirely.

> [!NOTE]
>
> Some widgets like monitor and docker-containers have built-in default update intervals (typically 2 minutes) that are used if `update-interval` is not specified.
