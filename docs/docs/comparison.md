# Dynacat vs Glance - Feature Comparison

## Performance & Loading

| Feature | Dynacat | Glance |
|---|:---:|:---:|
| Single binary deployment (<20 MB) | ✓ | ✓ |
| Low memory footprint | ✓ | ✓ |
| Minimal vanilla JS | ✓ | ✓ |
| Fast initial page load (~1s uncached) | ✓ | ✖ |
| HTMX-powered partial page updates | ✓ | ✖ |
| Server-side asset proxying & caching | ✓ | ✖ |


## Dynamic Updates

| Feature | Dynacat | Glance |
|---|:---:|:---:|
| Background widget refresh| ✓ | ✖ |
| Per-widget configurable update intervals | ✓ | ✓ |
| Only updated widget HTML is swapped | ✓ | ✖ |


## Data & Caching

| Feature | Dynacat | Glance |
|---|:---:|:---:|
| Per-widget cache duration configuration | ✓ | ✓ |
| Server-side image/asset caching (API keys never exposed to browser) | ✓ | ✖ |
| API keys are never exposed to browser | ✓ | ✖ |
| Persistent data storage (SQLite) | ✓ | ✖ |
| Config hot-reload (no restart needed) | ✓ | ✓ |
| Multi-file config composition (`$include`) | ✓ | ✓ |


## Integrations & Extensibility

| Feature | Dynacat | Glance |
|---|:---:|:---:|
| RSS, social media & news feeds | ✓ | ✓ |
| Weather, calendar & market data | ✓ | ✓ |
| Container management & monitoring | ✓ | ✓ |
| Server resource monitoring | ✓ | ✓ |
| External integrations (e.g. Plex, qBitorrent) | ✓ | ✖ |
| Custom API widget with concurrent subrequests | ✓ | ✓ |
| Third-party / extension widgets | ✓ | ✓ |
| Self-hosted icon & asset serving | ✓ | ✓ |

## Security & Deployment

| Feature | Dynacat | Glance |
|---|:---:|:---:|
| Built-in authentication with rate limiting | ✓ | ✓ |
| Reverse proxy support (`X-Forwarded-For`) | ✓ | ✓ |
| Environment variable injection in config | ✓ | ✓ |
| Docker secrets support (`${secret:name}`) | ✓ | ✓ |
| File-based token loading (`${readFileFromEnv:...}`) | ✓ | ✓ |
| API keys proxied server-side (never sent to browser) | ✓ | ✖ |
| Docker Compose deployment | ✓ | ✓ |


## Summary

| | Dynacat | Glance |
|---|:---:|:---:|
| **Live background updates** | ✓ | ✖ |
| **Server-side security proxying** | ✓ | ✖ |
| **External integrations** | ✓ | ✖ |
| **PWA / installable app** | ✓ | ✖ |
| **Persistent storage** | ✓ | ✖ |
| **Lightweight self-hosted binary** | ✓ | ✓ |

<br>

> Still hesitating?
>
> Dynacat is fully compatible with existing Glance configurations!
>
> [Migration Guide](/docs/#installation/coming-from-glance)
