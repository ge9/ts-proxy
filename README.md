# ts-proxy
`ts-proxy` is a userspace Tailscale client that provides TCP/UDP port-forwarding and SOCKS5 proxy with UDP support. Full cone NAT and no UDP-over-TCP for SOCKS5 UDP (as long as Tailscale is not falling back on DERP).

# Usage
```
$ ts-proxy -h
Usage of ts-proxy:
  -debug
        enable debug mode
  -fwd-socks value
        Forward SOCKS: 'bind_addr=tailscale_addr'
  -hostname string
        Tailscale device hostname (default "ts-proxy")
  -serve-socks value
        Serve SOCKS: 'tailscale_addr[,outaddr_config...]'
  -tcp value
        TCP forward rule: 'bind_addr=[TLS=]connect_addr'
  -tcp-timeout int
        TCP timeout in seconds (default 1100)
  -tsnet-dir string
        Directory for Tailscale credentials
  -udp value
        UDP forward rule: 'bind_addr=connect_addr'
  -udp-timeout int
        UDP timeout in seconds (default 330)
```

## General
`bind_addr`, `connect_addr`, and `tailscale_addr` share basically the same syntax: `host:port`.
If a host string ends with `.tshost`, it will be replaced by the corresponding IP(v4) address using Tailscale DNS.
SOCKS and forward options can be specified multiple times.

`-tcp-timeout` sets the TCP timeout in SOCKS5. `-udp-timeout` sets the UDP timeout for both UDP port forwarding and SOCKS5 UDP Associate.
`-tsnet-dir` and `-hostname` are Tailscale-specific options.

## Port Forwarding (`-tcp`, `-udp`)
If addresses (possibly resolved from `.tshost`) are within the Tailscale IP range (`100.64.0.0/10` or `fd7a:115c:a1e0::/64`), they will be bound or connected via Tailscale.
`bind_addr` accepts a port-only specification (e.g., `:8080`). `connect_addr` accepts a domain specification (e.g., `example.com:80`).
Port forwarding between two local addresses or two Tailscale addresses is also possible (though less useful).

For TCP forwarding, using `=TLS=` instead of `=` enables TLS termination. In this mode, `bind_addr` is automatically treated as a Tailscale address, so port-only specification works.
 You must enable HTTPS in the Tailscale Admin Console for this to work.

## SOCKS5 serving (`-serve-socks`)
Exposes a SOCKS5 proxy to the tailnet. A port-only specification (`:port`) is supported (IPv4), but specific Tailscale addresses (IPv4 or IPv6) can also be used.
`tailscale_port` can be followed by a comma-separated list of `outaddr_config` entries, which specify outgoing addresses for the SOCKS5 proxy.

Each `outaddr_config` must follow the `scope=ip` syntax, where `scope` is one of: `tcp4`, `tcp6`, `udp4`, `udp6`, `ip4` (sets both `tcp4` and `udp4`), or `ip6` (sets both `tcp6` and `udp6`).
Either `udp4` or `udp6` can be set to `disabled` to avoid potential performance issues with `delayed46UDPConn`.

## SOCKS5 forwarding (`-fwd-socks`)
Starts a SOCKS5 proxy locally on `bind_addr` that forwards traffic to an upstream SOCKS5 proxy specified by `tailscale_addr`.
`bind_addr` must be a local address, and `tailscale_addr` must be a Tailscale address.

## Example
`ts-proxy -hostname pc1 -serve-socks :1080,tcp4=10.0.0.1 -tcp pc1.tshost:1234=127.0.0.1:5678 -usp :1234=pc2.tshost:5678`

# How it works
`tsnet` handles all Tailscale connectivity. https://github.com/txthinking/socks5 is used for the SOCKS5 server/client with minor customizations.

## Fix for Android (Termux)
Due to https://github.com/golang/go/issues/40569, `net.Interface()` and `net.InterfaceAddrs()` do not work correctly on newer Android versions. This tool uses https://github.com/wlynxg/anet to resolve this issue. In Android, `anet` has to be run/built with `-ldflags "-checklinkname=0"` to avoid this error: `link: github.com/wlynxg/anet: invalid reference to net.zoneCache`.
Additionally, a small patch is applied to enable TLS certificate requests, which are currently disabled in the Tailscale library. This can also be set up by go.work (this is useful when ts-proxy is used as library).

# TODO
- HTTP Proxy support
- SOCKS5 authentication