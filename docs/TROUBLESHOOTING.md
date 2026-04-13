## ShibuDb Troubleshooting

## Server won’t start

- **Port conflict**: if `--port` or `--management-port` is already in use, choose different ports.

```bash
shibudb start --port 9090 --management-port 19090
```

- **Server already running**: `shibudb start` uses a PID file under `<data-dir>/run/shibudb.pid`.

```bash
shibudb stop
```

If you used a custom data directory:

```bash
shibudb stop --data-dir /path/to/data
```

## Can’t log in / lost admin password

Credentials are stored in `<data-dir>/lib/users.json`. If you remove that file, the next `shibudb start` will prompt you to create a new admin user (or you can bootstrap with flags).

```bash
rm ~/.shibudb/lib/users.json
shibudb start
```

## Management API returns 403 Forbidden

The management API requires a bearer token:

```text
Authorization: Bearer <token>
```

Create a token (admin-only):

```bash
shibudb manager --username <admin> --password <pass> generate-token
```

## `DELETE-VECTOR` fails for HNSW

HNSW index types do not support vector deletion. Use Flat / IVF / PQ index types if you need deletes.

## “vector dimension mismatch”

Your query vector must have exactly `<space dimension>` values. Recreate the space with the correct dimension or send the correct-length vector.

## Where are my files?

Default data directory root:

- If `XDG_DATA_HOME` is set: `$XDG_DATA_HOME/shibudb`
- Else: `~/.shibudb`

Layout:

```text
<data-dir>/
  lib/
  log/shibudb.log
  run/shibudb.pid
```

