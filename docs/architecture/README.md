# Architecture

Nexora uses a split control-plane/data-plane architecture.

Hot path:

Client -> Cloudflare/Nginx -> Go Gateway -> upstream AI -> streamed response

Control-plane responsibilities are kept outside the token streaming path. The Go gateway must remain independently usable if the React dashboard is unavailable.
