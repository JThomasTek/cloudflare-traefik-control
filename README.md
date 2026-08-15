# Cloudflare Traefik Control

Cloudflare Traefik Control (CTC) will read your Traefik config.yaml routes and generate DNS records in Cloudflare. It also acts as a dynamic DNS for the DNS entries it creates.

This application can be ran using Docker (recommended) or ran locally using CLI. To function, the application expects environment variables to be set for configuration.

## Running the App

It is recommended that you run the application using Docker to make it easier to upgrade if new features are added or security patches are implemented. Below you will find a examples of running the application with a simple Docker CLI command and a `docker-compose.yaml` file example for use with Docker Compose.

### Docker CLI

``` bash
docker run -d --restart always -v path/to/local/ctc/dir:/etc/ctc -v /etc/traefik:/etc/traefik ghcr.io/jthomastek/cloudflare-traefik-control:latest
```

### Docker Compose

``` yaml
version: "3"

volumes:
    ctc-dir:

services:
    cloudflare-traefik-control:
        image: ghcr.io/jthomastek/cloudflare-traefik-control:latest
        restart: always
        volumes:
            - ctc-dir:/etc/ctc
            - /etc/traefik:/etc/traefik
```

CTC stops on `SIGTERM`, so `docker stop` lets the current reconcile finish
instead of killing the process part-way through one.

## Environment Variables

|       Variable Name       |       Default Value      | Required |
| ------------------------- | ------------------------ | -------- |
|   CLOUDFLARE_API_TOKEN    |                          |    Yes   |
|     CLOUDFLARE_ZONE_ID    |                          |    Yes   |
|         LOG_LEVEL         |                          |    No    |
|    TRAEFIK_CONFIG_FILE    | /etc/traefik/config.yaml |    Yes   |
| TRAEFIK_HOST_IGNORE_REGEX |            ^$            |    No    |
|    TRAEFIK_STATE_FILE     |   /etc/ctc/state.yml     |    No    |

The `TRAEFIK_HOST_IGNORE_REGEX` tells CTC which routes to ignore in the config.yaml file. This can be useful if you have a local DNS that handles routes that you do not want added to Cloudflare.

The `TRAEFIK_STATE_FILE` tells CTC where to keep its record of what it has already done. It defaults to `/etc/ctc/state.yml`, which is why the examples above mount a volume at `/etc/ctc` — keep that volume so a restart picks up where the last run left off instead of re-creating records it already owns.

At this time, the application only supports providing a Cloudflare API token. If there is large interest in there being API key support then it will be implemented.

## Which records CTC touches

CTC only ever changes records it created itself. Every record it adds carries the comment `Managed by ctc: <router name>`, and that comment is how it recognises its own work later. Records you created by hand are left alone, even if they share a hostname with a Traefik route.

Two consequences worth knowing:

- When your WAN IP changes, CTC repoints **every** record carrying that comment — including one whose Traefik router you have since deleted but whose record was never cleaned up. A record still carrying CTC's comment is still CTC's to maintain.
- If you edit or remove the comment on a record, CTC stops recognising it and will neither update nor delete it again.
