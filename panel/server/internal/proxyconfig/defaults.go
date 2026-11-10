package proxyconfig

// DefaultHAProxyVPN is the standard hapanel VPN HAProxy template.
// Matches agent defaults: maxconn, long tunnels, ClientHello gate, per-IP limits,
// entrance 8443 → app. Backends are added on the node as before; keep a stub
// backend so the config validates with `haproxy -c`.
func DefaultHAProxyVPN() string {
	return `# hapanel standard VPN template (как на нодах сейчас)
# Движок: HAProxy 3.x · TCP passthrough · Reality/VLESS-friendly
# Бэкенды на ноде добавляются по-прежнему; ниже — заглушка app для валидности.
# Правьте только если нужно; обычный путь — этот шаблон + серверы на ноде.

global
    maxconn 50000
    nbthread 2
    hard-stop-after 5m
    stats socket ipv4@0.0.0.0:9999 level admin
    stats timeout 30s
    master-worker

defaults
    mode    tcp
    no log
    option  splice-auto
    timeout connect 5s
    timeout client  30m
    timeout server  30m
    timeout tunnel  30m
    timeout client-fin 30s
    timeout server-fin 30s
    retries 2

frontend entrance_8443
    mode tcp
    bind *:8443
    maxconn 40000
    # Per-IP rate limit (как protect по умолчанию)
    stick-table type ip size 1m expire 30s store conn_cur,conn_rate(10s)
    tcp-request connection track-sc0 src
    tcp-request connection reject if { sc_conn_cur(0) gt 100 }
    tcp-request connection reject if { sc_conn_rate(0) gt 60 }
    # Только TLS ClientHello — режет пустой TCP / сканеры
    tcp-request inspect-delay 5s
    acl is_tls_clienthello req_ssl_hello_type 1
    tcp-request content accept if is_tls_clienthello
    tcp-request content reject
    default_backend app

# Заглушка: реальные server … добавляются на ноде (leastconn, weight, send-proxy-v2).
backend app
    mode tcp
    balance leastconn
`
}

// DefaultEnvoyVPN is a starter Envoy TCP listener template (panel-only until agent).
func DefaultEnvoyVPN() string {
	return `# hapanel Envoy starter (пока только хранение в панели)
# TCP listener :8443 → cluster app. Доведите под свой upstream.

static_resources:
  listeners:
  - name: entrance_8443
    address:
      socket_address: { address: 0.0.0.0, port_value: 8443 }
    filter_chains:
    - filters:
      - name: envoy.filters.network.tcp_proxy
        typed_config:
          "@type": type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy
          stat_prefix: entrance_8443
          cluster: app
  clusters:
  - name: app
    type: STRICT_DNS
    lb_policy: LEAST_REQUEST
    load_assignment:
      cluster_name: app
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address: { address: 127.0.0.1, port_value: 8443 }
`
}

// DefaultBody returns the standard template for an engine.
func DefaultBody(engine string) string {
	if engine == "envoy" {
		return DefaultEnvoyVPN()
	}
	return DefaultHAProxyVPN()
}
