# cat9s

`cat9s` is a separate program. It is a terminal UI for the same catalog.

Browsing does not need a credential. A published restart does. That restart republishes VIP Services, Serve, and advertisements for the names it starts. It does not write the ACL. The CLI equivalent is [`restart`](tailnet.md#restart).

## Keys

`j` / `k` are the arrow keys as well.

| | |
| --- | --- |
| List | `j` / `k` move. `r` restarts the selected row. `R` restarts every visible row that needs it. `x` stops. `l` opens logs. `/` filters. Enter describes. `q` quits. Esc closes the error panel. Otherwise it clears a filter. With no error and no filter, Esc quits. |
| Detail | Esc back. `q` quits. |
| Logs | `f` toggles follow. `/` searches. Tab is the next name. `j` / `k` scroll. Esc backs out one step: leave the search prompt, clear a search, then return to the list. |

## Restart

Published rows that are down or unhealthy are listed first.

Restart starts a dead process. When the port is already open, it stops and starts only if this catalog launched the listener. That is when the local check failed, or when the local check passed and the overall probe failed. It skips a port something else holds. It skips `not-approved` and `draining`.

`requires` are started first and are not published. `R` only restarts visible rows.
